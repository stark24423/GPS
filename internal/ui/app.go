package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
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

	startButton       *widget.Button
	stopButton        *widget.Button
	clearButton       *widget.Button
	undoButton        *widget.Button
	outputButton      *widget.Button
	logButton         *widget.Button
	refreshButton     *widget.Button
	tunnelStartButton *widget.Button
	tunnelStopButton  *widget.Button
	resolveButton     *widget.Button
	setSingleButton   *widget.Button
	addRouteButton    *widget.Button
	applyNowButton    *widget.Button
	saveRouteButton   *widget.Button
	loadRouteButton   *widget.Button
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
	a.undoButton = widget.NewButtonWithIcon("Undo", theme.NavigateBackIcon(), a.removeLastPoint)
	a.outputButton = widget.NewButtonWithIcon("Output", theme.FolderOpenIcon(), a.openOutputFolder)
	a.logButton = widget.NewButtonWithIcon("Logs", theme.InfoIcon(), a.openLogWindow)
	a.refreshButton = widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), a.refreshDevices)
	a.tunnelStartButton = widget.NewButtonWithIcon("Start Tunnel", theme.MediaPlayIcon(), a.startTunnel)
	a.tunnelStopButton = widget.NewButtonWithIcon("Stop Tunnel", theme.MediaStopIcon(), a.stopTunnel)
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

func (a *Application) buildLayout() fyne.CanvasObject {
	mapToolbar := widget.NewToolbar(
		widget.NewToolbarAction(theme.ZoomInIcon(), a.mapView.ZoomIn),
		widget.NewToolbarAction(theme.ZoomOutIcon(), a.mapView.ZoomOut),
		widget.NewToolbarSeparator(),
		widget.NewToolbarAction(theme.ZoomFitIcon(), func() {
			a.stateMu.Lock()
			current := a.joystickPosition
			a.stateMu.Unlock()
			if current != nil {
				a.mapView.CenterOn(*current)
			}
		}),
	)
	mapHeader := container.NewBorder(nil, nil,
		widget.NewLabelWithStyle("地圖", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		mapToolbar,
		widget.NewLabel("點擊加入定位點，拖曳平移，滾輪縮放"),
	)
	mapArea := container.NewBorder(mapHeader, nil, nil, nil, a.mapView)

	joystickSpeedSlider := widget.NewSlider(0.1, 500)
	joystickSpeedSlider.Step = 0.5
	joystickSpeedSlider.Value = a.joystickSpeed
	joystickSpeedSlider.OnChanged = func(value float64) {
		a.stateMu.Lock()
		a.joystickSpeed = value
		a.stateMu.Unlock()
		a.joystickSpeedLabel.SetText(fmt.Sprintf("%.1f km/h", value))
	}
	joystick := NewJoystick(a.onJoystickDirection)

	headerDevice := container.NewHBox(
		container.NewGridWrap(fyne.NewSize(260, a.deviceSelect.MinSize().Height), a.deviceSelect),
		a.refreshButton,
	)
	headerRoute := container.NewHBox(a.saveRouteButton, a.loadRouteButton, a.clearButton)
	appHeader := container.NewVBox(
		container.NewPadded(container.NewBorder(nil, nil,
			headerDevice,
			headerRoute,
			nil,
		)),
		widget.NewSeparator(),
	)

	locationSection := compactSection("定位", container.NewVBox(
		widget.NewForm(
			widget.NewFormItem("模式", a.modeSelect),
			widget.NewFormItem("輸出", a.bridgeSelect),
		),
		a.locationEntry,
		buttonGrid(3, a.resolveButton, a.setSingleButton, a.addRouteButton),
		container.NewPadded(a.applyNowButton),
	))

	moveSection := compactSection("移動", container.NewVBox(
		labeledSlider("路線速度", a.speedSlider, a.speedLabel),
		labeledSlider("飄移", a.jitterSlider, a.jitterLabel),
		buttonGrid(2, a.startButton, a.stopButton),
	))

	joystickHint := widget.NewLabel("方向鍵 / WASD")
	joystickHint.Alignment = fyne.TextAlignCenter
	joystickHint.TextStyle = fyne.TextStyle{Italic: true}
	joystickStopButton := widget.NewButtonWithIcon("停止", theme.MediaStopIcon(), func() {
		joystick.Reset()
		a.stopJoystick()
		a.mapView.SetFollowMode(false)
		a.setStatus("Joystick paused")
	})
	joystickSection := compactSection("搖桿", container.NewBorder(nil, nil,
		container.NewCenter(container.NewGridWrap(fyne.NewSize(112, 112), joystick)),
		nil,
		container.NewVBox(
			labeledSlider("速度", joystickSpeedSlider, a.joystickSpeedLabel),
			joystickHint,
			container.NewPadded(joystickStopButton),
		),
	))

	side := container.NewPadded(container.NewVBox(
		locationSection,
		moveSection,
		joystickSection,
	))

	split := container.NewHSplit(mapArea, side)
	split.Offset = 0.72

	statusRow := container.NewHBox(statusDotCell(a.statusDot, a.statusLabel), a.statusLabel)
	footer := container.NewVBox(
		widget.NewSeparator(),
		container.NewPadded(container.NewGridWithColumns(4,
			infoPill("狀態", statusRow),
			infoValue(a.pointsLabel),
			infoValue(a.currentLabel),
			infoValue(a.tunnelLabel),
		)),
	)

	return container.NewBorder(appHeader, footer, nil, nil, split)
}

func valueSlider(slider *widget.Slider, valueLabel *widget.Label) fyne.CanvasObject {
	valueLabel.Alignment = fyne.TextAlignTrailing
	valueLabel.TextStyle = fyne.TextStyle{Monospace: true}
	return container.NewBorder(nil, nil, nil, valueLabel, slider)
}

func labeledSlider(label string, slider *widget.Slider, valueLabel *widget.Label) fyne.CanvasObject {
	labelWidget := widget.NewLabel(label)
	labelWidget.Truncation = fyne.TextTruncateEllipsis
	valueLabel.Alignment = fyne.TextAlignTrailing
	valueLabel.TextStyle = fyne.TextStyle{Monospace: true}

	return container.NewBorder(nil, nil,
		container.NewGridWrap(fyne.NewSize(76, slider.MinSize().Height), labelWidget),
		container.NewGridWrap(fyne.NewSize(82, slider.MinSize().Height), valueLabel),
		slider,
	)
}

func buttonGrid(columns int, objects ...fyne.CanvasObject) fyne.CanvasObject {
	padded := make([]fyne.CanvasObject, 0, len(objects))
	for _, object := range objects {
		padded = append(padded, container.NewPadded(object))
	}
	return container.NewGridWithColumns(columns, padded...)
}

func compactSection(title string, content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(
		widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		content,
	)
}

func infoPill(title string, content fyne.CanvasObject) fyne.CanvasObject {
	titleLabel := widget.NewLabel(title + ":")
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}
	return container.NewHBox(titleLabel, content)
}

func infoValue(content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewHBox(content)
}

func statusDotCell(dot *canvas.Circle, label *widget.Label) fyne.CanvasObject {
	height := label.MinSize().Height
	if height < 18 {
		height = 18
	}
	return container.NewGridWrap(
		fyne.NewSize(12, height),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(12, 12), dot)),
	)
}

func (a *Application) resolveLocationFromInput() {
	a.applyInputLocation(inputActionResolve)
}

func (a *Application) applyInputLocation(action string) {
	query := strings.TrimSpace(a.locationEntry.Text)
	if query == "" {
		dialog.ShowInformation("缺少定位資料", "請輸入地址或 GPS 座標。", a.window)
		return
	}

	a.setLocationButtonsEnabled(false)
	a.setStatus("Resolving location...")
	go func() {
		point, label, err := resolveLocation(query)
		fyne.Do(func() {
			a.setLocationButtonsEnabled(true)
			if err != nil {
				a.setStatus("Location lookup failed")
				dialog.ShowError(err, a.window)
				return
			}
			a.applyResolvedLocation(action, point, label)
		})
	}()
}

func (a *Application) applyResolvedLocation(action string, point core.Coordinate, label string) {
	switch action {
	case inputActionRoute:
		a.modeSelect.SetSelected(modeRoute)
		a.addPoint(point)
		a.setStatus("Route point added")
	case inputActionSetNow:
		a.modeSelect.SetSelected(modeSingle)
		a.addPoint(point)
		a.start()
	case inputActionSingle, inputActionResolve:
		a.modeSelect.SetSelected(modeSingle)
		a.addPoint(point)
		a.setStatus("Location ready")
	}
	a.mapView.CenterOn(point)
	a.locationEntry.SetText(label)
	a.logf("Resolved location: %s -> %.6f, %.6f", label, point.Lat, point.Lon)
}

func (a *Application) setLocationButtonsEnabled(enabled bool) {
	buttons := []*widget.Button{a.resolveButton, a.setSingleButton, a.addRouteButton, a.applyNowButton}
	for _, button := range buttons {
		if enabled {
			button.Enable()
		} else {
			button.Disable()
		}
	}
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

func (a *Application) saveRouteTXT() {
	points := a.selectedPoints()
	if len(points) == 0 {
		dialog.ShowInformation("沒有路線", "請先建立至少一個定位點。", a.window)
		return
	}

	rendered, err := core.RenderRouteTXT(points)
	if err != nil {
		dialog.ShowError(err, a.window)
		return
	}

	saveDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}
		if writer == nil {
			return
		}
		defer writer.Close()
		if _, err := io.WriteString(writer, rendered); err != nil {
			dialog.ShowError(err, a.window)
			return
		}
		a.logf("Route TXT saved: %s", writer.URI().String())
		a.setStatus("Route saved")
	}, a.window)
	saveDialog.SetFileName(fmt.Sprintf("route_%s.txt", time.Now().Format("20060102_150405")))
	saveDialog.SetFilter(storage.NewExtensionFileFilter([]string{".txt"}))
	saveDialog.Show()
}

func (a *Application) loadRouteTXT() {
	openDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}
		if reader == nil {
			return
		}
		defer reader.Close()

		data, err := io.ReadAll(reader)
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}
		points, err := core.ParseRouteTXT(string(data))
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}
		a.applyLoadedRoute(points)
		a.logf("Route TXT loaded: %s", reader.URI().String())
		a.setStatus("Route loaded")
	}, a.window)
	openDialog.SetFilter(storage.NewExtensionFileFilter([]string{".txt"}))
	openDialog.Show()
}

func (a *Application) applyLoadedRoute(points []core.Coordinate) {
	a.stopPreview()
	a.stopJoystick()
	a.stateMu.Lock()
	a.points = append([]core.Coordinate(nil), points...)
	last := points[len(points)-1]
	a.joystickPosition = &core.Coordinate{Lat: last.Lat, Lon: last.Lon}
	a.stateMu.Unlock()

	if len(points) > 1 {
		a.modeSelect.SetSelected(modeRoute)
	} else {
		a.modeSelect.SetSelected(modeSingle)
	}
	a.pointsLabel.SetText(fmt.Sprintf("Points: %d", len(points)))
	a.currentLabel.SetText(fmt.Sprintf("Current: %.6f, %.6f", last.Lat, last.Lon))
	a.mapView.SetFollowMode(false)
	a.mapView.SetPoints(points)
	a.mapView.SetCurrentPosition(last, "Loaded")
	a.mapView.CenterOn(last)
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

	if a.bridgeSelect.Selected == bridgeIPhone && a.joystickSendInFlight.CompareAndSwap(false, true) {
		go func(point core.Coordinate) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			err := a.bridge.SetLocation(ctx, point)
			a.joystickSendInFlight.Store(false)
			if err != nil {
				fyne.Do(func() {
					a.logf("Keyboard iPhone error: %s", err)
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

func resolveLocation(query string) (core.Coordinate, string, error) {
	if point, ok := parseCoordinate(query); ok {
		label := fmt.Sprintf("%.6f, %.6f", point.Lat, point.Lon)
		return point, label, nil
	}
	return geocodeAddress(query)
}

func parseCoordinate(text string) (core.Coordinate, bool) {
	normalized := strings.NewReplacer("，", ",", " ", ",", "\t", ",", "\n", ",").Replace(text)
	parts := strings.Split(normalized, ",")
	values := make([]float64, 0, 2)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		value, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return core.Coordinate{}, false
		}
		values = append(values, value)
	}
	if len(values) != 2 {
		return core.Coordinate{}, false
	}
	lat, lon := values[0], values[1]
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return core.Coordinate{}, false
	}
	return core.Coordinate{Lat: lat, Lon: lon}, true
}

func geocodeAddress(query string) (core.Coordinate, string, error) {
	endpoint := "https://nominatim.openstreetmap.org/search?format=json&limit=1&q=" + url.QueryEscape(query)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return core.Coordinate{}, "", err
	}
	req.Header.Set("User-Agent", "gpssim-go-fyne/1.0")

	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return core.Coordinate{}, "", fmt.Errorf("地址搜尋失敗：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return core.Coordinate{}, "", fmt.Errorf("地址搜尋失敗：HTTP %d", resp.StatusCode)
	}

	var results []struct {
		Lat         string `json:"lat"`
		Lon         string `json:"lon"`
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return core.Coordinate{}, "", fmt.Errorf("地址搜尋回應無法解析：%w", err)
	}
	if len(results) == 0 {
		return core.Coordinate{}, "", fmt.Errorf("找不到地址：%s", query)
	}

	lat, err := strconv.ParseFloat(results[0].Lat, 64)
	if err != nil {
		return core.Coordinate{}, "", fmt.Errorf("地址緯度無法解析：%w", err)
	}
	lon, err := strconv.ParseFloat(results[0].Lon, 64)
	if err != nil {
		return core.Coordinate{}, "", fmt.Errorf("地址經度無法解析：%w", err)
	}
	return core.Coordinate{Lat: lat, Lon: lon}, results[0].DisplayName, nil
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
