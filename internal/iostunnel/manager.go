package iostunnel

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"gpssim/internal/ioscore/lockdown"
	"gpssim/internal/ioscore/usbmux"
	"gpssim/internal/iostunnel/wintun"
)

const minimumIOSVersion = "17.4"

var (
	ErrUnsupportedPlatform = errors.New("built-in Go tunnel currently supports Windows only")
	ErrWintunMissing       = errors.New("Wintun DLL was not found next to gps-simulator-go.exe or in the DLL search path")
	ErrAdminRequired       = errors.New("Administrator privileges are required to create the tunnel adapter")
	ErrIOSVersion          = errors.New("iOS 17.4+ required for built-in Go tunnel")
	ErrTunnelCorePending   = errors.New("built-in CoreDeviceProxy tunnel transport is not complete yet")
)

type TunnelInfo struct {
	UDID          string
	InterfaceName string
	RSDAddress    string
	RSDPort       int
	MTU           int
	StartedAt     time.Time
	State         string
	Message       string
}

type Manager struct {
	mu      sync.Mutex
	mux     *usbmux.Client
	active  map[string]TunnelInfo
	wintun  wintun.Loader
	isAdmin func() bool
}

func NewManager() *Manager {
	return &Manager{
		mux:     usbmux.New(""),
		active:  make(map[string]TunnelInfo),
		wintun:  wintun.SystemLoader{},
		isAdmin: IsAdministrator,
	}
}

func (m *Manager) Start(ctx context.Context, udid string) (TunnelInfo, error) {
	if udid == "" {
		return TunnelInfo{}, fmt.Errorf("UDID is required")
	}
	if runtime.GOOS != "windows" {
		return TunnelInfo{}, ErrUnsupportedPlatform
	}
	if !m.wintun.Available() {
		return TunnelInfo{}, ErrWintunMissing
	}
	if !m.isAdmin() {
		return TunnelInfo{}, ErrAdminRequired
	}

	device, values, err := m.findDevice(ctx, udid)
	if err != nil {
		return TunnelInfo{}, err
	}
	if !versionAtLeast(values.ProductVersion, minimumIOSVersion) {
		return TunnelInfo{}, fmt.Errorf("%w: detected %s", ErrIOSVersion, values.ProductVersion)
	}

	// 第一版先把前置條件與裝置資訊都驗證完成，再回報 tunnel transport 尚未完成。
	// 這可避免 UI 誤判成真機定位已可用，也方便下一階段接上 CoreDeviceProxy 封包轉送。
	return TunnelInfo{
		UDID:      device.SerialNumber,
		StartedAt: time.Now(),
		State:     "pending-coredevice",
		Message:   ErrTunnelCorePending.Error(),
	}, ErrTunnelCorePending
}

func (m *Manager) EnsureRunning(ctx context.Context, udid string) (TunnelInfo, error) {
	m.mu.Lock()
	if info, ok := m.active[udid]; ok {
		m.mu.Unlock()
		return info, nil
	}
	m.mu.Unlock()
	return m.Start(ctx, udid)
}

func (m *Manager) Stop(udid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if udid == "" {
		m.active = make(map[string]TunnelInfo)
		return nil
	}
	delete(m.active, udid)
	return nil
}

func (m *Manager) Status() []TunnelInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := make([]TunnelInfo, 0, len(m.active))
	for _, info := range m.active {
		status = append(status, info)
	}
	return status
}

func (m *Manager) findDevice(ctx context.Context, udid string) (usbmux.Device, lockdown.DeviceValues, error) {
	devices, err := m.mux.ListDevices(ctx)
	if err != nil {
		return usbmux.Device{}, lockdown.DeviceValues{}, err
	}
	for _, device := range devices {
		if device.SerialNumber != udid {
			continue
		}
		client, err := lockdown.Dial(ctx, m.mux, device)
		if err != nil {
			return usbmux.Device{}, lockdown.DeviceValues{}, err
		}
		defer client.Close()
		values, err := client.QueryValues()
		if err != nil {
			return usbmux.Device{}, lockdown.DeviceValues{}, err
		}
		return device, values, nil
	}
	return usbmux.Device{}, lockdown.DeviceValues{}, fmt.Errorf("iPhone %s not found over USB", udid)
}

func versionAtLeast(version, minimum string) bool {
	parts := parseVersion(version)
	minParts := parseVersion(minimum)
	for i := 0; i < len(minParts); i++ {
		if parts[i] > minParts[i] {
			return true
		}
		if parts[i] < minParts[i] {
			return false
		}
	}
	return true
}

func parseVersion(version string) [3]int {
	var out [3]int
	fields := strings.Split(version, ".")
	for i := 0; i < len(fields) && i < len(out); i++ {
		n, _ := strconv.Atoi(fields[i])
		out[i] = n
	}
	return out
}
