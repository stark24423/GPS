package ui

import (
	"context"
	"fmt"
	"image/color"
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
	"fyne.io/fyne/v2/canvas"
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
	logWin fyne.Window

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
	logLabel     *widget.Label

	startButton       *widget.Button
	stopButton        *widget.Button
	clearButton       *widget.Button
	undoButton        *widget.Button
	outputButton      *widget.Button
	logButton         *widget.Button
	refreshButton     *widget.Button
	tunnelStartButton *widget.Button
	tunnelStopButton  *widget.Button
	statusDot         *canvas.Circle

	stateMu          sync.Mutex
	points           []core.Coordinate
	running          bool
	previewCancel    context.CancelFunc
	joystickCancel   context.CancelFunc
	joystickDX       float64
	joystickDY       float64
	joystickPosition *core.Coordinate
	deviceChoices    map[string]string
	logMu            sync.Mutex
	logLines         []string

	joystickSendInFlight atomic.Bool
	tunnelStartInFlight  atomic.Bool
	outputDir            string
	logFilePath          string
}

func Run() {
	application := NewApplication()
	application.window.ShowAndRun()
}

func NewApplication() *Application {
	fyneApp := app.NewWithID("gpssim.go")
	window := fyneApp.NewWindow("GPS Simulator")
	window.Resize(fyne.NewSize(1280, 760))

	root := resolveProjectRoot()
	a := &Application{
		app:           fyneApp,
		window:        window,
		bridge:        iosbridge.New(),
		outputDir:     filepath.Join(root, "output"),
		deviceChoices: make(map[string]string),
		joystickSpeed: 20,
	}
	a.logFilePath = filepath.Join(a.outputDir, "gps-simulator.log")

	a.mapView = NewMapWidget(a.addPoint)
	a.buildControls()
	a.window.SetContent(a.buildLayout())
	a.bridge.SetLogger(a.logf)
	a.stopButton.Disable()
	a.refreshDevices()
	a.logRequirements()
	a.logf("Application started in dry-run mode.")

	return a
}

func (a *Application) buildControls() {
	a.statusLabel = widget.NewLabel("Idle")
	a.statusLabel.TextStyle = fyne.TextStyle{Bold: true}
	a.statusDot = canvas.NewCircle(statusIdleColor())
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
			if a.bridgeSelect.Selected != bridgeIPhone {
				a.bridgeSelect.SetSelected(bridgeIPhone)
				return
			}
			a.startTunnelForSelected(false)
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
	a.logButton = widget.NewButtonWithIcon("Logs", theme.InfoIcon(), a.openLogWindow)
	a.refreshButton = widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), a.refreshDevices)
	a.tunnelStartButton = widget.NewButtonWithIcon("Start Tunnel", theme.MediaPlayIcon(), a.startTunnel)
	a.tunnelStopButton = widget.NewButtonWithIcon("Stop Tunnel", theme.MediaStopIcon(), a.stopTunnel)

	a.logLabel = widget.NewLabel("")
	a.logLabel.Wrapping = fyne.TextWrapWord

	a.window.SetMaster()
}

func (a *Application) buildLayout() fyne.CanvasObject {
	mapToolbar := widget.NewToolbar(
		widget.NewToolbarAction(theme.ZoomInIcon(), a.mapView.ZoomIn),
		widget.NewToolbarAction(theme.ZoomOutIcon(), a.mapView.ZoomOut),
	)
	mapHeader := container.NewBorder(nil, nil,
		widget.NewLabelWithStyle("Map", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		mapToolbar,
		widget.NewLabel("Click to add a point. Drag to pan."),
	)
	mapArea := container.NewBorder(mapHeader, nil, nil, nil, a.mapView)

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

	statusRow := container.NewHBox(container.NewGridWrap(fyne.NewSize(12, 12), a.statusDot), a.statusLabel)
	statusCard := widget.NewCard("Run Status", "", container.NewVBox(
		statusRow,
		widget.NewSeparator(),
		container.NewGridWithColumns(1, a.pointsLabel, a.currentLabel),
	))

	simulationForm := widget.NewForm(
		widget.NewFormItem("Mode", a.modeSelect),
		widget.NewFormItem("Bridge", a.bridgeSelect),
		widget.NewFormItem("Route speed", valueSlider(a.speedSlider, a.speedLabel)),
		widget.NewFormItem("Jitter", valueSlider(a.jitterSlider, a.jitterLabel)),
	)
	simulationCard := widget.NewCard("Simulation Setup", "", container.NewVBox(
		simulationForm,
		container.NewGridWithColumns(2, a.startButton, a.stopButton),
	))

	joystickCard := widget.NewCard("Manual Movement", "", container.NewVBox(
		container.NewCenter(joystick),
		widget.NewForm(widget.NewFormItem("Joystick speed", valueSlider(joystickSpeedSlider, a.joystickSpeedLabel))),
	))

	deviceForm := widget.NewForm(
		widget.NewFormItem("Device", a.deviceSelect),
	)
	deviceCard := widget.NewCard("iPhone Connection", "", container.NewVBox(
		deviceForm,
		a.deviceLabel,
		a.tunnelLabel,
		container.NewGridWithColumns(1, a.refreshButton),
	))

	utilityToolbar := widget.NewToolbar(
		widget.NewToolbarAction(theme.FolderOpenIcon(), a.openOutputFolder),
		widget.NewToolbarAction(theme.InfoIcon(), a.openLogWindow),
	)
	utilityCard := widget.NewCard("Utilities", "", container.NewVBox(
		container.NewGridWithColumns(2, a.undoButton, a.clearButton),
		utilityToolbar,
	))

	sideContent := container.NewVBox(
		widget.NewLabelWithStyle("GPS Simulator", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		statusCard,
		simulationCard,
		joystickCard,
		deviceCard,
		utilityCard,
	)
	sideScroll := container.NewVScroll(container.NewPadded(sideContent))
	sideScroll.SetMinSize(fyne.NewSize(360, 620))
	side := container.NewBorder(nil, nil, nil, nil, sideScroll)

	split := container.NewHSplit(mapArea, side)
	split.Offset = 0.70
	return split
}

func valueSlider(slider *widget.Slider, valueLabel *widget.Label) fyne.CanvasObject {
	valueLabel.Alignment = fyne.TextAlignTrailing
	valueLabel.TextStyle = fyne.TextStyle{Monospace: true}
	return container.NewBorder(nil, nil, nil, valueLabel, slider)
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
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	a.tunnelStartButton.Disable()
	a.tunnelLabel.SetText("Tunnel: starting...")
	a.setStatus("Starting tunnel...")
	a.logf("Starting built-in Go tunnel for selected iPhone.")

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		info, err := a.bridge.StartTunnel(ctx)
		fyne.Do(func() {
			a.tunnelStartInFlight.Store(false)
			a.tunnelStartButton.Enable()
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
	if a.statusDot != nil {
		a.statusDot.FillColor = statusColor(status)
		a.statusDot.Refresh()
	}
}

func statusColor(status string) color.Color {
	normalized := strings.ToLower(status)
	switch {
	case strings.Contains(normalized, "error"), strings.Contains(normalized, "failed"):
		return color.NRGBA{R: 220, G: 38, B: 38, A: 255}
	case strings.Contains(normalized, "starting"), strings.Contains(normalized, "scanning"), strings.Contains(normalized, "running"), strings.Contains(normalized, "active"):
		return color.NRGBA{R: 37, G: 99, B: 235, A: 255}
	case strings.Contains(normalized, "warn"), strings.Contains(normalized, "pending"), strings.Contains(normalized, "no iphone"), strings.Contains(normalized, "stopped"):
		return color.NRGBA{R: 217, G: 119, B: 6, A: 255}
	case strings.Contains(normalized, "ready"), strings.Contains(normalized, "ok"), strings.Contains(normalized, "arrived"):
		return color.NRGBA{R: 22, G: 163, B: 74, A: 255}
	default:
		return statusIdleColor()
	}
}

func statusIdleColor() color.Color {
	return color.NRGBA{R: 100, G: 116, B: 139, A: 255}
}

func (a *Application) logf(format string, args ...interface{}) {
	line := fmt.Sprintf("%s  %s", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
	a.writeLogLine(line)
	fyne.Do(func() {
		a.logMu.Lock()
		defer a.logMu.Unlock()
		a.logLines = append(a.logLines, line)
		if len(a.logLines) > 500 {
			a.logLines = a.logLines[len(a.logLines)-500:]
		}
		a.logLabel.SetText(strings.Join(a.logLines, "\n"))
	})
}

func (a *Application) writeLogLine(line string) {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	if err := os.MkdirAll(a.outputDir, 0755); err != nil {
		return
	}
	file, err := os.OpenFile(a.logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.WriteString(line + "\n")
}

func (a *Application) openLogWindow() {
	if a.logWin == nil {
		a.logWin = a.app.NewWindow("GPS Simulator Logs")
		a.logWin.Resize(fyne.NewSize(760, 460))
		a.logWin.SetCloseIntercept(func() {
			a.logWin.Hide()
		})
		logBox := container.NewVScroll(a.logLabel)
		logBox.SetMinSize(fyne.NewSize(740, 420))
		clearButton := widget.NewButtonWithIcon("Clear", theme.DeleteIcon(), func() {
			a.logMu.Lock()
			defer a.logMu.Unlock()
			a.logLines = nil
			a.logLabel.SetText("")
		})
		a.logWin.SetContent(container.NewBorder(nil, container.NewHBox(clearButton), nil, nil, logBox))
	}
	a.logWin.Show()
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

func compactError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.Join(strings.Fields(err.Error()), " ")
	if len(text) <= 320 {
		return text
	}
	return text[:320] + "..."
}
