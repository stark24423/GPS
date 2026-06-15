//go:build windows

package wintun

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

const (
	ringCapacity     = 0x400000
	errorNoMoreItems = syscall.Errno(259)
)

func (SystemLoader) Available() bool {
	// 只檢查 DLL 是否能被 Windows loader 找到；真正建立 adapter 會在 tunnel transport 階段處理。
	dll := syscall.NewLazyDLL("wintun.dll")
	return dll.Load() == nil
}

type device struct {
	name    string
	dll     *syscall.LazyDLL
	adapter uintptr
	session uintptr
	event   syscall.Handle
}

func Open(config Config) (Device, error) {
	if config.Name == "" {
		return nil, fmt.Errorf("wintun adapter name is required")
	}
	dll := syscall.NewLazyDLL("wintun.dll")
	if err := dll.Load(); err != nil {
		return nil, err
	}

	name, err := syscall.UTF16PtrFromString(config.Name)
	if err != nil {
		return nil, err
	}
	tunnelType, err := syscall.UTF16PtrFromString("Wintun")
	if err != nil {
		return nil, err
	}

	createAdapter := dll.NewProc("WintunCreateAdapter")
	adapter, _, callErr := createAdapter.Call(
		uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(tunnelType)),
		0,
	)
	if adapter == 0 {
		return nil, fmt.Errorf("WintunCreateAdapter: %w", syscall.Errno(callErr.(syscall.Errno)))
	}

	startSession := dll.NewProc("WintunStartSession")
	session, _, callErr := startSession.Call(adapter, ringCapacity)
	if session == 0 {
		dll.NewProc("WintunCloseAdapter").Call(adapter)
		return nil, fmt.Errorf("WintunStartSession: %w", syscall.Errno(callErr.(syscall.Errno)))
	}

	getReadWaitEvent := dll.NewProc("WintunGetReadWaitEvent")
	event, _, _ := getReadWaitEvent.Call(session)
	d := &device{
		name:    config.Name,
		dll:     dll,
		adapter: adapter,
		session: session,
		event:   syscall.Handle(event),
	}
	if err := d.configure(config); err != nil {
		_ = d.Close()
		return nil, err
	}
	return d, nil
}

func (d *device) Name() string {
	return d.name
}

func (d *device) ReadPacket(ctx context.Context) ([]byte, error) {
	receivePacket := d.dll.NewProc("WintunReceivePacket")
	releasePacket := d.dll.NewProc("WintunReleaseReceivePacket")

	for {
		var size uint32
		ptr, _, callErr := receivePacket.Call(d.session, uintptr(unsafe.Pointer(&size)))
		if ptr != 0 {
			packet := make([]byte, size)
			copy(packet, unsafe.Slice((*byte)(unsafe.Pointer(ptr)), size))
			releasePacket.Call(d.session, ptr)
			return packet, nil
		}
		errno, _ := callErr.(syscall.Errno)
		if errno != errorNoMoreItems {
			return nil, errno
		}
		if err := waitForEvent(ctx, d.event); err != nil {
			return nil, err
		}
	}
}

func (d *device) WritePacket(packet []byte) error {
	allocatePacket := d.dll.NewProc("WintunAllocateSendPacket")
	sendPacket := d.dll.NewProc("WintunSendPacket")
	ptr, _, callErr := allocatePacket.Call(d.session, uintptr(len(packet)))
	if ptr == 0 {
		errno, _ := callErr.(syscall.Errno)
		return errno
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(ptr)), len(packet)), packet)
	sendPacket.Call(d.session, ptr)
	return nil
}

func (d *device) Close() error {
	if d.session != 0 {
		d.dll.NewProc("WintunEndSession").Call(d.session)
		d.session = 0
	}
	if d.adapter != 0 {
		d.dll.NewProc("WintunCloseAdapter").Call(d.adapter)
		d.adapter = 0
	}
	return nil
}

func (d *device) configure(config Config) error {
	if ip := net.ParseIP(config.ClientAddress); ip == nil || ip.To16() == nil || ip.To4() != nil {
		return fmt.Errorf("invalid client IPv6 address %q", config.ClientAddress)
	}
	if ip := net.ParseIP(config.ServerAddress); ip == nil || ip.To16() == nil || ip.To4() != nil {
		return fmt.Errorf("invalid server IPv6 address %q", config.ServerAddress)
	}
	if config.MTU <= 0 {
		config.MTU = 1280
	}

	// netsh 會依介面名稱設定 IPv6 位址與路由。這裡先用外部 Windows 工具，
	// 避免在第一版同時實作大量 IP Helper API binding。
	commands := [][]string{
		{"interface", "ipv6", "set", "interface", d.name, fmt.Sprintf("mtu=%d", config.MTU), "store=active"},
		{"interface", "ipv6", "add", "address", d.name, config.ClientAddress, "store=active"},
		{"interface", "ipv6", "add", "route", config.ServerAddress + "/128", d.name, "store=active"},
	}
	for _, args := range commands {
		cmd := exec.Command("netsh", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("netsh %v: %w: %s", args, err, string(out))
		}
	}
	return nil
}

func waitForEvent(ctx context.Context, event syscall.Handle) error {
	if event == 0 {
		return errors.New("wintun read wait event is missing")
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		result, err := syscall.WaitForSingleObject(event, 100)
		if err != nil {
			return err
		}
		switch result {
		case syscall.WAIT_OBJECT_0:
			return nil
		case syscall.WAIT_TIMEOUT:
			runtime.Gosched()
			time.Sleep(1 * time.Millisecond)
		default:
			return fmt.Errorf("WaitForSingleObject returned %d", result)
		}
	}
}
