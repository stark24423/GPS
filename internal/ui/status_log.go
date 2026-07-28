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

const (
	logFileMaxBytes = 5 * 1024 * 1024
	logFileBackups  = 3
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
		// 路線播放器會在每個 tick 讀取最新速度，因此執行中仍需允許調整。
		if a.modeSelect.Selected == modeRoute {
			a.speedSlider.Enable()
		} else {
			a.speedSlider.Disable()
		}
		a.jitterSlider.Disable()
		a.mapView.SetEditingLocked(true)
		a.setStatus("執行中")
		return
	}

	a.startButton.Enable()
	a.stopButton.Disable()
	a.clearButton.Enable()
	a.modeSelect.Enable()
	a.updateRouteControls()
	a.mapView.SetEditingLocked(false)
	a.setStatus("待命")
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
	case strings.Contains(normalized, "error"), strings.Contains(normalized, "failed"), strings.Contains(status, "失敗"), strings.Contains(status, "錯誤"):
		return color.NRGBA{R: 220, G: 38, B: 38, A: 255}
	case strings.Contains(normalized, "starting"), strings.Contains(normalized, "scanning"), strings.Contains(normalized, "running"), strings.Contains(normalized, "active"),
		strings.Contains(normalized, "checking"), strings.Contains(normalized, "finding"), strings.Contains(normalized, "opening"),
		strings.Contains(normalized, "creating"), strings.Contains(normalized, "waiting"), strings.Contains(normalized, "connecting"),
		strings.Contains(normalized, "sending"), strings.Contains(normalized, "discovering"), strings.Contains(normalized, "probing"), strings.Contains(status, "正在"), strings.Contains(status, "執行中"):
		return color.NRGBA{R: 37, G: 99, B: 235, A: 255}
	case strings.Contains(normalized, "warn"), strings.Contains(normalized, "pending"), strings.Contains(normalized, "no iphone"), strings.Contains(normalized, "stopped"),
		strings.Contains(normalized, "retry"), strings.Contains(normalized, "rebuilding"), strings.Contains(normalized, "slow"), strings.Contains(status, "未偵測"), strings.Contains(status, "已停止"):
		return color.NRGBA{R: 217, G: 119, B: 6, A: 255}
	case strings.Contains(normalized, "ready"), strings.Contains(normalized, "ok"), strings.Contains(normalized, "arrived"), strings.Contains(status, "已就緒"), strings.Contains(status, "已完成"), strings.Contains(status, "已還原"):
		return color.NRGBA{R: 22, G: 163, B: 74, A: 255}
	default:
		return statusIdleColor()
	}
}

func statusIdleColor() color.Color {
	return color.NRGBA{R: 100, G: 116, B: 139, A: 255}
}

func (a *Application) logf(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	a.setStatusFromBridgeLog(message)
	line := fmt.Sprintf("%s  %s", time.Now().Format("15:04:05"), message)
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

func (a *Application) nextOperationID(prefix string) string {
	return fmt.Sprintf("%s-%06d", prefix, a.operationSeq.Add(1))
}

func (a *Application) logOperationDone(opID string, started time.Time, err error) {
	if err != nil {
		a.logf("[%s] failed after %s: %s", opID, time.Since(started).Round(time.Millisecond), err)
		return
	}
	a.logf("[%s] completed in %s", opID, time.Since(started).Round(time.Millisecond))
}

func (a *Application) setOperationCancel(opID string, cancel context.CancelFunc) {
	a.stateMu.Lock()
	previous := a.operationCancel
	a.operationID = opID
	a.operationCancel = cancel
	a.stateMu.Unlock()
	if previous != nil {
		previous()
	}
}

func (a *Application) clearOperationCancel(opID string) {
	a.stateMu.Lock()
	if a.operationID == opID {
		a.operationID = ""
		a.operationCancel = nil
	}
	a.stateMu.Unlock()
}

func (a *Application) cancelCurrentOperation() {
	// 即使背景 DVT 呼叫已完成，它的 UI 回呼仍可能尚未執行。
	// 先推進世代，避免舊回呼在停止或還原後重新啟動 keepalive。
	a.locationGeneration.Add(1)
	a.stateMu.Lock()
	cancel := a.operationCancel
	opID := a.operationID
	a.operationID = ""
	a.operationCancel = nil
	a.stateMu.Unlock()
	if cancel != nil {
		a.logf("[%s] cancellation requested", opID)
		cancel()
	}
}

func (a *Application) setStatusFromBridgeLog(message string) {
	status, ok := bridgeLogStatus(message)
	if !ok {
		return
	}
	fyne.Do(func() {
		a.setStatus(status)
	})
}

func bridgeLogStatus(message string) (string, bool) {
	normalized := strings.ToLower(message)
	switch {
	case strings.Contains(normalized, "tunnel start already in progress"):
		return "Tunnel: waiting for current start", true
	case strings.Contains(normalized, "tunnel discovery requested"):
		return "Tunnel: starting", true
	case strings.Contains(normalized, "tunnel already active"):
		return "Tunnel ready", true
	case strings.Contains(normalized, "tunnel preflight"):
		return "Tunnel: checking requirements", true
	case strings.Contains(normalized, "searching usb device"):
		return "Tunnel: finding iPhone", true
	case strings.Contains(normalized, "device found"):
		return "Tunnel: iPhone found", true
	case strings.Contains(normalized, "connecting lockdown"):
		return "Tunnel: connecting lockdown", true
	case strings.Contains(normalized, "starting coredeviceproxy"):
		return "Tunnel: opening CoreDevice", true
	case strings.Contains(normalized, "exchanging coredevice"):
		return "Tunnel: negotiating tunnel", true
	case strings.Contains(normalized, "rsd ready"):
		return "Tunnel: RSD discovered", true
	case strings.Contains(normalized, "creating wintun"):
		return "Tunnel: creating Wintun", true
	case strings.Contains(normalized, "waiting for rsd tcp"):
		return "Tunnel: probing RSD", true
	case strings.Contains(normalized, "rsd tcp reachable"):
		return "Tunnel: RSD reachable", true
	case strings.Contains(normalized, "tunnel discovery complete"):
		return "Tunnel ready", true
	case strings.Contains(normalized, "rebuilding tunnel"):
		return "Tunnel: rebuilding", true
	case strings.Contains(normalized, "location rsd native: discovering"):
		return "Location: discovering DVT", true
	case strings.Contains(normalized, "location dvt native: connecting"):
		return "Location: connecting DVT", true
	case strings.Contains(normalized, "location dvt native: set"):
		return "Location: sending point", true
	case strings.Contains(normalized, "location keepalive started"):
		return "Location: keepalive active", true
	case strings.Contains(normalized, "location keepalive ok"):
		return "Location: keepalive OK", true
	case strings.Contains(normalized, "location keepalive failed"):
		return "Location: keepalive error", true
	case strings.Contains(normalized, "location dvt native: clear"):
		return "Location: clearing", true
	case strings.Contains(normalized, "location dvt native: play route"):
		return "Location: playing route", true
	case strings.Contains(normalized, "route ui tracker started"):
		return "Location: route UI tracking", true
	case strings.Contains(normalized, "location dvt native: route send slow"):
		return "Location: DVT slow", true
	case strings.Contains(normalized, "location dvt native: stream"):
		return "Location: streaming", true
	default:
		return "", false
	}
}

func (a *Application) writeLogLine(line string) {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	if err := os.MkdirAll(a.outputDir, 0755); err != nil {
		return
	}
	_ = rotateLogFile(a.logFilePath, logFileMaxBytes, logFileBackups)
	file, err := os.OpenFile(a.logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.WriteString(line + "\n")
}

func rotateLogFile(path string, maxBytes int64, backups int) error {
	if maxBytes <= 0 || backups <= 0 {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Size() < maxBytes {
		return nil
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", path, backups))
	for i := backups - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", path, i)
		newPath := fmt.Sprintf("%s.%d", path, i+1)
		if _, err := os.Stat(oldPath); err == nil {
			_ = os.Rename(oldPath, newPath)
		}
	}
	return os.Rename(path, fmt.Sprintf("%s.1", path))
}

func (a *Application) openLogWindow() {
	if a.logWin == nil {
		a.logWin = a.app.NewWindow("GPS Simulator 診斷紀錄")
		a.logWin.Resize(fyne.NewSize(760, 460))
		a.logWin.SetCloseIntercept(func() {
			a.logWin.Hide()
		})
		logBox := container.NewVScroll(a.logLabel)
		logBox.SetMinSize(fyne.NewSize(740, 420))
		clearButton := widget.NewButtonWithIcon("清除畫面", theme.DeleteIcon(), func() {
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

func compactLogText(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= limit {
		return text
	}
	if limit < 4 {
		return text[:limit]
	}
	return text[:limit-3] + "..."
}
