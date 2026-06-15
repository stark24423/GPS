package iostunnel

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"gpssim/internal/ioscore/lockdown"
	"gpssim/internal/ioscore/usbmux"
	"gpssim/internal/iostunnel/coredevice"
	"gpssim/internal/iostunnel/wintun"
)

const minimumIOSVersion = "17.4"

var (
	ErrUnsupportedPlatform  = errors.New("built-in Go tunnel currently supports Windows only")
	ErrWintunMissing        = errors.New("Wintun DLL was not found next to gps-simulator-go.exe or in the DLL search path")
	ErrAdminRequired        = errors.New("Administrator privileges are required to create the tunnel adapter")
	ErrIOSVersion           = errors.New("iOS 17.4+ required for built-in Go tunnel")
	ErrTunnelForwardPending = errors.New("built-in Wintun packet forwarding is not complete yet")
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
	active  map[string]*activeTunnel
	wintun  wintun.Loader
	isAdmin func() bool
	logger  func(format string, args ...any)
}

type activeTunnel struct {
	info   TunnelInfo
	cancel context.CancelFunc
	done   chan struct{}
	tun    wintun.Device
	stream net.Conn
	client *lockdown.Client
}

func NewManager() *Manager {
	return &Manager{
		mux:     usbmux.New(""),
		active:  make(map[string]*activeTunnel),
		wintun:  wintun.SystemLoader{},
		isAdmin: IsAdministrator,
	}
}

func (m *Manager) SetLogger(logger func(format string, args ...any)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logger = logger
}

func (m *Manager) Start(ctx context.Context, udid string) (TunnelInfo, error) {
	startedAt := time.Now()
	m.logf("Tunnel discovery requested: udid=%s", udid)
	if udid == "" {
		m.logf("Tunnel discovery failed: missing UDID")
		return TunnelInfo{}, fmt.Errorf("UDID is required")
	}
	if runtime.GOOS != "windows" {
		m.logf("Tunnel discovery failed: unsupported platform=%s", runtime.GOOS)
		return TunnelInfo{}, ErrUnsupportedPlatform
	}
	m.logf("Tunnel preflight: platform=%s", runtime.GOOS)
	if !m.wintun.Available() {
		m.logf("Tunnel preflight failed: wintun.dll was not loadable")
		return TunnelInfo{}, ErrWintunMissing
	}
	m.logf("Tunnel preflight: wintun.dll loadable")
	if !m.isAdmin() {
		m.logf("Tunnel preflight failed: process is not elevated")
		return TunnelInfo{}, ErrAdminRequired
	}
	m.logf("Tunnel preflight: administrator privileges confirmed")
	m.mu.Lock()
	if active, ok := m.active[udid]; ok {
		info := active.info
		m.mu.Unlock()
		m.logf("Tunnel already active: state=%s rsd=%s:%d", info.State, info.RSDAddress, info.RSDPort)
		return info, nil
	}
	m.mu.Unlock()

	m.logf("Tunnel discovery: searching USB device")
	device, values, err := m.findDevice(ctx, udid)
	if err != nil {
		m.logf("Tunnel discovery failed: %s", err)
		return TunnelInfo{}, err
	}
	m.logf("Tunnel discovery: device found name=%s ios=%s connection=%s", values.DeviceName, values.ProductVersion, device.ConnectionType)
	if !versionAtLeast(values.ProductVersion, minimumIOSVersion) {
		m.logf("Tunnel discovery failed: iOS version %s is below %s", values.ProductVersion, minimumIOSVersion)
		return TunnelInfo{}, fmt.Errorf("%w: detected %s", ErrIOSVersion, values.ProductVersion)
	}

	m.logf("Tunnel discovery: connecting lockdown")
	client, err := lockdown.Dial(ctx, m.mux, device)
	if err != nil {
		m.logf("Tunnel discovery failed: lockdown dial: %s", err)
		return TunnelInfo{}, err
	}

	m.logf("Tunnel discovery: starting CoreDeviceProxy service")
	stream, err := client.StartCoreDeviceProxy()
	if err != nil {
		_ = client.Close()
		m.logf("Tunnel discovery failed: CoreDeviceProxy start: %s", err)
		return TunnelInfo{}, err
	}

	m.logf("Tunnel discovery: exchanging CoreDevice tunnel parameters")
	params, err := coredevice.ExchangeParameters(stream)
	if err != nil {
		_ = stream.Close()
		_ = client.Close()
		m.logf("Tunnel discovery failed: CoreDevice handshake: %s", err)
		return TunnelInfo{}, err
	}
	m.logf("Tunnel discovery: RSD ready server=%s port=%d client=%s mtu=%d",
		params.ServerAddress,
		params.ServerRSDPort,
		params.ClientParameters.Address,
		params.ClientParameters.MTU,
	)

	interfaceName := "gpssim-" + shortUDID(device.SerialNumber)
	m.logf("Tunnel transport: creating Wintun adapter name=%s client=%s server=%s mtu=%d",
		interfaceName,
		params.ClientParameters.Address,
		params.ServerAddress,
		params.ClientParameters.MTU,
	)
	tun, err := wintun.Open(wintun.Config{
		Name:          interfaceName,
		ClientAddress: params.ClientParameters.Address,
		ServerAddress: params.ServerAddress,
		MTU:           int(params.ClientParameters.MTU),
	})
	if err != nil {
		_ = stream.Close()
		_ = client.Close()
		m.logf("Tunnel transport failed: Wintun open/configure: %s", err)
		return TunnelInfo{}, err
	}

	info := TunnelInfo{
		UDID:          device.SerialNumber,
		InterfaceName: tun.Name(),
		RSDAddress:    params.ServerAddress,
		RSDPort:       int(params.ServerRSDPort),
		MTU:           int(params.ClientParameters.MTU),
		StartedAt:     time.Now(),
		State:         "forwarding",
		Message:       "CoreDeviceProxy tunnel is forwarding IPv6 packets through Wintun.",
	}
	tunnelCtx, cancel := context.WithCancel(context.Background())
	active := &activeTunnel{
		info:   info,
		cancel: cancel,
		done:   make(chan struct{}),
		tun:    tun,
		stream: stream,
		client: client,
	}
	m.mu.Lock()
	m.active[udid] = active
	m.mu.Unlock()
	go m.forwardLoop(tunnelCtx, active)
	m.logf("Tunnel discovery complete in %s: state=%s", time.Since(startedAt).Round(time.Millisecond), info.State)
	return info, nil
}

func (m *Manager) EnsureRunning(ctx context.Context, udid string) (TunnelInfo, error) {
	m.mu.Lock()
	if info, ok := m.active[udid]; ok {
		m.mu.Unlock()
		return info.info, nil
	}
	m.mu.Unlock()
	return m.Start(ctx, udid)
}

func (m *Manager) Info(udid string) (TunnelInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if udid != "" {
		active, ok := m.active[udid]
		if !ok {
			return TunnelInfo{}, false
		}
		return active.info, true
	}
	for _, active := range m.active {
		return active.info, true
	}
	return TunnelInfo{}, false
}

func (m *Manager) Stop(udid string) error {
	m.logf("Tunnel stop requested: udid=%s", udid)
	var targets []*activeTunnel
	m.mu.Lock()
	if udid == "" {
		for _, active := range m.active {
			targets = append(targets, active)
		}
		m.active = make(map[string]*activeTunnel)
		m.mu.Unlock()
	} else if active, ok := m.active[udid]; ok {
		targets = append(targets, active)
		delete(m.active, udid)
		m.mu.Unlock()
	} else {
		m.mu.Unlock()
	}
	for _, active := range targets {
		if active.cancel != nil {
			active.cancel()
		}
		if active.stream != nil {
			_ = active.stream.Close()
		}
		select {
		case <-active.done:
		case <-time.After(2 * time.Second):
			m.logf("Tunnel forwarding cleanup timed out; closing adapter anyway")
		}
		if active.tun != nil {
			_ = active.tun.Close()
		}
		if active.client != nil {
			_ = active.client.Close()
		}
	}
	m.logf("Tunnel status cleared: udid=%s", udid)
	return nil
}

func (m *Manager) Status() []TunnelInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := make([]TunnelInfo, 0, len(m.active))
	for _, info := range m.active {
		status = append(status, info.info)
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

func (m *Manager) logf(format string, args ...any) {
	m.mu.Lock()
	logger := m.logger
	m.mu.Unlock()
	if logger != nil {
		logger(format, args...)
	}
}

func (m *Manager) forwardLoop(ctx context.Context, active *activeTunnel) {
	defer func() {
		close(active.done)
		if ctx.Err() == nil {
			m.dropActive(active.info.UDID)
			if active.tun != nil {
				_ = active.tun.Close()
			}
			if active.client != nil {
				_ = active.client.Close()
			}
		}
	}()
	errs := make(chan error, 2)
	go func() {
		errs <- copyTunToDevice(ctx, active.tun, active.stream)
	}()
	go func() {
		errs <- copyDeviceToTun(ctx, active.stream, active.tun)
	}()
	for completed := 0; completed < 2; completed++ {
		err := <-errs
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
			m.logf("Tunnel forwarding ended: %s", err)
		}
	}
	if ctx.Err() != nil {
		m.logf("Tunnel forwarding stopped: %s", ctx.Err())
		return
	}
	m.logf("Tunnel forwarding ended")
}

func (m *Manager) dropActive(udid string) {
	m.mu.Lock()
	active, ok := m.active[udid]
	if ok && active.info.UDID == udid {
		delete(m.active, udid)
	}
	m.mu.Unlock()
	if ok {
		m.logf("Tunnel marked inactive: udid=%s", udid)
	}
}

func copyTunToDevice(ctx context.Context, tun wintun.Device, stream net.Conn) error {
	for {
		packet, err := tun.ReadPacket(ctx)
		if err != nil {
			return err
		}
		if len(packet) == 0 || packet[0]>>4 != 6 {
			continue
		}
		if _, err := stream.Write(packet); err != nil {
			return err
		}
	}
}

func copyDeviceToTun(ctx context.Context, stream net.Conn, tun wintun.Device) error {
	header := make([]byte, 40)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if _, err := io.ReadFull(stream, header); err != nil {
			return err
		}
		payloadLen := int(binary.BigEndian.Uint16(header[4:6]))
		packet := make([]byte, 40+payloadLen)
		copy(packet, header)
		if _, err := io.ReadFull(stream, packet[40:]); err != nil {
			return err
		}
		if err := tun.WritePacket(packet); err != nil {
			return err
		}
	}
}

func shortUDID(udid string) string {
	if len(udid) <= 8 {
		return udid
	}
	return udid[len(udid)-8:]
}
