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

const (
	generatedGPXPattern     = "simulation_*.gpx"
	generatedGPXKeep        = 25
	singleLocationTimeout   = 90 * time.Second
	locationKeepAliveEvery  = 3 * time.Second
	locationKeepAliveSend   = 20 * time.Second
	routePlaybackGrace      = 60 * time.Second
	minRoutePlaybackTimeout = 2 * time.Minute
)

func (a *Application) start() {
	opID := a.nextOperationID("sim")
	started := time.Now()
	a.logf("[%s] start requested mode=%s target=%s", opID, a.modeSelect.Selected, bridgeIPhone)

	a.stateMu.Lock()
	if a.running {
		a.stateMu.Unlock()
		a.logf("[%s] ignored: simulation already active", opID)
		dialog.ShowInformation("Simulation active", "Stop the current simulation before starting another one.", a.window)
		return
	}
	a.stateMu.Unlock()

	points := a.selectedPoints()
	a.logf("[%s] selected points=%d %s", opID, len(points), coordinateSummary(points))
	if len(points) == 0 {
		a.logf("[%s] rejected: missing location", opID)
		dialog.ShowInformation("Missing location", "Click the map to add at least one point first.", a.window)
		return
	}
	if a.modeSelect.Selected == modeRoute && len(points) < 2 {
		a.logf("[%s] rejected: route mode requires at least two points", opID)
		dialog.ShowInformation("Missing route", "Route mode requires at least two points.", a.window)
		return
	}
	if a.bridge.UDID() == "" {
		a.logf("[%s] rejected: no selected iPhone", opID)
		a.setStatus("No iPhone detected")
		dialog.ShowInformation("Missing iPhone", "Select an iPhone before changing location.", a.window)
		return
	}

	jitter := 0.0
	if a.modeSelect.Selected == modeRoute {
		jitter = a.jitterSlider.Value
	}
	a.logf("[%s] build timed points speed=%.1fkm/h jitter=%.1fm", opID, a.speedSlider.Value, jitter)
	timed, err := core.BuildTimedPoints(points, a.speedSlider.Value, jitter)
	if err != nil {
		a.logOperationDone(opID, started, err)
		dialog.ShowError(err, a.window)
		return
	}

	path, err := a.writeOperationGPX(opID, timed)
	if err != nil {
		a.logOperationDone(opID, started, err)
		dialog.ShowError(err, a.window)
		return
	}

	simulationPoints := core.TimedPointsCoordinates(timed)
	a.logf("[%s] simulation points=%d gpx=%s", opID, len(simulationPoints), path)
	if jitter > 0 {
		a.logf("[%s] route GPS jitter: up to %.1f m", opID, jitter)
	}

	a.stopLocationKeepAlive()
	a.stopJoystick()
	a.setRunning(true)
	a.startIPhoneOperation(opID, started, points, simulationPoints)
}

func (a *Application) writeOperationGPX(opID string, timed []core.TimedPoint) (string, error) {
	if err := os.MkdirAll(a.outputDir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(a.outputDir, fmt.Sprintf("simulation_%s_%s.gpx", time.Now().Format("20060102_150405_000"), opID))
	if err := core.WriteGPX(path, timed, "GPS Simulation"); err != nil {
		return "", err
	}
	deleted, err := core.PruneGeneratedFiles(a.outputDir, generatedGPXPattern, generatedGPXKeep)
	if err != nil {
		a.logf("[%s] GPX retention warning: %s", opID, err)
	} else if deleted > 0 {
		a.logf("[%s] GPX retention pruned old files=%d keep=%d pattern=%s", opID, deleted, generatedGPXKeep, generatedGPXPattern)
	}
	return path, nil
}

func (a *Application) startIPhoneOperation(opID string, started time.Time, points, simulationPoints []core.Coordinate) {
	if a.modeSelect.Selected == modeSingle {
		point := points[0]
		a.setStatus("Location: preparing")
		a.logf("[%s] sending single point to iPhone lat=%.6f lon=%.6f timeout=%s", opID, point.Lat, point.Lon, singleLocationTimeout)
		ctx, cancel := context.WithTimeout(context.Background(), singleLocationTimeout)
		a.setOperationCancel(opID, cancel)
		go func() {
			defer cancel()
			defer a.clearOperationCancel(opID)
			err := a.bridge.SetLocation(ctx, point)
			a.finishIPhoneOperation(opID, started, "Set iPhone location", err, []core.Coordinate{point})
		}()
		return
	}

	a.setStatus("Location: preparing route")
	timeout := routePlaybackTimeout(len(simulationPoints), core.DefaultRouteTick)
	a.logf("[%s] playing route on iPhone points=%d tick=%s timeout=%s", opID, len(simulationPoints), core.DefaultRouteTick, timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	a.setOperationCancel(opID, cancel)
	a.startRouteTracker(ctx, opID, simulationPoints, core.DefaultRouteTick)
	go func() {
		defer cancel()
		defer a.clearOperationCancel(opID)
		defer a.stopPreview()
		err := a.bridge.PlayRoute(ctx, simulationPoints, core.DefaultRouteTick)
		a.finishIPhoneOperation(opID, started, "Play iPhone route", err, simulationPoints)
	}()
}

func routePlaybackTimeout(points int, tick time.Duration) time.Duration {
	if points < 1 {
		return minRoutePlaybackTimeout
	}
	timeout := time.Duration(points)*tick + routePlaybackGrace
	if timeout < minRoutePlaybackTimeout {
		return minRoutePlaybackTimeout
	}
	return timeout
}

func coordinateSummary(points []core.Coordinate) string {
	if len(points) == 0 {
		return "[]"
	}
	first := points[0]
	if len(points) == 1 {
		return fmt.Sprintf("[first=%.6f,%.6f]", first.Lat, first.Lon)
	}
	last := points[len(points)-1]
	return fmt.Sprintf("[first=%.6f,%.6f last=%.6f,%.6f]", first.Lat, first.Lon, last.Lat, last.Lon)
}

func (a *Application) finishIPhoneOperation(opID string, started time.Time, name string, err error, confirmed []core.Coordinate) {
	fyne.Do(func() {
		if err == nil {
			a.applyConfirmedIPhonePosition(confirmed)
			a.setRunning(false)
			a.logf("[%s] %s: OK", opID, name)
			a.setStatus(name + " OK")
			a.logOperationDone(opID, started, nil)
			if name == "Set iPhone location" && len(confirmed) == 1 {
				a.startLocationKeepAlive(confirmed[0])
			}
			return
		}
		a.stopPreview()
		a.setRunning(false)
		if errorsIsCanceled(err) {
			a.logf("[%s] %s: stopped: %s", opID, name, err)
			a.setStatus("Stopped")
			return
		}
		if iosbridge.IsLocationSimulationPending(err) {
			a.setStatus("Tunnel ready - location pending")
			a.logf("[%s] %s pending: tunnel discovery succeeded, but DVT LocationSimulation is not implemented yet.", opID, name)
			a.logOperationDone(opID, started, err)
			a.openLogWindow()
			dialog.ShowInformation(
				"Tunnel discovery ready",
				"目前程式會自動管理 iPhone tunnel。請打開 Logs 查看 RSD 位址與 debug log。",
				a.window,
			)
			return
		}
		a.logf("[%s] %s failed: %s", opID, name, err)
		a.setStatus("iPhone error")
		a.logOperationDone(opID, started, err)
		dialog.ShowError(fmt.Errorf("%s failed: %s\n\nFull log: %s", name, compactError(err), a.logFilePath), a.window)
	})
}

func (a *Application) applyConfirmedIPhonePosition(points []core.Coordinate) {
	if len(points) == 0 {
		return
	}
	last := points[len(points)-1]
	a.stateMu.Lock()
	a.joystickPosition = &core.Coordinate{Lat: last.Lat, Lon: last.Lon}
	a.stateMu.Unlock()
	a.currentLabel.SetText(fmt.Sprintf("Current: %.6f, %.6f", last.Lat, last.Lon))
	status := "Confirmed"
	if len(points) > 1 {
		status = "Arrived"
	}
	a.mapView.SetCurrentPosition(last, status)
}

func (a *Application) startLocationKeepAlive(point core.Coordinate) {
	ctx, cancel := context.WithCancel(context.Background())
	a.stateMu.Lock()
	previous := a.keepAliveCancel
	a.keepAliveCancel = cancel
	a.keepAlivePoint = &core.Coordinate{Lat: point.Lat, Lon: point.Lon}
	a.stateMu.Unlock()
	if previous != nil {
		previous()
	}

	keepID := a.nextOperationID("keep")
	a.stateMu.Lock()
	a.keepAliveID = keepID
	a.stateMu.Unlock()
	a.logf("[%s] location keepalive started interval=%s timeout=%s lat=%.6f lon=%.6f", keepID, locationKeepAliveEvery, locationKeepAliveSend, point.Lat, point.Lon)
	go func() {
		ticker := time.NewTicker(locationKeepAliveEvery)
		defer ticker.Stop()
		defer a.clearLocationKeepAlive(keepID)
		for {
			select {
			case <-ctx.Done():
				a.logf("[%s] location keepalive stopped", keepID)
				return
			case <-ticker.C:
			}

			a.stateMu.Lock()
			current := a.keepAlivePoint
			if current == nil {
				current = &point
			}
			point = core.Coordinate{Lat: current.Lat, Lon: current.Lon}
			a.stateMu.Unlock()

			sendCtx, sendCancel := context.WithTimeout(ctx, locationKeepAliveSend)
			started := time.Now()
			err := a.bridge.SetLocation(sendCtx, point)
			sendCancel()
			if ctx.Err() != nil {
				a.logf("[%s] location keepalive stopped", keepID)
				return
			}
			if err != nil {
				a.logf("[%s] location keepalive failed after %s: %s", keepID, time.Since(started).Round(time.Millisecond), err)
				fyne.Do(func() {
					a.setStatus("Location: keepalive error")
				})
				continue
			}
			a.logf("[%s] location keepalive OK in %s", keepID, time.Since(started).Round(time.Millisecond))
		}
	}()
}

func (a *Application) clearLocationKeepAlive(keepID string) {
	a.stateMu.Lock()
	if a.keepAliveID == keepID {
		a.keepAliveID = ""
		a.keepAliveCancel = nil
		a.keepAlivePoint = nil
	}
	a.stateMu.Unlock()
}

func (a *Application) stopLocationKeepAlive() {
	a.stateMu.Lock()
	cancel := a.keepAliveCancel
	a.keepAliveID = ""
	a.keepAliveCancel = nil
	a.keepAlivePoint = nil
	a.stateMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *Application) updateLocationKeepAlive(point core.Coordinate) {
	a.stateMu.Lock()
	if a.keepAliveCancel != nil {
		a.keepAlivePoint = &core.Coordinate{Lat: point.Lat, Lon: point.Lon}
	}
	a.stateMu.Unlock()
}

func (a *Application) stop() {
	a.stopLocationKeepAlive()
	a.cancelCurrentOperation()
	a.stopPreview()
	a.mapView.SetFollowMode(false)
	a.setRunning(false)
	a.currentLabel.SetText("Current: stopped")
	a.stateMu.Lock()
	current := a.joystickPosition
	a.stateMu.Unlock()
	if current != nil {
		a.mapView.SetCurrentPosition(*current, "Stopped")
	}

	go func() {
		err := a.bridge.Stop()
		fyne.Do(func() {
			if err != nil {
				a.logf("Stopping iPhone simulation failed: %s", err)
				a.setStatus("Stop failed")
			} else {
				a.logf("Stopping iPhone simulation: playback and keepalive stopped; location was not reset")
				a.refreshTunnelStatus()
				a.setStatus("Stopped")
			}
		})
	}()
}

func (a *Application) resetLocation() {
	opID := a.nextOperationID("reset")
	started := time.Now()
	a.logf("[%s] reset requested target=%s", opID, bridgeIPhone)
	if a.resetInFlight.Swap(true) {
		a.logf("[%s] reset ignored: already in progress", opID)
		return
	}
	a.stopLocationKeepAlive()
	a.cancelCurrentOperation()
	a.stopPreview()
	a.stopJoystick()
	a.mapView.SetFollowMode(false)
	a.setRunning(false)
	a.startButton.Disable()
	a.resetButton.Disable()

	if a.bridge.UDID() == "" {
		a.resetInFlight.Store(false)
		a.startButton.Enable()
		a.resetButton.Enable()
		a.logf("[%s] reset rejected: no selected iPhone", opID)
		dialog.ShowInformation("Missing iPhone", "Select an iPhone before resetting location.", a.window)
		a.setStatus("No iPhone detected")
		return
	}

	a.setStatus("Resetting location...")
	a.logf("[%s] resetting iPhone location: clearing simulated location", opID)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	a.setOperationCancel(opID, cancel)
	go func() {
		defer cancel()
		defer a.clearOperationCancel(opID)
		err := a.bridge.ClearLocation(ctx)
		fyne.Do(func() {
			a.resetInFlight.Store(false)
			a.startButton.Enable()
			a.resetButton.Enable()
			if err != nil {
				a.logf("[%s] reset iPhone location failed: %s", opID, err)
				a.setStatus("Reset failed")
				a.logOperationDone(opID, started, err)
				dialog.ShowError(err, a.window)
				return
			}
			a.clearCurrentLocationView()
			a.refreshTunnelStatus()
			a.logf("[%s] reset iPhone location: location cleared; tunnel kept running", opID)
			a.setStatus("Location reset")
			a.logOperationDone(opID, started, nil)
		})
	}()
}

func (a *Application) clearCurrentLocationView() {
	a.stateMu.Lock()
	a.joystickPosition = nil
	a.stateMu.Unlock()
	a.currentLabel.SetText("Current: reset")
	a.mapView.ClearCurrentPosition()
}

func (a *Application) stopPreview() {
	a.stateMu.Lock()
	cancel := a.previewCancel
	a.previewID = ""
	a.previewCancel = nil
	a.stateMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *Application) startRouteTracker(parent context.Context, opID string, points []core.Coordinate, tick time.Duration) {
	if len(points) == 0 {
		return
	}
	if tick <= 0 {
		tick = core.DefaultRouteTick
	}
	a.stopPreview()
	ctx, cancel := context.WithCancel(parent)
	a.stateMu.Lock()
	a.previewID = opID
	a.previewCancel = cancel
	a.stateMu.Unlock()
	a.mapView.SetFollowMode(true)
	a.logf("[%s] route UI tracker started points=%d tick=%s", opID, len(points), tick)

	go func() {
		started := time.Now()
		lastIndex := -1
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
		defer a.clearRouteTracker(opID)

		for {
			elapsed := time.Since(started)
			index := int(elapsed / tick)
			if index >= len(points) {
				index = len(points) - 1
			}
			if index != lastIndex {
				point := points[index]
				a.stateMu.Lock()
				a.joystickPosition = &point
				a.stateMu.Unlock()
				status := fmt.Sprintf("Route %d/%d", index+1, len(points))
				if index == len(points)-1 {
					status = "Arrived"
				}
				fyne.Do(func() {
					a.currentLabel.SetText(fmt.Sprintf("Current: %.6f, %.6f", point.Lat, point.Lon))
					a.mapView.SetCurrentPosition(point, status)
				})
				lastIndex = index
			}
			if index == len(points)-1 {
				a.logf("[%s] route UI tracker reached final point", opID)
				return
			}

			select {
			case <-ctx.Done():
				a.logf("[%s] route UI tracker stopped", opID)
				return
			case <-ticker.C:
			}
		}
	}()
}

func (a *Application) clearRouteTracker(opID string) {
	a.stateMu.Lock()
	if a.previewID == opID {
		a.previewID = ""
		a.previewCancel = nil
	}
	a.stateMu.Unlock()
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
	if a.bridge.UDID() == "" {
		a.setStatus("No iPhone detected")
		a.logf("Joystick movement rejected: no selected iPhone")
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

	a.queueJoystickLocation(next)
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
	if a.bridge.UDID() == "" {
		a.stateMu.Unlock()
		a.setStatus("No iPhone detected")
		a.logf("Keyboard movement rejected: no selected iPhone")
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

	a.queueJoystickLocation(next)
}

func (a *Application) queueJoystickLocation(point core.Coordinate) {
	if a.bridge.UDID() == "" {
		a.stopJoystick()
		a.setStatus("No iPhone detected")
		a.logf("Joystick location update rejected: no selected iPhone")
		return
	}
	a.updateLocationKeepAlive(point)
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
