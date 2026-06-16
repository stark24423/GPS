package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"gpssim/internal/core"
	"gpssim/internal/iosbridge"
)

func (a *Application) start() {
	a.stateMu.Lock()
	if a.running {
		a.stateMu.Unlock()
		dialog.ShowInformation("Simulation active", "Stop the current simulation before starting another one.", a.window)
		return
	}
	a.stateMu.Unlock()

	points := a.selectedPoints()
	if len(points) == 0 {
		dialog.ShowInformation("Missing location", "Click the map to add at least one point first.", a.window)
		return
	}
	if a.modeSelect.Selected == modeRoute && len(points) < 2 {
		dialog.ShowInformation("Missing route", "Route mode requires at least two points.", a.window)
		return
	}

	jitter := 0.0
	if a.modeSelect.Selected == modeRoute {
		jitter = a.jitterSlider.Value
	}
	timed, err := core.BuildTimedPoints(points, a.speedSlider.Value, jitter)
	if err != nil {
		dialog.ShowError(err, a.window)
		return
	}

	if err := os.MkdirAll(a.outputDir, 0755); err != nil {
		dialog.ShowError(err, a.window)
		return
	}
	path := filepath.Join(a.outputDir, fmt.Sprintf("simulation_%s.gpx", time.Now().Format("20060102_150405")))
	if err := core.WriteGPX(path, timed, "GPS Simulation"); err != nil {
		dialog.ShowError(err, a.window)
		return
	}

	simulationPoints := core.TimedPointsCoordinates(timed)
	a.logf("GPX: %s", path)
	if jitter > 0 {
		a.logf("Route GPS jitter: up to %.1f m", jitter)
	}

	a.stopJoystick()
	a.setRunning(true)
	a.startPreview(simulationPoints)

	if a.bridgeSelect.Selected == bridgeIPhone {
		a.startIPhoneOperation(points, simulationPoints)
		return
	}

	a.logf("Dry-run complete. GPX was generated, but no iPhone location was changed.")
	a.setStatus("GPX generated")
}

func (a *Application) startIPhoneOperation(points, simulationPoints []core.Coordinate) {
	if a.modeSelect.Selected == modeSingle {
		point := points[0]
		a.logf("Sending location %.6f, %.6f to iPhone.", point.Lat, point.Lon)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			err := a.bridge.SetLocation(ctx, point)
			a.finishIPhoneOperation("Set iPhone location", err)
		}()
		return
	}

	a.logf("Playing route on iPhone (%d points).", len(simulationPoints))
	go func() {
		err := a.bridge.PlayRoute(context.Background(), simulationPoints, time.Second)
		a.finishIPhoneOperation("Play iPhone route", err)
	}()
}

func (a *Application) finishIPhoneOperation(name string, err error) {
	fyne.Do(func() {
		if err == nil {
			a.logf("%s: OK", name)
			a.setStatus(name + " OK")
			return
		}
		if errorsIsCanceled(err) {
			a.logf("%s: stopped", name)
			return
		}
		if iosbridge.IsLocationSimulationPending(err) {
			a.stopPreview()
			a.setRunning(false)
			a.setStatus("Tunnel ready - location pending")
			a.logf("%s pending: tunnel discovery succeeded, but DVT LocationSimulation is not implemented yet.", name)
			a.openLogWindow()
			dialog.ShowInformation(
				"Tunnel discovery ready",
				"目前程式會自動管理 iPhone tunnel。請打開 Logs 查看 RSD 位址與 debug log。",
				a.window,
			)
			return
		}
		a.logf("%s failed: %s", name, err)
		a.setStatus("iPhone error")
		dialog.ShowError(fmt.Errorf("%s failed: %s\n\nFull log: %s", name, compactError(err), a.logFilePath), a.window)
	})
}

func (a *Application) stop() {
	a.stopPreview()
	a.mapView.SetFollowMode(false)
	if a.bridgeSelect.Selected == bridgeIPhone {
		go func() {
			err := a.bridge.Stop()
			fyne.Do(func() {
				if err != nil {
					a.logf("Stopping iPhone simulation failed: %s", err)
					a.setStatus("Stop failed")
				} else {
					a.logf("Stopping iPhone simulation: location cleared; tunnel kept running")
					a.refreshTunnelStatus()
					a.setStatus("Location cleared")
				}
			})
		}()
	} else {
		a.logf("Dry-run stop complete. No iPhone location was changed.")
		a.setStatus("Stopped")
	}

	a.currentLabel.SetText("Current: stopped")
	points := a.selectedPoints()
	if len(points) > 0 {
		a.mapView.SetCurrentPosition(points[len(points)-1], "Stopped")
	}
	a.setRunning(false)
}

func (a *Application) startPreview(points []core.Coordinate) {
	a.stopPreview()
	if len(points) == 0 {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.stateMu.Lock()
	a.previewCancel = cancel
	a.stateMu.Unlock()

	a.mapView.SetFollowMode(true)
	a.mapView.SetCurrentPosition(points[0], "Simulating")
	a.currentLabel.SetText(fmt.Sprintf("Current: %.6f, %.6f", points[0].Lat, points[0].Lon))
	if len(points) == 1 {
		return
	}

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		index := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				index++
				if index >= len(points) {
					fyne.Do(func() {
						last := points[len(points)-1]
						a.mapView.SetCurrentPosition(last, "Arrived")
						a.currentLabel.SetText(fmt.Sprintf("Current: %.6f, %.6f", last.Lat, last.Lon))
						a.setStatus("Arrived - press Stop to clear")
					})
					return
				}
				point := points[index]
				fyne.Do(func() {
					a.currentLabel.SetText(fmt.Sprintf("Current: %.6f, %.6f", point.Lat, point.Lon))
					a.mapView.SetCurrentPosition(point, "Simulating")
				})
			}
		}
	}()
}

func (a *Application) stopPreview() {
	a.stateMu.Lock()
	cancel := a.previewCancel
	a.previewCancel = nil
	a.stateMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *Application) onJoystickDirection(dx, dy float64) {
	a.stateMu.Lock()
	a.joystickDX = dx
	a.joystickDY = dy
	running := a.running
	a.stateMu.Unlock()

	if dx == 0 && dy == 0 {
		a.stopJoystick()
		a.mapView.SetFollowMode(false)
		a.setStatus("Joystick paused")
		return
	}
	if running {
		a.logf("Joystick disabled while simulation is running.")
		return
	}

	a.stateMu.Lock()
	if a.joystickPosition == nil {
		if len(a.points) > 0 {
			last := a.points[len(a.points)-1]
			a.joystickPosition = &core.Coordinate{Lat: last.Lat, Lon: last.Lon}
		} else {
			a.joystickPosition = &core.Coordinate{Lat: 24.7808548, Lon: 121.0252718}
		}
	}
	alreadyActive := a.joystickCancel != nil
	a.stateMu.Unlock()

	a.setStatus("Joystick active")
	a.mapView.SetFollowMode(true)
	if !alreadyActive {
		a.startJoystick()
	}
}

func (a *Application) startJoystick() {
	ctx, cancel := context.WithCancel(context.Background())
	a.stateMu.Lock()
	a.joystickCancel = cancel
	a.stateMu.Unlock()

	go func() {
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.joystickTick()
			}
		}
	}()
}

func (a *Application) stopJoystick() {
	a.stateMu.Lock()
	cancel := a.joystickCancel
	a.joystickCancel = nil
	streamCancel := a.joystickStreamCancel
	a.joystickStreamCancel = nil
	a.joystickUpdates = nil
	a.stateMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if streamCancel != nil {
		streamCancel()
	}
}

func (a *Application) joystickTick() {
	a.stateMu.Lock()
	if a.joystickPosition == nil {
		a.stateMu.Unlock()
		return
	}
	current := *a.joystickPosition
	dx := a.joystickDX
	dy := a.joystickDY
	speed := a.joystickSpeed
	a.stateMu.Unlock()

	speedMps := speed * 1000 / 3600
	next := core.OffsetCoordinate(current, dx*speedMps*0.08, dy*speedMps*0.08)

	a.stateMu.Lock()
	a.joystickPosition = &next
	a.stateMu.Unlock()

	fyne.Do(func() {
		a.currentLabel.SetText(fmt.Sprintf("Current: %.6f, %.6f", next.Lat, next.Lon))
		a.mapView.SetCurrentPosition(next, "Joystick")
	})

	if a.bridgeSelect.Selected == bridgeIPhone {
		a.queueJoystickLocation(next)
	}
}

func (a *Application) onKeyboardMovement(event *fyne.KeyEvent) {
	if a.window.Canvas().Focused() != nil {
		return
	}

	var dx, dy float64
	switch strings.ToLower(string(event.Name)) {
	case strings.ToLower(string(fyne.KeyUp)), "w":
		dy = 1
	case strings.ToLower(string(fyne.KeyDown)), "s":
		dy = -1
	case strings.ToLower(string(fyne.KeyLeft)), "a":
		dx = -1
	case strings.ToLower(string(fyne.KeyRight)), "d":
		dx = 1
	default:
		return
	}
	a.keyboardNudge(dx, dy)
}

func (a *Application) keyboardNudge(dx, dy float64) {
	a.stateMu.Lock()
	if a.running {
		a.stateMu.Unlock()
		a.logf("Keyboard movement disabled while simulation is running.")
		return
	}
	if a.joystickPosition == nil {
		if len(a.points) > 0 {
			last := a.points[len(a.points)-1]
			a.joystickPosition = &core.Coordinate{Lat: last.Lat, Lon: last.Lon}
		} else {
			a.joystickPosition = &core.Coordinate{Lat: 24.7808548, Lon: 121.0252718}
		}
	}
	current := *a.joystickPosition
	speed := a.joystickSpeed
	a.stateMu.Unlock()

	speedMps := speed * 1000 / 3600
	next := core.OffsetCoordinate(current, dx*speedMps*0.35, dy*speedMps*0.35)

	a.stateMu.Lock()
	a.joystickPosition = &next
	a.stateMu.Unlock()

	a.currentLabel.SetText(fmt.Sprintf("Current: %.6f, %.6f", next.Lat, next.Lon))
	a.mapView.SetFollowMode(true)
	a.mapView.SetCurrentPosition(next, "Keyboard")
	a.setStatus("Keyboard movement")

	if a.bridgeSelect.Selected == bridgeIPhone {
		a.queueJoystickLocation(next)
	}
}

func (a *Application) queueJoystickLocation(point core.Coordinate) {
	updates := a.ensureJoystickLocationStream()
	if updates == nil {
		return
	}
	select {
	case updates <- point:
	default:
		select {
		case <-updates:
		default:
		}
		select {
		case updates <- point:
		default:
		}
	}
}

func (a *Application) ensureJoystickLocationStream() chan core.Coordinate {
	a.stateMu.Lock()
	if a.joystickUpdates != nil {
		updates := a.joystickUpdates
		a.stateMu.Unlock()
		return updates
	}
	ctx, cancel := context.WithCancel(context.Background())
	updates := make(chan core.Coordinate, 1)
	a.joystickStreamCancel = cancel
	a.joystickUpdates = updates
	a.stateMu.Unlock()

	go func() {
		err := a.bridge.StreamLatestLocation(ctx, updates, 250*time.Millisecond)
		fyne.Do(func() {
			a.stateMu.Lock()
			if a.joystickUpdates == updates {
				a.joystickUpdates = nil
				a.joystickStreamCancel = nil
			}
			a.stateMu.Unlock()
			if err != nil && !errorsIsCanceled(err) {
				a.stopJoystick()
				a.logf("Joystick iPhone stream error: %s", err)
				a.setStatus("iPhone error")
			}
		})
	}()
	return updates
}
