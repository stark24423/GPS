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
	appVersion = "v0.1.8"

	modeSingle = "單點定位"
	modeRoute  = "路線模擬"

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
	deviceSelect  *widget.Select
	locationEntry *widget.Entry

	speedSlider        *widget.Slider
	speedLabel         *widget.Label
	routeSpeedSummary  *widget.Label
	jitterSlider       *widget.Slider
	jitterLabel        *widget.Label
	joystickSpeed      float64
	joystickSpeedLabel *widget.Label
	routeProgress      *widget.ProgressBar
	routeSpeedKmh      float64

	statusLabel  *widget.Label
	pointsLabel  *widget.Label
	currentLabel *widget.Label
	deviceLabel  *widget.Label
	tunnelLabel  *widget.Label
	logLabel     *widget.Label

	startButton     *widget.Button
	stopButton      *widget.Button
	resetButton     *widget.Button
	clearButton     *widget.Button
	undoButton      *widget.Button
	refreshButton   *widget.Button
	resolveButton   *widget.Button
	setSingleButton *widget.Button
	addRouteButton  *widget.Button
	planRouteButton *widget.Button
	applyNowButton  *widget.Button
	saveRouteButton *widget.Button
	loadRouteButton *widget.Button
	statusDot       *canvas.Circle

	stateMu              sync.Mutex
	points               []core.Coordinate
	running              bool
	operationID          string
	operationCancel      context.CancelFunc
	keepAliveID          string
	keepAliveCancel      context.CancelFunc
	keepAliveDone        chan struct{}
	keepAlivePoint       *core.Coordinate
	previewID            string
	previewCancel        context.CancelFunc
	joystickCancel       context.CancelFunc
	joystickStreamCancel context.CancelFunc
	joystickStreamDone   chan struct{}
	joystickUpdates      chan core.Coordinate
	joystickDX           float64
	joystickDY           float64
	joystickPosition     *core.Coordinate
	deviceChoices        map[string]string
	logMu                sync.Mutex
	logLines             []string

	tunnelStartInFlight atomic.Bool
	resetInFlight       atomic.Bool
	operationSeq        atomic.Uint64
	locationGeneration  atomic.Uint64
	outputDir           string
	logFilePath         string
}

func Run() {
	application := NewApplication()
	application.window.ShowAndRun()
}

func NewApplication() *Application {
	fyneApp := app.NewWithID("gpssim.go")
	window := fyneApp.NewWindow("GPS Simulator " + appVersion)
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
	a.undoButton.Disable()
	a.refreshDevices()
	a.logRequirements()
	a.logf("Application started in iPhone mode.")

	return a
}

func (a *Application) buildControls() {
	a.statusLabel = widget.NewLabel("待命")
	a.statusLabel.TextStyle = fyne.TextStyle{Bold: true}
	a.statusDot = canvas.NewCircle(statusIdleColor())
	a.pointsLabel = widget.NewLabel("定位點：0")
	a.currentLabel = widget.NewLabel("目前位置：—")
	a.deviceLabel = widget.NewLabel("尚未偵測到 iPhone")
	a.deviceLabel.Truncation = fyne.TextTruncateEllipsis
	a.tunnelLabel = widget.NewLabel("連線：尚未建立")
	a.tunnelLabel.Truncation = fyne.TextTruncateEllipsis

	a.modeSelect = widget.NewSelect([]string{modeSingle, modeRoute}, func(value string) {
		a.updateRouteControls()
	})
	a.modeSelect.Selected = modeSingle

	a.deviceSelect = widget.NewSelect(nil, func(label string) {
		if udid, ok := a.deviceChoices[label]; ok {
			a.stopLocationKeepAlive()
			a.stopPreview()
			a.stopJoystick()
			a.bridge.SetUDID(udid)
			a.logf("Selected device: %s", label)
			a.startTunnelForSelected(false)
		}
	})
	a.deviceSelect.PlaceHolder = "選擇 iPhone"

	a.locationEntry = widget.NewEntry()
	a.locationEntry.SetPlaceHolder("地址或 GPS 座標，例如 24.7808548, 121.0252718")
	a.locationEntry.OnSubmitted = func(string) { a.resolveLocationFromInput() }

	a.speedSlider = widget.NewSlider(0.1, 300)
	a.speedSlider.Step = 0.1
	a.speedSlider.Value = 19
	a.routeSpeedKmh = a.speedSlider.Value
	a.speedLabel = widget.NewLabel("19.0 km/h")
	a.speedSlider.OnChanged = func(value float64) {
		a.stateMu.Lock()
		a.routeSpeedKmh = value
		running := a.running
		a.stateMu.Unlock()
		a.speedLabel.SetText(fmt.Sprintf("%.1f km/h", value))
		a.refreshRouteSpeedSummary()
		if running {
			a.setStatus(fmt.Sprintf("路線播放中｜速度 %.1f km/h", value))
		}
	}
	a.routeSpeedSummary = widget.NewLabel("")
	a.routeSpeedSummary.Wrapping = fyne.TextWrapWord
	a.routeProgress = widget.NewProgressBar()

	a.jitterSlider = widget.NewSlider(0, 50)
	a.jitterSlider.Step = 0.5
	a.jitterSlider.Value = 5
	a.jitterLabel = widget.NewLabel("5.0 m")
	a.jitterSlider.OnChanged = func(value float64) {
		a.jitterLabel.SetText(fmt.Sprintf("%.1f m", value))
	}
	a.updateRouteControls()

	a.joystickSpeedLabel = widget.NewLabel("19.0 km/h")

	a.startButton = widget.NewButtonWithIcon("套用定位", theme.MediaPlayIcon(), a.start)
	a.startButton.Importance = widget.HighImportance
	a.stopButton = widget.NewButtonWithIcon("停止播放", theme.MediaStopIcon(), a.stop)
	a.stopButton.Importance = widget.DangerImportance
	a.resetButton = widget.NewButtonWithIcon("還原真實定位", theme.ContentClearIcon(), a.resetLocation)
	a.clearButton = widget.NewButtonWithIcon("清除定位點", theme.DeleteIcon(), a.clearPoints)
	a.undoButton = widget.NewButtonWithIcon("復原上一點", theme.ContentUndoIcon(), a.removeLastPoint)
	a.refreshButton = widget.NewButtonWithIcon("重新掃描", theme.ViewRefreshIcon(), a.refreshDevices)
	a.resolveButton = widget.NewButtonWithIcon("搜尋", theme.SearchIcon(), a.resolveLocationFromInput)
	a.setSingleButton = widget.NewButtonWithIcon("設為單點", theme.RadioButtonIcon(), func() {
		a.applyInputLocation(inputActionSingle)
	})
	a.addRouteButton = widget.NewButtonWithIcon("加入路線", theme.ContentAddIcon(), func() {
		a.applyInputLocation(inputActionRoute)
	})
	a.planRouteButton = widget.NewButtonWithIcon("規劃 A–B 路線", theme.NavigateNextIcon(), a.planABRoute)
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
	a.planRouteButton.SetText("規劃 A–B 路線")
	a.applyNowButton.SetText("立即修改定位")

	a.logLabel = widget.NewLabel("")
	a.logLabel.Wrapping = fyne.TextWrapWord

	a.window.SetMaster()
}
