package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

type appPalette struct {
	background       color.NRGBA
	chrome           color.NRGBA
	panel            color.NRGBA
	input            color.NRGBA
	border           color.NRGBA
	foreground       color.NRGBA
	mutedForeground  color.NRGBA
	accent           color.NRGBA
	accentForeground color.NRGBA
	hover            color.NRGBA
	focus            color.NRGBA
	selection        color.NRGBA
	pressed          color.NRGBA
	disabled         color.NRGBA
	disabledButton   color.NRGBA
	scrollBar        color.NRGBA
	scrollBarTrack   color.NRGBA
	shadow           color.NRGBA
	statusBar        color.NRGBA
}

var darkPalette = appPalette{
	background:       color.NRGBA{R: 0x1e, G: 0x1e, B: 0x1e, A: 0xff},
	chrome:           color.NRGBA{R: 0x18, G: 0x18, B: 0x18, A: 0xff},
	panel:            color.NRGBA{R: 0x25, G: 0x25, B: 0x26, A: 0xff},
	input:            color.NRGBA{R: 0x2d, G: 0x2d, B: 0x30, A: 0xff},
	border:           color.NRGBA{R: 0x3c, G: 0x3c, B: 0x3c, A: 0xff},
	foreground:       color.NRGBA{R: 0xd4, G: 0xd4, B: 0xd4, A: 0xff},
	mutedForeground:  color.NRGBA{R: 0x9d, G: 0x9d, B: 0x9d, A: 0xff},
	accent:           color.NRGBA{R: 0x00, G: 0x7a, B: 0xcc, A: 0xff},
	accentForeground: color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
	hover:            color.NRGBA{R: 0x2a, G: 0x3a, B: 0x4a, A: 0xff},
	focus:            color.NRGBA{R: 0x37, G: 0x94, B: 0xff, A: 0x66},
	selection:        color.NRGBA{R: 0x09, G: 0x47, B: 0x71, A: 0xff},
	pressed:          color.NRGBA{R: 0x00, G: 0x5a, B: 0x9e, A: 0xff},
	disabled:         color.NRGBA{R: 0x6a, G: 0x6a, B: 0x6a, A: 0xff},
	disabledButton:   color.NRGBA{R: 0x23, G: 0x23, B: 0x23, A: 0xff},
	scrollBar:        color.NRGBA{R: 0x79, G: 0x79, B: 0x79, A: 0x99},
	scrollBarTrack:   color.NRGBA{R: 0x1e, G: 0x1e, B: 0x1e, A: 0x00},
	shadow:           color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x88},
	statusBar:        color.NRGBA{R: 0x00, G: 0x7a, B: 0xcc, A: 0xff},
}

var lightPalette = appPalette{
	background:       color.NRGBA{R: 0xf3, G: 0xf3, B: 0xf3, A: 0xff},
	chrome:           color.NRGBA{R: 0xee, G: 0xee, B: 0xee, A: 0xff},
	panel:            color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
	input:            color.NRGBA{R: 0xf8, G: 0xf8, B: 0xf8, A: 0xff},
	border:           color.NRGBA{R: 0xd0, G: 0xd0, B: 0xd0, A: 0xff},
	foreground:       color.NRGBA{R: 0x2d, G: 0x2d, B: 0x2d, A: 0xff},
	mutedForeground:  color.NRGBA{R: 0x6a, G: 0x6a, B: 0x6a, A: 0xff},
	accent:           color.NRGBA{R: 0x00, G: 0x78, B: 0xd4, A: 0xff},
	accentForeground: color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
	hover:            color.NRGBA{R: 0xe5, G: 0xf1, B: 0xfb, A: 0xff},
	focus:            color.NRGBA{R: 0x00, G: 0x78, B: 0xd4, A: 0x55},
	selection:        color.NRGBA{R: 0xc7, G: 0xe0, B: 0xf4, A: 0xff},
	pressed:          color.NRGBA{R: 0xc7, G: 0xe0, B: 0xf4, A: 0xff},
	disabled:         color.NRGBA{R: 0x9a, G: 0x9a, B: 0x9a, A: 0xff},
	disabledButton:   color.NRGBA{R: 0xe5, G: 0xe5, B: 0xe5, A: 0xff},
	scrollBar:        color.NRGBA{R: 0x8f, G: 0x8f, B: 0x8f, A: 0x88},
	scrollBarTrack:   color.NRGBA{R: 0xf3, G: 0xf3, B: 0xf3, A: 0x00},
	shadow:           color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x22},
	statusBar:        color.NRGBA{R: 0xe8, G: 0xf2, B: 0xff, A: 0xff},
}

type appTheme struct {
	base    fyne.Theme
	palette appPalette
}

func newAppTheme(light bool) fyne.Theme {
	base := theme.DarkTheme()
	palette := darkPalette
	if light {
		base = theme.LightTheme()
		palette = lightPalette
	}
	return &appTheme{base: base, palette: palette}
}

func paletteForMode(light bool) appPalette {
	if light {
		return lightPalette
	}
	return darkPalette
}

func (t *appTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return t.palette.background
	case theme.ColorNameButton:
		return t.palette.input
	case theme.ColorNameDisabled:
		return t.palette.disabled
	case theme.ColorNameDisabledButton:
		return t.palette.disabledButton
	case theme.ColorNameError:
		return color.NRGBA{R: 0xf1, G: 0x4c, B: 0x4c, A: 0xff}
	case theme.ColorNameFocus:
		return t.palette.focus
	case theme.ColorNameForeground:
		return t.palette.foreground
	case theme.ColorNameForegroundOnPrimary:
		return t.palette.accentForeground
	case theme.ColorNameHeaderBackground:
		return t.palette.panel
	case theme.ColorNameHover:
		return t.palette.hover
	case theme.ColorNameInputBackground:
		return t.palette.input
	case theme.ColorNameInputBorder:
		return t.palette.border
	case theme.ColorNameMenuBackground:
		return t.palette.panel
	case theme.ColorNameOverlayBackground:
		return t.palette.panel
	case theme.ColorNamePlaceHolder:
		return t.palette.mutedForeground
	case theme.ColorNamePressed:
		return t.palette.pressed
	case theme.ColorNamePrimary:
		return t.palette.accent
	case theme.ColorNameScrollBar:
		return t.palette.scrollBar
	case theme.ColorNameScrollBarBackground:
		return t.palette.scrollBarTrack
	case theme.ColorNameSelection:
		return t.palette.selection
	case theme.ColorNameSeparator:
		return t.palette.border
	case theme.ColorNameShadow:
		return t.palette.shadow
	case theme.ColorNameSuccess:
		return color.NRGBA{R: 0x89, G: 0xd1, B: 0x85, A: 0xff}
	case theme.ColorNameWarning:
		return color.NRGBA{R: 0xcc, G: 0xa7, B: 0x00, A: 0xff}
	default:
		return t.base.Color(name, variant)
	}
}

func (t *appTheme) Font(style fyne.TextStyle) fyne.Resource {
	return t.base.Font(style)
}

func (t *appTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}

func (t *appTheme) Size(name fyne.ThemeSizeName) float32 {
	return t.base.Size(name)
}
