package ui

import (
	"context"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"strings"
	"time"
)

func (a *Application) refreshDevices() {
	a.refreshButton.Disable()
	a.deviceLabel.SetText("Device: scanning...")
	a.setStatus("Scanning iPhone...")
	go func() {
		devices, err := a.bridge.ListDevices()
		fyne.Do(func() {
			a.refreshButton.Enable()
			if err != nil {
				a.deviceLabel.SetText("Device: scan failed")
				a.setStatus("Device scan failed")
				a.logf("iPhone scan failed: %s", err)
				return
			}
			if len(devices) == 0 {
				a.deviceLabel.SetText("Device: no iPhone detected")
				a.tunnelLabel.SetText("Tunnel: no selected iPhone")
				a.setStatus("No iPhone detected")
				a.deviceSelect.Options = nil
				a.deviceSelect.ClearSelected()
				a.deviceSelect.Refresh()
				a.logf("No iPhone connected. Dry-run remains available.")
				return
			}

			labels := make([]string, 0, len(devices))
			a.deviceChoices = make(map[string]string, len(devices))
			for _, device := range devices {
				label := fmt.Sprintf("%s (%s)", device.Name, device.ID)
				labels = append(labels, label)
				a.deviceChoices[label] = device.ID
			}
			a.deviceSelect.Options = labels
			a.deviceSelect.SetSelected(labels[0])
			a.deviceSelect.Refresh()
			a.deviceLabel.SetText("Device: " + strings.Join(labels, ", "))
			a.refreshTunnelStatus()
			a.logf("Detected device(s): %s", strings.Join(labels, ", "))
			if a.bridgeSelect.Selected == bridgeDryRun {
				a.bridgeSelect.SetSelected(bridgeIPhone)
			} else {
				a.setStatus("iPhone ready")
			}
		})
	}()
}

func (a *Application) startTunnel() {
	a.startTunnelForSelected(true)
}

func (a *Application) startTunnelForSelected(showDialog bool) {
	if a.bridge.UDID() == "" {
		if showDialog {
			dialog.ShowInformation("Missing iPhone", "Select an iPhone before starting the tunnel.", a.window)
		}
		return
	}
	if a.tunnelStartInFlight.Swap(true) {
		a.logf("Tunnel start already in progress.")
		return
	}
	if a.selectedTunnelActive() {
		a.tunnelStartInFlight.Store(false)
		a.refreshTunnelStatus()
		a.logf("Tunnel already active for selected iPhone.")
		return
	}
	if showDialog {
		a.openLogWindow()
	}
	a.tunnelLabel.SetText("Tunnel: starting...")
	a.setStatus("Starting tunnel...")
	a.logf("Starting built-in Go tunnel for selected iPhone.")

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		info, err := a.bridge.StartTunnel(ctx)
		fyne.Do(func() {
			a.tunnelStartInFlight.Store(false)
			if err != nil {
				a.tunnelLabel.SetText("Tunnel: " + err.Error())
				a.setStatus("Tunnel error")
				a.logf("Start tunnel failed: %s", err)
				if showDialog {
					dialog.ShowError(err, a.window)
				}
				return
			}
			a.tunnelLabel.SetText(fmt.Sprintf("Tunnel: %s %s:%d", info.State, info.RSDAddress, info.RSDPort))
			a.setStatus("Tunnel ready")
			a.logf("Tunnel discovery result: udid=%s interface=%s state=%s rsd=%s:%d mtu=%d message=%s",
				info.UDID,
				info.InterfaceName,
				info.State,
				info.RSDAddress,
				info.RSDPort,
				info.MTU,
				info.Message,
			)
		})
	}()
}

func (a *Application) selectedTunnelActive() bool {
	udid := a.bridge.UDID()
	if udid == "" {
		return false
	}
	for _, info := range a.bridge.TunnelStatus() {
		if info.UDID == udid {
			return true
		}
	}
	return false
}

func (a *Application) stopTunnel() {
	a.logf("Stopping built-in Go tunnel.")
	go func() {
		err := a.bridge.StopTunnel()
		fyne.Do(func() {
			if err != nil {
				a.logf("Stop tunnel failed: %s", err)
				dialog.ShowError(err, a.window)
				return
			}
			a.tunnelLabel.SetText("Tunnel: stopped")
			a.setStatus("Tunnel stopped")
			a.logf("Tunnel stopped.")
		})
	}()
}

func (a *Application) refreshTunnelStatus() {
	status := a.bridge.TunnelStatus()
	if len(status) == 0 {
		a.tunnelLabel.SetText("Tunnel: built-in Go tunnel not started")
		return
	}
	info := status[0]
	a.tunnelLabel.SetText(fmt.Sprintf("Tunnel: %s %s:%d", info.State, info.RSDAddress, info.RSDPort))
}

func (a *Application) logRequirements() {
	go func() {
		for _, requirement := range a.bridge.CheckRequirements() {
			status := "WARN"
			if requirement.OK {
				status = "OK"
			}
			a.logf("[%s] %s: %s", status, requirement.Name, requirement.Message)
		}
	}()
}
