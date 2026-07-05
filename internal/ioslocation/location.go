package ioslocation

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"gpssim/internal/core"
	"gpssim/internal/ioscore/dtx"
	"gpssim/internal/ioscore/remotexpc"
	"gpssim/internal/iostunnel"
)

var ErrDVTLocationPending = errors.New("RSD TCP is reachable, but RemoteXPC/DTX LocationSimulation is not implemented yet")

type Client struct {
	Tunnel    *iostunnel.Manager
	logger    func(format string, args ...any)
	sessionMu sync.Mutex
	sessions  map[string]*nativeSession
}

type nativeSession struct {
	client     *dtx.Client
	rsdAddress string
	rsdPort    int
}

type routeUpdate struct {
	index         int
	total         int
	point         core.Coordinate
	scheduledAt   time.Time
	droppedBefore int
}

func New(manager *iostunnel.Manager) *Client {
	return &Client{
		Tunnel:   manager,
		sessions: make(map[string]*nativeSession),
	}
}

func (c *Client) SetLogger(logger func(format string, args ...any)) {
	c.logger = logger
}

func (c *Client) SetLocation(ctx context.Context, udid string, point core.Coordinate) error {
	info, err := c.Tunnel.EnsureRunning(ctx, udid)
	if err != nil {
		return err
	}
	if err := probeRSD(ctx, info.RSDAddress, info.RSDPort); err != nil {
		if err := locationCancellationError(ctx, err); err != nil {
			return err
		}
		c.logf("Location RSD probe failed, rebuilding tunnel once: %s", err)
		return c.retrySetLocationWithNewTunnel(ctx, udid, point, fmt.Errorf("RSD TCP probe failed after tunnel start: %w", err))
	}
	if err := c.runNativeLocation(ctx, info, point); err != nil {
		if err := locationCancellationError(ctx, err); err != nil {
			return err
		}
		c.logf("Location DVT set failed, rebuilding tunnel once: %s", err)
		return c.retrySetLocationWithNewTunnel(ctx, udid, point, err)
	}
	return nil
}

func (c *Client) PlayRoute(ctx context.Context, udid string, points []core.Coordinate, tick time.Duration) error {
	if len(points) == 0 {
		return nil
	}
	info, err := c.Tunnel.EnsureRunning(ctx, udid)
	if err != nil {
		return err
	}
	if err := probeRSD(ctx, info.RSDAddress, info.RSDPort); err != nil {
		return fmt.Errorf("RSD TCP probe failed after tunnel start: %w", err)
	}
	if err := c.runNativeRoute(ctx, info, points, tick); err != nil {
		if err := locationCancellationError(ctx, err); err != nil {
			return err
		}
		c.logf("Location DVT route failed, rebuilding tunnel once: %s", err)
		return c.retryPlayRouteWithNewTunnel(ctx, udid, points, tick, err)
	}
	return nil
}

func (c *Client) ClearLocation(ctx context.Context, udid string) error {
	defer c.closeNativeSession(udid)
	info, err := c.Tunnel.EnsureRunning(ctx, udid)
	if err != nil {
		return err
	}
	if err := probeRSD(ctx, info.RSDAddress, info.RSDPort); err != nil {
		if err := locationCancellationError(ctx, err); err != nil {
			return err
		}
		c.logf("Location clear RSD probe failed, rebuilding tunnel once: %s", err)
		return c.retryClearLocationWithNewTunnel(ctx, udid, fmt.Errorf("RSD TCP probe failed before clear: %w", err))
	}
	if err := c.runNativeClear(ctx, info); err != nil {
		if err := locationCancellationError(ctx, err); err != nil {
			return err
		}
		c.logf("Location DVT clear failed, rebuilding tunnel once: %s", err)
		return c.retryClearLocationWithNewTunnel(ctx, udid, err)
	}
	return nil
}

func (c *Client) CloseSession(udid string) {
	c.closeNativeSession(udid)
}

func (c *Client) StreamLatestLocation(ctx context.Context, udid string, updates <-chan core.Coordinate, tick time.Duration) error {
	if tick <= 0 {
		tick = 250 * time.Millisecond
	}
	info, err := c.Tunnel.EnsureRunning(ctx, udid)
	if err != nil {
		return err
	}
	if err := probeRSD(ctx, info.RSDAddress, info.RSDPort); err != nil {
		return fmt.Errorf("RSD TCP probe failed after tunnel start: %w", err)
	}

	client, err := c.nativeSession(ctx, info)
	if err != nil {
		return err
	}

	c.logf("Location DVT native: stream latest tick=%s", tick)
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	var latest core.Coordinate
	dirty := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case point, ok := <-updates:
			if !ok {
				return nil
			}
			latest = point
			dirty = true
		case <-ticker.C:
			if !dirty {
				continue
			}
			if err := client.SetLocation(ctx, latest.Lat, latest.Lon); err != nil {
				c.closeNativeSession(udid)
				return fmt.Errorf("stream latest location: %w", err)
			}
			dirty = false
		}
	}
}

func (c *Client) retrySetLocationWithNewTunnel(ctx context.Context, udid string, point core.Coordinate, firstErr error) error {
	if err := locationCancellationError(ctx, firstErr); err != nil {
		return err
	}
	c.closeNativeSession(udid)
	_ = c.Tunnel.Stop(udid)
	info, err := c.Tunnel.EnsureRunning(ctx, udid)
	if err != nil {
		return fmt.Errorf("location retry could not rebuild tunnel after first failure (%s): %w", summarizeError(firstErr), err)
	}
	if err := probeRSD(ctx, info.RSDAddress, info.RSDPort); err != nil {
		return fmt.Errorf("location retry RSD probe failed after first failure (%s): %w", summarizeError(firstErr), err)
	}
	if err := c.runNativeLocation(ctx, info, point); err != nil {
		return fmt.Errorf("location retry DVT set failed after first failure (%s): %w", summarizeError(firstErr), err)
	}
	return nil
}

func (c *Client) retryPlayRouteWithNewTunnel(ctx context.Context, udid string, points []core.Coordinate, tick time.Duration, firstErr error) error {
	if err := locationCancellationError(ctx, firstErr); err != nil {
		return err
	}
	c.closeNativeSession(udid)
	_ = c.Tunnel.Stop(udid)
	info, err := c.Tunnel.EnsureRunning(ctx, udid)
	if err != nil {
		return fmt.Errorf("route retry could not rebuild tunnel after first failure (%s): %w", summarizeError(firstErr), err)
	}
	if err := probeRSD(ctx, info.RSDAddress, info.RSDPort); err != nil {
		return fmt.Errorf("route retry RSD probe failed after first failure (%s): %w", summarizeError(firstErr), err)
	}
	if err := c.runNativeRoute(ctx, info, points, tick); err != nil {
		return fmt.Errorf("route retry DVT play failed after first failure (%s): %w", summarizeError(firstErr), err)
	}
	return nil
}

func (c *Client) retryClearLocationWithNewTunnel(ctx context.Context, udid string, firstErr error) error {
	if err := locationCancellationError(ctx, firstErr); err != nil {
		return err
	}
	c.closeNativeSession(udid)
	_ = c.Tunnel.Stop(udid)
	info, err := c.Tunnel.EnsureRunning(ctx, udid)
	if err != nil {
		return fmt.Errorf("clear retry could not rebuild tunnel after first failure (%s): %w", summarizeError(firstErr), err)
	}
	if err := probeRSD(ctx, info.RSDAddress, info.RSDPort); err != nil {
		return fmt.Errorf("clear retry RSD probe failed after first failure (%s): %w", summarizeError(firstErr), err)
	}
	if err := c.runNativeClear(ctx, info); err != nil {
		return fmt.Errorf("clear retry DVT clear failed after first failure (%s): %w", summarizeError(firstErr), err)
	}
	return nil
}

func probeRSD(ctx context.Context, address string, port int) error {
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(address, fmt.Sprint(port)))
	if err != nil {
		return err
	}
	return conn.Close()
}

func (c *Client) runNativeLocation(ctx context.Context, info iostunnel.TunnelInfo, point core.Coordinate) error {
	client, err := c.nativeSession(ctx, info)
	if err != nil {
		return err
	}
	c.logf("Location DVT native: set %.8f, %.8f", point.Lat, point.Lon)
	if err := c.setLocationWithTunnelWatch(ctx, info, client, point); err != nil {
		c.closeNativeSession(info.UDID)
		return err
	}
	return nil
}

func (c *Client) setLocationWithTunnelWatch(ctx context.Context, info iostunnel.TunnelInfo, client *dtx.Client, point core.Coordinate) error {
	lost, _ := c.Tunnel.Lost(info)
	setCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-lost:
			cancel()
		case <-setCtx.Done():
		}
	}()
	err := client.SetLocation(setCtx, point.Lat, point.Lon)
	select {
	case <-lost:
		if err == nil {
			err = fmt.Errorf("tunnel lost before DVT set confirmation")
		}
		return fmt.Errorf("tunnel lost during DVT set: %w", err)
	default:
	}
	return err
}

func (c *Client) runNativeClear(ctx context.Context, info iostunnel.TunnelInfo) error {
	client, err := c.nativeSession(ctx, info)
	if err != nil {
		return err
	}
	c.logf("Location DVT native: clear")
	return client.ClearLocation(ctx)
}

func (c *Client) runNativeRoute(ctx context.Context, info iostunnel.TunnelInfo, points []core.Coordinate, tick time.Duration) error {
	if tick <= 0 {
		tick = core.DefaultRouteTick
	}
	client, err := c.nativeSession(ctx, info)
	if err != nil {
		return err
	}
	c.logf("Location DVT native: play route points=%d tick=%s mode=latest-only", len(points), tick)

	updates := make(chan routeUpdate, 1)
	workerDone := make(chan error, 1)
	go func() {
		workerDone <- c.sendLatestRouteUpdates(ctx, client, updates, tick)
	}()

	scheduleErr := c.scheduleRouteUpdates(ctx, points, tick, updates)
	close(updates)
	workerErr := <-workerDone
	if scheduleErr != nil {
		return scheduleErr
	}
	return workerErr
}

func (c *Client) scheduleRouteUpdates(ctx context.Context, points []core.Coordinate, tick time.Duration, updates chan routeUpdate) error {
	started := time.Now()
	droppedBeforeNext := 0
	for index, point := range points {
		scheduledAt := started.Add(time.Duration(index) * tick)
		if wait := time.Until(scheduledAt); wait > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}

		update := routeUpdate{
			index:         index,
			total:         len(points),
			point:         point,
			scheduledAt:   scheduledAt,
			droppedBefore: droppedBeforeNext,
		}
		droppedBeforeNext = 0
		select {
		case <-ctx.Done():
			return ctx.Err()
		case updates <- update:
		default:
			select {
			case old := <-updates:
				droppedBeforeNext += old.droppedBefore + 1
			default:
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case updates <- routeUpdate{
				index:         index,
				total:         len(points),
				point:         point,
				scheduledAt:   scheduledAt,
				droppedBefore: droppedBeforeNext,
			}:
				droppedBeforeNext = 0
			}
		}
	}
	return nil
}

func (c *Client) sendLatestRouteUpdates(ctx context.Context, client *dtx.Client, updates <-chan routeUpdate, tick time.Duration) error {
	sent := 0
	for update := range updates {
		latest := update
		skipped := latest.droppedBefore
		closed := false
		for {
			select {
			case next, ok := <-updates:
				if !ok {
					closed = true
					goto send
				}
				skipped += next.droppedBefore + 1
				latest = next
			default:
				goto send
			}
		}

	send:
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		sendStarted := time.Now()
		if err := client.SetLocation(ctx, latest.point.Lat, latest.point.Lon); err != nil {
			return fmt.Errorf("send route point %d/%d: %w", latest.index+1, latest.total, err)
		}
		sent++
		rtt := time.Since(sendStarted)
		lag := time.Since(latest.scheduledAt)
		c.logf(
			"Location DVT native: route send point=%d/%d sent=%d skipped=%d rtt=%s lag=%s tick=%s lat=%.8f lon=%.8f",
			latest.index+1,
			latest.total,
			sent,
			skipped,
			rtt.Round(time.Millisecond),
			lag.Round(time.Millisecond),
			tick,
			latest.point.Lat,
			latest.point.Lon,
		)
		if rtt > tick {
			c.logf(
				"Location DVT native: route send slow point=%d/%d rtt=%s exceeds tick=%s",
				latest.index+1,
				latest.total,
				rtt.Round(time.Millisecond),
				tick,
			)
		}
		if closed {
			return nil
		}
	}
	return nil
}

func (c *Client) nativeSession(ctx context.Context, info iostunnel.TunnelInfo) (*dtx.Client, error) {
	c.sessionMu.Lock()
	if c.sessions == nil {
		c.sessions = make(map[string]*nativeSession)
	}
	if session := c.sessions[info.UDID]; session != nil &&
		session.rsdAddress == info.RSDAddress &&
		session.rsdPort == info.RSDPort {
		client := session.client
		c.sessionMu.Unlock()
		return client, nil
	}
	c.sessionMu.Unlock()

	c.closeNativeSession(info.UDID)
	client, err := c.openNativeDVT(ctx, info)
	if err != nil {
		return nil, err
	}

	c.sessionMu.Lock()
	if existing := c.sessions[info.UDID]; existing != nil &&
		existing.rsdAddress == info.RSDAddress &&
		existing.rsdPort == info.RSDPort {
		c.sessionMu.Unlock()
		_ = client.Close()
		return existing.client, nil
	}
	c.sessions[info.UDID] = &nativeSession{
		client:     client,
		rsdAddress: info.RSDAddress,
		rsdPort:    info.RSDPort,
	}
	c.sessionMu.Unlock()
	return client, nil
}

func (c *Client) closeNativeSession(udid string) {
	c.sessionMu.Lock()
	if udid == "" {
		sessions := c.sessions
		c.sessions = make(map[string]*nativeSession)
		c.sessionMu.Unlock()
		for _, session := range sessions {
			if session != nil {
				_ = session.client.Close()
			}
		}
		return
	}
	session := c.sessions[udid]
	delete(c.sessions, udid)
	c.sessionMu.Unlock()
	if session != nil {
		_ = session.client.Close()
	}
}

func (c *Client) openNativeDVT(ctx context.Context, info iostunnel.TunnelInfo) (*dtx.Client, error) {
	c.logf("Location RSD native: discovering DVT service via %s:%d", info.RSDAddress, info.RSDPort)
	service, err := remotexpc.DiscoverAnyService(
		ctx,
		info.RSDAddress,
		info.RSDPort,
		"com.apple.instruments.dtservicehub",
		"com.apple.instruments.remoteserver.DVTSecureSocketProxy",
		"com.apple.instruments.remoteserver",
	)
	if err != nil {
		return nil, fmt.Errorf("RSD DVT service discovery failed: %w", err)
	}
	c.logf("Location DVT native: connecting %s %s:%d", service.Name, info.RSDAddress, service.Port)
	client, err := dtx.DialLocation(ctx, info.RSDAddress, service.Port, c.logger)
	if err != nil {
		return nil, fmt.Errorf("DVT LocationSimulation connection failed: %w", err)
	}
	return client, nil
}

func (c *Client) logf(format string, args ...any) {
	if c.logger != nil {
		c.logger(format, args...)
	}
}

func summarizeError(err error) string {
	if err == nil {
		return ""
	}
	return compactText(err.Error(), 160)
}

func compactText(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

func locationCancellationError(ctx context.Context, err error) error {
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return nil
}
