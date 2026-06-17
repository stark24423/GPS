package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func (a *Application) buildMainContent() fyne.CanvasObject {
	inspector := container.NewVScroll(container.NewPadded(container.NewVBox(
		a.buildSimulationPanel(),
		verticalGap(),
		a.buildManualMovementPanel(),
	)))
	inspector.SetMinSize(fyne.NewSize(360, 0))

	split := container.NewHSplit(a.buildMapPanel(), inspector)
	split.Offset = 0.72
	return split
}

func (a *Application) buildMapPanel() fyne.CanvasObject {
	mapToolbar := widget.NewToolbar(
		widget.NewToolbarAction(theme.ZoomInIcon(), a.mapView.ZoomIn),
		widget.NewToolbarAction(theme.ZoomOutIcon(), a.mapView.ZoomOut),
	)
	title := widget.NewLabelWithStyle("Map", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	help := widget.NewLabel("Click to add a point. Drag to pan. Scroll to zoom.")
	help.Truncation = fyne.TextTruncateEllipsis

	header := container.NewBorder(nil, nil, title, mapToolbar, help)
	return container.NewPadded(container.NewBorder(header, nil, nil, nil, a.mapView))
}

func (a *Application) buildSimulationPanel() fyne.CanvasObject {
	form := widget.NewForm(
		widget.NewFormItem("Mode", a.modeSelect),
		widget.NewFormItem("Bridge", a.bridgeSelect),
		widget.NewFormItem("Route speed", valueSlider(a.speedSlider, a.speedLabel)),
		widget.NewFormItem("Jitter", valueSlider(a.jitterSlider, a.jitterLabel)),
	)
	return widget.NewCard("Simulation", "Route and playback configuration", form)
}

func (a *Application) buildManualMovementPanel() fyne.CanvasObject {
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
	form := widget.NewForm(widget.NewFormItem("Joystick speed", valueSlider(joystickSpeedSlider, a.joystickSpeedLabel)))
	content := container.NewVBox(
		container.NewCenter(container.NewGridWrap(fyne.NewSize(112, 112), joystick)),
		form,
	)
	return widget.NewCard("Manual Movement", "Direct joystick movement control", content)
}

func verticalGap() fyne.CanvasObject {
	return container.NewGridWrap(fyne.NewSize(1, 8), widget.NewLabel(""))
}
