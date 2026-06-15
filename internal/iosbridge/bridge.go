package iosbridge

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"gpssim/internal/core"
	"gpssim/internal/ioscore/lockdown"
	"gpssim/internal/ioscore/usbmux"
	"gpssim/internal/ioslocation"
	"gpssim/internal/iostunnel"
)

type Bridge struct {
	mu       sync.Mutex
	udid     string
	cancel   context.CancelFunc
	playID   uint64
	mux      *usbmux.Client
	tunnel   *iostunnel.Manager
	location *ioslocation.Client
}

func New() *Bridge {
	tunnelManager := iostunnel.NewManager()
	return &Bridge{
		mux:      usbmux.New(""),
		tunnel:   tunnelManager,
		location: ioslocation.New(tunnelManager),
	}
}

func (b *Bridge) SetLogger(logger func(format string, args ...any)) {
	b.tunnel.SetLogger(logger)
	b.location.SetLogger(logger)
}

func (b *Bridge) SetUDID(udid string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.udid = udid
}

func (b *Bridge) UDID() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.udid
}

func (b *Bridge) ListDevices() ([]core.DeviceInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	muxDevices, err := b.mux.ListDevices(ctx)
	if err != nil {
		return nil, err
	}

	devices := make([]core.DeviceInfo, 0, len(muxDevices))
	for _, device := range muxDevices {
		name := "iPhone"
		client, err := lockdown.Dial(ctx, b.mux, device)
		if err == nil {
			values, queryErr := client.QueryValues()
			_ = client.Close()
			if queryErr == nil && values.DeviceName != "" && values.DeviceName != "<nil>" {
				name = values.DeviceName
			}
		}
		devices = append(devices, core.DeviceInfo{
			ID:         device.SerialNumber,
			Name:       name,
			Kind:       "ios",
			Connected:  true,
			Connection: device.ConnectionType,
		})
	}
	return devices, nil
}

func (b *Bridge) CheckRequirements() []core.RequirementStatus {
	statuses := []core.RequirementStatus{
		{
			Name:    "Built-in Go tunnel",
			OK:      true,
			Message: "Windows USB iOS 17.4+ only; CoreDevice transport is still under active implementation.",
		},
	}

	devices, err := b.ListDevices()
	if err != nil {
		statuses = append(statuses, core.RequirementStatus{
			Name:    "USB iPhone",
			OK:      false,
			Message: err.Error(),
		})
		return statuses
	}

	message := "No iPhone detected."
	if len(devices) > 0 {
		message = fmt.Sprintf("%d USB iPhone device(s) detected.", len(devices))
	}
	statuses = append(statuses, core.RequirementStatus{
		Name:    "USB iPhone",
		OK:      len(devices) > 0,
		Message: message,
	})
	return statuses
}

func (b *Bridge) StartTunnel(ctx context.Context) (iostunnel.TunnelInfo, error) {
	return b.tunnel.Start(ctx, b.UDID())
}

func (b *Bridge) StopTunnel() error {
	return b.tunnel.Stop(b.UDID())
}

func (b *Bridge) TunnelStatus() []iostunnel.TunnelInfo {
	return b.tunnel.Status()
}

func IsLocationSimulationPending(err error) bool {
	return errors.Is(err, ioslocation.ErrDVTLocationPending)
}

func (b *Bridge) SetLocation(ctx context.Context, point core.Coordinate) error {
	b.stopPlayback()
	udid := b.UDID()
	if udid == "" {
		return fmt.Errorf("select an iPhone before setting location")
	}
	return b.location.SetLocation(ctx, udid, point)
}

func (b *Bridge) PlayRoute(ctx context.Context, points []core.Coordinate, tick time.Duration) error {
	if len(points) == 0 {
		return fmt.Errorf("at least one coordinate is required")
	}
	if tick <= 0 {
		tick = time.Second
	}
	udid := b.UDID()
	if udid == "" {
		return fmt.Errorf("select an iPhone before playing route")
	}

	playCtx, cancel := context.WithCancel(ctx)
	b.mu.Lock()
	if b.cancel != nil {
		b.cancel()
	}
	b.playID++
	playID := b.playID
	b.cancel = cancel
	b.mu.Unlock()
	defer b.clearCancel(playID)

	return b.location.PlayRoute(playCtx, udid, points, tick)
}

func (b *Bridge) Stop() error {
	b.stopPlayback()
	return b.location.ClearLocation(context.Background(), b.UDID())
}

func (b *Bridge) stopPlayback() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
	b.playID++
}

func (b *Bridge) clearCancel(playID uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.playID == playID {
		b.cancel = nil
	}
}
