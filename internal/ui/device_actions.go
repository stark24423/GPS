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
	opID := a.nextOperationID("dev")
	started := time.Now()
	a.refreshButton.Disable()
	a.deviceLabel.SetText("Device: scanning...")
	a.setStatus("Scanning iPhone...")
	a.logf("[%s] device scan started", opID)
	go func() {
		devices, err := a.bridge.ListDevices()
		fyne.Do(func() {
			a.refreshButton.Enable()
			if err != nil {
				a.deviceLabel.SetText("Device: scan failed")
				a.setStatus("Device scan failed")
				a.logf("[%s] iPhone scan failed after %s: %s", opID, time.Since(started).Round(time.Millisecond), err)
				return
			}
			if len(devices) == 0 {
				a.deviceLabel.SetText("Device: no iPhone detected")
				a.tunnelLabel.SetText("Tunnel: no selected iPhone")
				a.setStatus("No iPhone detected")
				a.stopLocationKeepAlive()
				a.stopPreview()
				a.stopJoystick()
				a.bridge.SetUDID("")
				a.deviceSelect.Options = nil
				a.deviceSelect.ClearSelected()
				a.deviceSelect.Refresh()
				a.logf("[%s] no iPhone connected after %s; location changes are unavailable until a USB iPhone is selected", opID, time.Since(started).Round(time.Millisecond))
				return
			}

			labels := make([]string, 0, len(devices))
			a.deviceChoices = make(map[string]string, len(devices))
			for _, device := range devices {
				label := formatDeviceChoiceLabel(device.Name, device.Connection, device.ID)
				labels = append(labels, label)
				a.deviceChoices[label] = device.ID
			}
			a.deviceSelect.Options = labels
			selected := a.deviceSelect.Selected
			if _, ok := a.deviceChoices[selected]; !ok {
				selected = labels[0]
			}
			if a.bridge.UDID() != a.deviceChoices[selected] {
				a.stopLocationKeepAlive()
				a.stopPreview()
				a.stopJoystick()
			}
			a.deviceSelect.Selected = selected
			a.bridge.SetUDID(a.deviceChoices[selected])
			a.deviceSelect.Refresh()
			a.deviceLabel.SetText("Device: " + strings.Join(labels, ", "))
			a.refreshTunnelStatus()
			a.logf("[%s] detected device(s) after %s: %s", opID, time.Since(started).Round(time.Millisecond), strings.Join(labels, ", "))
			a.setStatus("iPhone ready")
			a.startTunnelForSelected(false)
		})
	}()
}

func formatDeviceChoiceLabel(name, connection, id string) string {
	return fmt.Sprintf("%s [%s] (%s)", name, displayDeviceConnection(connection), id)
}

func displayDeviceConnection(connection string) string {
	switch strings.ToLower(strings.TrimSpace(connection)) {
	case "usb":
		return "USB"
	case "network", "wifi", "wi-fi", "wireless":
		return "Wi-Fi"
	case "":
		return "Unknown"
	default:
		return connection
	}
}

func (a *Application) startTunnel() {
	a.startTunnelForSelected(true)
}

func (a *Application) startTunnelForSelected(showDialog bool) {
	opID := a.nextOperationID("tun")
	started := time.Now()
	if a.bridge.UDID() == "" {
		a.logf("[%s] tunnel start rejected: no selected iPhone", opID)
		if showDialog {
			dialog.ShowInformation("Missing iPhone", "Select an iPhone before starting the tunnel.", a.window)
		}
		return
	}
	if a.tunnelStartInFlight.Swap(true) {
		a.logf("[%s] tunnel start ignored: already in progress", opID)
		return
	}
	if showDialog {
		a.openLogWindow()
	}
	a.tunnelLabel.SetText("Tunnel: starting...")
	a.setStatus("Starting tunnel...")
	a.logf("[%s] starting built-in Go tunnel for selected iPhone", opID)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		info, err := a.bridge.StartTunnel(ctx)
		fyne.Do(func() {
			a.tunnelStartInFlight.Store(false)
			if err != nil {
				a.tunnelLabel.SetText("Tunnel: " + err.Error())
				a.setStatus("Tunnel error")
				a.logf("[%s] start tunnel failed after %s: %s", opID, time.Since(started).Round(time.Millisecond), err)
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
			a.logf("[%s] tunnel ready after %s", opID, time.Since(started).Round(time.Millisecond))
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
	opID := a.nextOperationID("tun-stop")
	started := time.Now()
	a.logf("[%s] stopping built-in Go tunnel", opID)
	go func() {
		err := a.bridge.StopTunnel()
		fyne.Do(func() {
			if err != nil {
				a.logf("[%s] stop tunnel failed after %s: %s", opID, time.Since(started).Round(time.Millisecond), err)
				dialog.ShowError(err, a.window)
				return
			}
			a.tunnelLabel.SetText("Tunnel: stopped")
			a.setStatus("Tunnel stopped")
			a.logf("[%s] tunnel stopped after %s", opID, time.Since(started).Round(time.Millisecond))
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
