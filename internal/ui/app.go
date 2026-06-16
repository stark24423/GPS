package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"gpssim/internal/core"
	"gpssim/internal/iosbridge"
)

const (
	appVersion = "v0.1.1"

	modeSingle = "Single point"
	modeRoute  = "Route"

	bridgeDryRun = "Dry-run"
	bridgeIPhone = "iPhone"

	inputActionResolve = "resolve"
	inputActionSingle  = "single"
	inputActionRoute   = "route"
	inputActionSetNow  = "set_now"
)

type Application struct {
	app    fyne.App
	window fyne.Window
	logWin fyne.Window

	mapView *MapWidget
	bridge  *iosbridge.Bridge

	modeSelect    *widget.Select
	bridgeSelect  *widget.Select
	deviceSelect  *widget.Select
	locationEntry *widget.Entry

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

	startButton     *widget.Button
	stopButton      *widget.Button
	clearButton     *widget.Button
	refreshButton   *widget.Button
	resolveButton   *widget.Button
	setSingleButton *widget.Button
	addRouteButton  *widget.Button
	applyNowButton  *widget.Button
	saveRouteButton *widget.Button
	loadRouteButton *widget.Button
	statusDot       *canvas.Circle

	stateMu              sync.Mutex
	points               []core.Coordinate
	running              bool
	previewCancel        context.CancelFunc
	joystickCancel       context.CancelFunc
	joystickStreamCancel context.CancelFunc
	joystickUpdates      chan core.Coordinate
	joystickDX           float64
	joystickDY           float64
	joystickPosition     *core.Coordinate
	deviceChoices        map[string]string
	logMu                sync.Mutex
	logLines             []string

	tunnelStartInFlight atomic.Bool
	outputDir           string
	logFilePath         string
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
		joystickSpeed: 19,
	}
	a.logFilePath = filepath.Join(a.outputDir, "gps-simulator.log")

	a.mapView = NewMapWidget(a.addPoint)
	a.buildControls()
	a.window.SetContent(a.buildLayout())
	a.window.Canvas().SetOnTypedKey(a.onKeyboardMovement)
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
	a.deviceLabel.Truncation = fyne.TextTruncateEllipsis
	a.tunnelLabel = widget.NewLabel("Tunnel: built-in Go tunnel not started")
	a.tunnelLabel.Truncation = fyne.TextTruncateEllipsis

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

	a.locationEntry = widget.NewEntry()
	a.locationEntry.SetPlaceHolder("地址或 GPS 座標，例如 24.7808548, 121.0252718")

	a.speedSlider = widget.NewSlider(0.1, 300)
	a.speedSlider.Step = 0.1
	a.speedSlider.Value = 19
	a.speedLabel = widget.NewLabel("19.0 km/h")
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

	a.joystickSpeedLabel = widget.NewLabel("19.0 km/h")

	a.startButton = widget.NewButtonWithIcon("Start", theme.MediaPlayIcon(), a.start)
	a.startButton.Importance = widget.HighImportance
	a.stopButton = widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), a.stop)
	a.stopButton.Importance = widget.DangerImportance
	a.clearButton = widget.NewButtonWithIcon("Clear", theme.DeleteIcon(), a.clearPoints)
	a.refreshButton = widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), a.refreshDevices)
	a.resolveButton = widget.NewButtonWithIcon("搜尋", theme.SearchIcon(), a.resolveLocationFromInput)
	a.setSingleButton = widget.NewButtonWithIcon("設為單點", theme.RadioButtonIcon(), func() {
		a.applyInputLocation(inputActionSingle)
	})
	a.addRouteButton = widget.NewButtonWithIcon("加入路線", theme.ContentAddIcon(), func() {
		a.applyInputLocation(inputActionRoute)
	})
	a.applyNowButton = widget.NewButtonWithIcon("立即修改定位", theme.ConfirmIcon(), func() {
		a.applyInputLocation(inputActionSetNow)
	})
	a.applyNowButton.Importance = widget.HighImportance
	a.saveRouteButton = widget.NewButtonWithIcon("儲存路線", theme.DocumentSaveIcon(), a.saveRouteTXT)
	a.loadRouteButton = widget.NewButtonWithIcon("載入路線", theme.FolderOpenIcon(), a.loadRouteTXT)
	a.locationEntry.SetPlaceHolder("地址或 GPS 座標，例如 24.7808548, 121.0252718")
	a.resolveButton.SetText("搜尋")
	a.setSingleButton.SetText("設為單點")
	a.addRouteButton.SetText("加入路線")
	a.applyNowButton.SetText("立即修改定位")

	a.logLabel = widget.NewLabel("")
	a.logLabel.Wrapping = fyne.TextWrapWord

	a.window.SetMaster()
}
