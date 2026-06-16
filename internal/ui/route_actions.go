package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
	"gpssim/internal/core"
)

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
