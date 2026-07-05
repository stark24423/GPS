package ui

import (
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

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
		),
		a.locationEntry,
		buttonGrid(3, a.resolveButton, a.setSingleButton, a.addRouteButton),
		buttonGrid(2, a.planRouteButton, a.applyNowButton),
	))

	moveSection := compactSection("移動", container.NewVBox(
		labeledSlider("路線速度", a.speedSlider, a.speedLabel),
		labeledSlider("飄移", a.jitterSlider, a.jitterLabel),
		a.routeSpeedSummary,
		buttonGrid(3, a.startButton, a.stopButton, a.resetButton),
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
		container.NewPadded(container.NewGridWithColumns(5,
			infoPill("狀態", statusRow),
			infoValue(a.pointsLabel),
			infoValue(a.currentLabel),
			infoValue(a.tunnelLabel),
			infoPill("版本", widget.NewLabel(appVersion)),
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
