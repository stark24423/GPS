package ioslocation

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"gpssim/internal/core"
	"gpssim/internal/ioscore/dtx"
	"gpssim/internal/ioscore/remotexpc"
	"gpssim/internal/iostunnel"
)

var ErrDVTLocationPending = errors.New("RSD TCP is reachable, but RemoteXPC/DTX LocationSimulation is not implemented yet")

type Client struct {
	Tunnel *iostunnel.Manager
	logger func(format string, args ...any)
}

func New(manager *iostunnel.Manager) *Client {
	return &Client{Tunnel: manager}
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
		c.logf("Location RSD probe failed, rebuilding tunnel once: %s", err)
		return c.retrySetLocationWithNewTunnel(ctx, udid, point, fmt.Errorf("RSD TCP probe failed after tunnel start: %w", err))
	}
	if err := c.runNativeLocation(ctx, info, point); err != nil {
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
		c.logf("Location DVT route failed, rebuilding tunnel once: %s", err)
		return c.retryPlayRouteWithNewTunnel(ctx, udid, points, tick, err)
	}
	return nil
}

func (c *Client) ClearLocation(ctx context.Context, udid string) error {
	if info, ok := c.Tunnel.Info(udid); ok {
		if err := probeRSD(ctx, info.RSDAddress, info.RSDPort); err != nil {
			c.logf("Location clear warning: RSD probe failed: %s", err)
			return err
		}
		if err := c.runNativeClear(ctx, info); err != nil {
			c.logf("Location clear warning: DVT clear failed: %s", err)
			return err
		}
	}
	return nil
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

	client, err := c.openNativeDVT(ctx, info)
	if err != nil {
		return err
	}
	defer client.Close()

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
				return fmt.Errorf("stream latest location: %w", err)
			}
			dirty = false
		}
	}
}

func (c *Client) retrySetLocationWithNewTunnel(ctx context.Context, udid string, point core.Coordinate, firstErr error) error {
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

func probeRSD(ctx context.Context, address string, port int) error {
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(address, fmt.Sprint(port)))
	if err != nil {
		return err
	}
	return conn.Close()
}

func (c *Client) runNativeLocation(ctx context.Context, info iostunnel.TunnelInfo, point core.Coordinate) error {
	client, err := c.openNativeDVT(ctx, info)
	if err != nil {
		return err
	}
	defer client.Close()
	c.logf("Location DVT native: set %.8f, %.8f", point.Lat, point.Lon)
	return client.SetLocation(ctx, point.Lat, point.Lon)
}

func (c *Client) runNativeClear(ctx context.Context, info iostunnel.TunnelInfo) error {
	client, err := c.openNativeDVT(ctx, info)
	if err != nil {
		return err
	}
	defer client.Close()
	c.logf("Location DVT native: clear")
	return client.ClearLocation(ctx)
}

func (c *Client) runNativeRoute(ctx context.Context, info iostunnel.TunnelInfo, points []core.Coordinate, tick time.Duration) error {
	if tick <= 0 {
		tick = time.Second
	}
	client, err := c.openNativeDVT(ctx, info)
	if err != nil {
		return err
	}
	defer client.Close()
	c.logf("Location DVT native: play route points=%d tick=%s", len(points), tick)
	for index, point := range points {
		if err := client.SetLocation(ctx, point.Lat, point.Lon); err != nil {
			return fmt.Errorf("send route point %d/%d: %w", index+1, len(points), err)
		}
		if index+1 < len(points) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(tick):
			}
		}
	}
	return nil
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
