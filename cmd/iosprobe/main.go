package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"gpssim/internal/core"
	"gpssim/internal/ioscore/lockdown"
	"gpssim/internal/ioscore/usbmux"
	"gpssim/internal/ioslocation"
	"gpssim/internal/iostunnel"
	"gpssim/internal/iostunnel/coredevice"
)

func main() {
	useManager := flag.Bool("manager", false, "run the production TunnelManager path")
	set101 := flag.Bool("set101", false, "set iPhone simulated location to Taipei 101 through the production path")
	targetUDID := flag.String("udid", "", "target iPhone UDID; defaults to the first USB device")
	flag.Parse()

	timeout := 20 * time.Second
	if *set101 {
		timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	mux := usbmux.New("")
	devices, err := mux.ListDevices(ctx)
	if err != nil {
		fatal("list devices", err)
	}
	if len(devices) == 0 {
		fmt.Println("No USB iPhone detected.")
		return
	}

	for index, device := range devices {
		fmt.Printf("[%d] UDID=%s DeviceID=%d Connection=%s\n", index, device.SerialNumber, device.DeviceID, device.ConnectionType)
	}

	device := devices[0]
	if *targetUDID != "" {
		found := false
		for _, candidate := range devices {
			if candidate.SerialNumber == *targetUDID {
				device = candidate
				found = true
				break
			}
		}
		if !found {
			fatal("select device", fmt.Errorf("UDID %s was not found", *targetUDID))
		}
	}

	if *useManager || *set101 {
		runManagerProbe(ctx, device.SerialNumber, *set101)
		return
	}

	client, err := lockdown.Dial(ctx, mux, device)
	if err != nil {
		fatal("connect lockdown", err)
	}
	values, err := client.QueryValues()
	if err != nil {
		_ = client.Close()
		fatal("query values", err)
	}
	fmt.Printf("DeviceName=%s ProductVersion=%s DeveloperMode=%s\n", values.DeviceName, values.ProductVersion, values.DeveloperStatus)

	// 這個預設 probe 只驗證 CoreDeviceProxy handshake，不建立 Wintun adapter。
	stream, err := client.StartCoreDeviceProxy()
	_ = client.Close()
	if err != nil {
		fatal("start CoreDeviceProxy", err)
	}
	defer stream.Close()

	parameters, err := coredevice.ExchangeParameters(stream)
	if err != nil {
		fatal("CoreDevice handshake", err)
	}
	fmt.Printf("CoreDevice handshake OK: server=%s rsd=%d client=%s mtu=%d\n",
		parameters.ServerAddress,
		parameters.ServerRSDPort,
		parameters.ClientParameters.Address,
		parameters.ClientParameters.MTU,
	)
}

func runManagerProbe(ctx context.Context, udid string, set101 bool) {
	manager := iostunnel.NewManager()
	manager.SetLogger(func(format string, args ...any) {
		fmt.Printf(format+"\n", args...)
	})
	info, err := manager.Start(ctx, udid)
	if err != nil {
		fatal("manager start tunnel", err)
	}
	fmt.Printf("TunnelManager OK: state=%s interface=%s rsd=%s:%d mtu=%d message=%s\n",
		info.State,
		info.InterfaceName,
		info.RSDAddress,
		info.RSDPort,
		info.MTU,
		info.Message,
	)
	if set101 {
		location := ioslocation.New(manager)
		location.SetLogger(func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		})
		point := core.Coordinate{Lat: 25.033964, Lon: 121.564468}
		fmt.Printf("Setting Taipei 101 location: %.6f, %.6f\n", point.Lat, point.Lon)
		if err := location.SetLocation(ctx, udid, point); err != nil {
			fatal("set Taipei 101", err)
		}
		fmt.Println("Set Taipei 101 OK.")
	}
}

func fatal(step string, err error) {
	fmt.Fprintf(os.Stderr, "%s failed: %v\n", step, err)
	os.Exit(1)
}
