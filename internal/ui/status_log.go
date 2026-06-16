package ui

import (
	"context"
	"errors"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

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
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
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
