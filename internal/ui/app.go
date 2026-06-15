package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"gpssim/internal/core"
	"gpssim/internal/iosbridge"
)

const (
	modeSingle = "Single point"
	modeRoute  = "Route"

	bridgeDryRun = "Dry-run"
	bridgeIPhone = "iPhone"
)

type Application struct {
	app    fyne.App
	window fyne.Window

	mapView *MapWidget
	bridge  *iosbridge.Bridge

	modeSelect   *widget.Select
	bridgeSelect *widget.Select
	deviceSelect *widget.Select

	speedSlider        *widget.Slider
	speedLabel         *widget.Label
	jitterSlider       *widget.Slider
	jitterLabel        *widget.Label
	joystickSpeed      float64
	joystickSpeedLabel *widget.Label

	statusLabel  *widget.Label
	pointsLabel  *widget.Label
	currentLabel *widget.Label
	deviceLabel  *widget.Label
	tunnelLabel  *widget.Label
	logEntry     *widget.Entry

	startButton       *widget.Button
	stopButton        *widget.Button
	clearButton       *widget.Button
	undoButton        *widget.Button
	outputButton      *widget.Button
	refreshButton     *widget.Button
	tunnelStartButton *widget.Button
	tunnelStopButton  *widget.Button

	stateMu          sync.Mutex
	points           []core.Coordinate
	running          bool
	previewCancel    context.CancelFunc
	joystickCancel   context.CancelFunc
	joystickDX       float64
	joystickDY       float64
	joystickPosition *core.Coordinate
	deviceChoices    map[string]string
	logLines         []string

	joystickSendInFlight atomic.Bool
	outputDir            string
}

func Run() {
	application := NewApplication()
	application.window.ShowAndRun()
}

func NewApplication() *Application {
	fyneApp := app.NewWithID("gpssim.go")
	window := fyneApp.NewWindow("GPS Simulator")
	window.Resize(fyne.NewSize(1120, 720))

	root := resolveProjectRoot()
	a := &Application{
		app:           fyneApp,
		window:        window,
		bridge:        iosbridge.New(),
		outputDir:     filepath.Join(root, "output"),
		deviceChoices: make(map[string]string),
		joystickSpeed: 20,
	}

	a.mapView = NewMapWidget(a.addPoint)
	a.buildControls()
	a.window.SetContent(a.buildLayout())
	a.stopButton.Disable()
	a.refreshDevices()
	a.logRequirements()
	a.logf("Application started in dry-run mode.")

	return a
}

func (a *Application) buildControls() {
	a.statusLabel = widget.NewLabel("Idle")
	a.pointsLabel = widget.NewLabel("Points: 0")
	a.currentLabel = widget.NewLabel("Current: -")
	a.deviceLabel = widget.NewLabel("Device: no iPhone detected")
	a.deviceLabel.Wrapping = fyne.TextWrapWord
	a.tunnelLabel = widget.NewLabel("Tunnel: built-in Go tunnel not started")
	a.tunnelLabel.Wrapping = fyne.TextWrapWord

	a.modeSelect = widget.NewSelect([]string{modeSingle, modeRoute}, func(value string) {
		if value == modeSingle {
			a.keepOnlyLastPoint()
		}
	})
	a.modeSelect.Selected = modeSingle

	a.bridgeSelect = widget.NewSelect([]string{bridgeDryRun, bridgeIPhone}, func(value string) {
		a.logf("Bridge mode changed to %s.", value)
		a.refreshDevices()
		a.logRequirements()
	})
	a.bridgeSelect.Selected = bridgeDryRun

	a.deviceSelect = widget.NewSelect(nil, func(label string) {
		if udid, ok := a.deviceChoices[label]; ok {
			a.bridge.SetUDID(udid)
			a.logf("Selected device: %s", label)
		}
	})
	a.deviceSelect.PlaceHolder = "Default device"

	a.speedSlider = widget.NewSlider(0.1, 300)
	a.speedSlider.Step = 0.1
	a.speedSlider.Value = 5
	a.speedLabel = widget.NewLabel("5.0 km/h")
	a.speedSlider.OnChanged = func(value float64) {
		a.speedLabel.SetText(fmt.Sprintf("%.1f km/h", value))
	}

	a.jitterSlider = widget.NewSlider(0, 50)
	a.jitterSlider.Step = 0.5
	a.jitterSlider.Value = 5
	a.jitterLabel = widget.NewLabel("5.0 m")
	a.jitterSlider.OnChanged = func(value float64) {
		a.jitterLabel.SetText(fmt.Sprintf("%.1f m", value))
	}

	a.joystickSpeedLabel = widget.NewLabel("20.0 km/h")

	a.startButton = widget.NewButtonWithIcon("Start", theme.MediaPlayIcon(), a.start)
	a.startButton.Importance = widget.HighImportance
	a.stopButton = widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), a.stop)
	a.stopButton.Importance = widget.DangerImportance
	a.clearButton = widget.NewButtonWithIcon("Clear", theme.DeleteIcon(), a.clearPoints)
	a.undoButton = widget.NewButtonWithIcon("Undo", theme.NavigateBackIcon(), a.removeLastPoint)
	a.outputButton = widget.NewButtonWithIcon("Output", theme.FolderOpenIcon(), a.openOutputFolder)
	a.refreshButton = widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), a.refreshDevices)
	a.tunnelStartButton = widget.NewButtonWithIcon("Start Tunnel", theme.MediaPlayIcon(), a.startTunnel)
	a.tunnelStopButton = widget.NewButtonWithIcon("Stop Tunnel", theme.MediaStopIcon(), a.stopTunnel)

	a.logEntry = widget.NewMultiLineEntry()
	a.logEntry.Disable()
	a.logEntry.Wrapping = fyne.TextWrapWord

	a.window.SetMaster()
}

func (a *Application) buildLayout() fyne.CanvasObject {
	zoomIn := widget.NewButtonWithIcon("", theme.ZoomInIcon(), a.mapView.ZoomIn)
	zoomOut := widget.NewButtonWithIcon("", theme.ZoomOutIcon(), a.mapView.ZoomOut)
	mapTools := container.NewHBox(zoomIn, zoomOut)
	mapArea := container.NewBorder(mapTools, nil, nil, nil, a.mapView)

	joystickSpeedSlider := widget.NewSlider(0.1, 500)
	joystickSpeedSlider.Step = 0.5
	joystickSpeedSlider.Value = 20
	joystickSpeedSlider.OnChanged = func(value float64) {
		a.stateMu.Lock()
		a.joystickSpeed = value
		a.stateMu.Unlock()
		a.joystickSpeedLabel.SetText(fmt.Sprintf("%.1f km/h", value))
	}
	joystick := NewJoystick(a.onJoystickDirection)

	statusCard := widget.NewCard("Status", "", container.NewVBox(
		a.statusLabel,
		a.pointsLabel,
		a.currentLabel,
	))
	settingsCard := widget.NewCard("Settings", "", container.NewVBox(
		widget.NewForm(
			widget.NewFormItem("Mode", a.modeSelect),
			widget.NewFormItem("Bridge", a.bridgeSelect),
			widget.NewFormItem("Speed", container.NewBorder(nil, nil, nil, a.speedLabel, a.speedSlider)),
			widget.NewFormItem("Jitter", container.NewBorder(nil, nil, nil, a.jitterLabel, a.jitterSlider)),
		),
	))
	joystickCard := widget.NewCard("Joystick", "", container.NewVBox(
		container.NewCenter(joystick),
		widget.NewForm(widget.NewFormItem("Speed", container.NewBorder(nil, nil, nil, a.joystickSpeedLabel, joystickSpeedSlider))),
	))
	deviceCard := widget.NewCard("iPhone", "", container.NewVBox(
		a.deviceLabel,
		a.deviceSelect,
		a.tunnelLabel,
		container.NewGridWithColumns(2, a.tunnelStartButton, a.tunnelStopButton),
		a.refreshButton,
	))

	actions := container.NewGridWithColumns(2, a.startButton, a.stopButton)
	tools := container.NewGridWithColumns(3, a.undoButton, a.clearButton, a.outputButton)
	logBox := container.NewVScroll(a.logEntry)
	logBox.SetMinSize(fyne.NewSize(280, 140))

	side := container.NewBorder(nil, nil, nil, nil, container.NewVScroll(container.NewVBox(
		widget.NewLabelWithStyle("GPS Simulator", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		statusCard,
		settingsCard,
		joystickCard,
		deviceCard,
		actions,
		tools,
		widget.NewLabel("Log"),
		logBox,
	)))

	split := container.NewHSplit(mapArea, side)
	split.Offset = 0.74
	return split
}

func (a *Application) addPoint(point core.Coordinate) {
	a.stateMu.Lock()
	if a.modeSelect.Selected == modeSingle {
		a.points = []core.Coordinate{point}
	} else {
		a.points = append(a.points, point)
	}
	a.joystickPosition = &core.Coordinate{Lat: point.Lat, Lon: point.Lon}
	points := append([]core.Coordinate(nil), a.points...)
	a.stateMu.Unlock()

	a.pointsLabel.SetText(fmt.Sprintf("Points: %d", len(points)))
	a.currentLabel.SetText(fmt.Sprintf("Current: %.6f, %.6f", point.Lat, point.Lon))
	a.mapView.SetPoints(points)
	a.mapView.SetCurrentPosition(point, "Selected")
	a.logf("Added point: %.6f, %.6f", point.Lat, point.Lon)
}

func (a *Application) clearPoints() {
	a.stopPreview()
	a.stopJoystick()
	a.stateMu.Lock()
	a.points = nil
	a.joystickPosition = nil
	a.stateMu.Unlock()

	a.pointsLabel.SetText("Points: 0")
	a.currentLabel.SetText("Current: -")
	a.mapView.SetFollowMode(false)
	a.mapView.ClearPoints()
	a.mapView.ClearCurrentPosition()
	a.setStatus("Idle")
	a.logf("Cleared points.")
}

func (a *Application) removeLastPoint() {
	a.stateMu.Lock()
	if len(a.points) == 0 {
		a.stateMu.Unlock()
		return
	}
	removed := a.points[len(a.points)-1]
	a.points = a.points[:len(a.points)-1]
	points := append([]core.Coordinate(nil), a.points...)
	a.stateMu.Unlock()

	a.pointsLabel.SetText(fmt.Sprintf("Points: %d", len(points)))
	a.mapView.SetPoints(points)
	if len(points) > 0 {
		point := points[len(points)-1]
		a.currentLabel.SetText(fmt.Sprintf("Current: %.6f, %.6f", point.Lat, point.Lon))
		a.mapView.SetCurrentPosition(point, "Selected")
	} else {
		a.currentLabel.SetText("Current: -")
		a.mapView.ClearCurrentPosition()
	}
	a.logf("Removed point: %.6f, %.6f", removed.Lat, removed.Lon)
}

func (a *Application) keepOnlyLastPoint() {
	a.stateMu.Lock()
	if len(a.points) <= 1 {
		a.stateMu.Unlock()
		return
	}
	last := a.points[len(a.points)-1]
	a.points = []core.Coordinate{last}
	a.stateMu.Unlock()
	a.pointsLabel.SetText("Points: 1")
	a.currentLabel.SetText(fmt.Sprintf("Current: %.6f, %.6f", last.Lat, last.Lon))
	a.mapView.SetPoints([]core.Coordinate{last})
	a.mapView.SetCurrentPosition(last, "Selected")
	a.logf("Single point mode keeps only the latest point.")
}

func (a *Application) selectedPoints() []core.Coordinate {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.modeSelect.Selected == modeSingle {
		if len(a.points) == 0 {
			return nil
		}
		return []core.Coordinate{a.points[len(a.points)-1]}
	}
	return append([]core.Coordinate(nil), a.points...)
}

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
}

func (a *Application) startIPhoneOperation(points, simulationPoints []core.Coordinate) {
	if a.modeSelect.Selected == modeSingle {
		point := points[0]
		a.logf("Sending location %.6f, %.6f to iPhone.", point.Lat, point.Lon)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
			return
		}
		if errorsIsCanceled(err) {
			a.logf("%s: stopped", name)
			return
		}
		a.logf("%s failed: %s", name, err)
		a.setStatus("iPhone error")
		dialog.ShowError(fmt.Errorf("%s failed: %w", name, err), a.window)
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
				} else {
					a.logf("Stopping iPhone simulation: OK")
				}
			})
		}()
	} else {
		a.logf("Dry-run stop complete. No iPhone location was changed.")
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
	a.stateMu.Unlock()
	if cancel != nil {
		cancel()
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

	if a.bridgeSelect.Selected == bridgeIPhone && a.joystickSendInFlight.CompareAndSwap(false, true) {
		go func(point core.Coordinate) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := a.bridge.SetLocation(ctx, point)
			a.joystickSendInFlight.Store(false)
			if err != nil {
				fyne.Do(func() {
					a.stopJoystick()
					a.logf("Joystick iPhone error: %s", err)
					a.setStatus("iPhone error")
				})
			}
		}(next)
	}
}

func (a *Application) refreshDevices() {
	a.refreshButton.Disable()
	a.deviceLabel.SetText("Device: scanning...")
	go func() {
		devices, err := a.bridge.ListDevices()
		fyne.Do(func() {
			a.refreshButton.Enable()
			if err != nil {
				a.deviceLabel.SetText("Device: scan failed")
				a.logf("iPhone scan failed: %s", err)
				return
			}
			if len(devices) == 0 {
				a.deviceLabel.SetText("Device: no iPhone detected")
				a.tunnelLabel.SetText("Tunnel: no selected iPhone")
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
			}
		})
	}()
}

func (a *Application) startTunnel() {
	if a.bridge.UDID() == "" {
		dialog.ShowInformation("Missing iPhone", "Select an iPhone before starting the tunnel.", a.window)
		return
	}
	a.tunnelStartButton.Disable()
	a.tunnelLabel.SetText("Tunnel: starting...")
	a.logf("Starting built-in Go tunnel.")

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		info, err := a.bridge.StartTunnel(ctx)
		fyne.Do(func() {
			a.tunnelStartButton.Enable()
			if err != nil {
				a.tunnelLabel.SetText("Tunnel: " + err.Error())
				a.logf("Start tunnel failed: %s", err)
				dialog.ShowError(err, a.window)
				return
			}
			a.tunnelLabel.SetText(fmt.Sprintf("Tunnel: %s %s:%d", info.State, info.RSDAddress, info.RSDPort))
			a.logf("Tunnel started: %+v", info)
		})
	}()
}

func (a *Application) stopTunnel() {
	a.tunnelStopButton.Disable()
	a.logf("Stopping built-in Go tunnel.")
	go func() {
		err := a.bridge.StopTunnel()
		fyne.Do(func() {
			a.tunnelStopButton.Enable()
			if err != nil {
				a.logf("Stop tunnel failed: %s", err)
				dialog.ShowError(err, a.window)
				return
			}
			a.tunnelLabel.SetText("Tunnel: stopped")
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

func (a *Application) openOutputFolder() {
	if err := os.MkdirAll(a.outputDir, 0755); err != nil {
		dialog.ShowError(err, a.window)
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", a.outputDir)
	case "darwin":
		cmd = exec.Command("open", a.outputDir)
	default:
		cmd = exec.Command("xdg-open", a.outputDir)
	}
	if err := cmd.Start(); err != nil {
		dialog.ShowError(err, a.window)
	}
}

func (a *Application) setRunning(running bool) {
	a.stateMu.Lock()
	a.running = running
	a.stateMu.Unlock()

	if running {
		a.startButton.Disable()
		a.stopButton.Enable()
		a.clearButton.Disable()
		a.undoButton.Disable()
		a.modeSelect.Disable()
		a.bridgeSelect.Disable()
		a.speedSlider.Disable()
		a.jitterSlider.Disable()
		a.mapView.SetEditingLocked(true)
		a.setStatus("Running")
		return
	}

	a.startButton.Enable()
	a.stopButton.Disable()
	a.clearButton.Enable()
	a.undoButton.Enable()
	a.modeSelect.Enable()
	a.bridgeSelect.Enable()
	a.speedSlider.Enable()
	a.jitterSlider.Enable()
	a.mapView.SetEditingLocked(false)
	a.setStatus("Idle")
}

func (a *Application) setStatus(status string) {
	a.statusLabel.SetText(status)
}

func (a *Application) logf(format string, args ...interface{}) {
	line := fmt.Sprintf("%s  %s", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
	fyne.Do(func() {
		a.logLines = append(a.logLines, line)
		if len(a.logLines) > 500 {
			a.logLines = a.logLines[len(a.logLines)-500:]
		}
		a.logEntry.SetText(strings.Join(a.logLines, "\n"))
	})
}

func resolveProjectRoot() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

func errorsIsCanceled(err error) bool {
	return err == context.Canceled || err == context.DeadlineExceeded
}
