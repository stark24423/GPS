package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
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
	if a.modeSelect.Selected == modeRoute {
		a.applyInputLocation(inputActionRoute)
		return
	}
	a.applyInputLocation(inputActionSingle)
}

func (a *Application) applyInputLocation(action string) {
	opID := a.nextOperationID("loc")
	started := time.Now()
	query := strings.TrimSpace(a.locationEntry.Text)
	a.logf("[%s] location input action=%s query_len=%d", opID, action, len(query))
	if query == "" {
		a.logf("[%s] rejected: empty location input", opID)
		dialog.ShowInformation("缺少定位資料", "請輸入地址或 GPS 座標。", a.window)
		return
	}

	a.setLocationButtonsEnabled(false)
	a.setStatus("正在搜尋位置…")
	go func() {
		point, label, err := resolveLocation(query)
		fyne.Do(func() {
			a.setLocationButtonsEnabled(true)
			if err != nil {
				a.setStatus("位置搜尋失敗")
				a.logf("[%s] location lookup failed after %s: %s", opID, time.Since(started).Round(time.Millisecond), err)
				dialog.ShowError(err, a.window)
				return
			}
			a.logf("[%s] location lookup OK after %s label=%q lat=%.6f lon=%.6f", opID, time.Since(started).Round(time.Millisecond), compactLogText(label, 120), point.Lat, point.Lon)
			a.applyResolvedLocation(action, point, label)
		})
	}()
}

func (a *Application) applyResolvedLocation(action string, point core.Coordinate, label string) {
	switch action {
	case inputActionRoute:
		a.modeSelect.SetSelected(modeRoute)
		a.addPoint(point)
		a.setStatus("已加入路線定位點")
	case inputActionSetNow:
		a.modeSelect.SetSelected(modeSingle)
		a.addPoint(point)
		a.start()
	case inputActionSingle, inputActionResolve:
		a.modeSelect.SetSelected(modeSingle)
		a.addPoint(point)
		a.setStatus("位置已就緒")
	}
	a.mapView.CenterOn(point)
	a.locationEntry.SetText(label)
	a.logf("Resolved location: %s -> %.6f, %.6f", label, point.Lat, point.Lon)
}

func (a *Application) setLocationButtonsEnabled(enabled bool) {
	buttons := []*widget.Button{a.resolveButton, a.setSingleButton, a.addRouteButton, a.planRouteButton, a.applyNowButton}
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

	a.pointsLabel.SetText(fmt.Sprintf("定位點：%d", len(points)))
	a.currentLabel.SetText(fmt.Sprintf("目前位置：%.6f, %.6f", point.Lat, point.Lon))
	a.mapView.SetPoints(points)
	a.mapView.SetCurrentPosition(point, "Selected")
	a.undoButton.Enable()
	a.refreshRouteSpeedSummary()
	a.logf("Added point: %.6f, %.6f", point.Lat, point.Lon)
}

func (a *Application) clearPoints() {
	a.stopPreview()
	a.stopJoystick()
	a.stateMu.Lock()
	a.points = nil
	a.joystickPosition = nil
	a.stateMu.Unlock()

	a.pointsLabel.SetText("定位點：0")
	a.currentLabel.SetText("目前位置：—")
	a.routeProgress.SetValue(0)
	a.mapView.SetFollowMode(false)
	a.mapView.ClearPoints()
	a.mapView.ClearCurrentPosition()
	a.undoButton.Disable()
	a.refreshRouteSpeedSummary()
	a.setStatus("待命")
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
	if len(points) > 0 {
		last := points[len(points)-1]
		a.joystickPosition = &core.Coordinate{Lat: last.Lat, Lon: last.Lon}
	} else {
		a.joystickPosition = nil
	}
	a.stateMu.Unlock()

	a.pointsLabel.SetText(fmt.Sprintf("定位點：%d", len(points)))
	a.mapView.SetPoints(points)
	if len(points) > 0 {
		point := points[len(points)-1]
		a.currentLabel.SetText(fmt.Sprintf("目前位置：%.6f, %.6f", point.Lat, point.Lon))
		a.mapView.SetCurrentPosition(point, "Selected")
	} else {
		a.currentLabel.SetText("目前位置：—")
		a.mapView.ClearCurrentPosition()
		a.undoButton.Disable()
	}
	a.refreshRouteSpeedSummary()
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
		a.setStatus("路線已載入")
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
	a.pointsLabel.SetText(fmt.Sprintf("定位點：%d", len(points)))
	a.currentLabel.SetText(fmt.Sprintf("目前位置：%.6f, %.6f", last.Lat, last.Lon))
	a.mapView.SetFollowMode(false)
	a.mapView.SetPoints(points)
	a.mapView.SetCurrentPosition(last, "Loaded")
	a.mapView.CenterOn(last)
	a.undoButton.Enable()
	a.refreshRouteSpeedSummary()
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
	a.pointsLabel.SetText("定位點：1")
	a.currentLabel.SetText(fmt.Sprintf("目前位置：%.6f, %.6f", last.Lat, last.Lon))
	a.mapView.SetPoints([]core.Coordinate{last})
	a.mapView.SetCurrentPosition(last, "Selected")
	a.refreshRouteSpeedSummary()
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

func (a *Application) updateRouteControls() {
	a.stateMu.Lock()
	running := a.running
	a.stateMu.Unlock()
	if running {
		a.speedSlider.Disable()
		a.jitterSlider.Disable()
		return
	}
	if a.modeSelect.Selected == modeRoute {
		if a.startButton != nil {
			a.startButton.SetText("播放路線")
		}
		a.speedSlider.Enable()
		a.jitterSlider.Enable()
	} else {
		if a.startButton != nil {
			a.startButton.SetText("套用定位")
		}
		a.speedSlider.Disable()
		a.jitterSlider.Disable()
	}
	a.refreshRouteSpeedSummary()
}

func (a *Application) refreshRouteSpeedSummary() {
	if a.routeSpeedSummary == nil {
		return
	}
	points := a.selectedPoints()
	if a.modeSelect == nil || a.modeSelect.Selected != modeRoute {
		a.routeSpeedSummary.SetText("切換到路線模擬後可設定速度與 GPS 漂移。")
		return
	}
	if len(points) < 2 {
		a.routeSpeedSummary.SetText("請加入至少兩個定位點以估算播放時間。")
		return
	}
	distanceMeters := routeDistanceMeters(points)
	speedKmh := a.speedSlider.Value
	duration := estimatedRouteDuration(distanceMeters, speedKmh)
	a.routeSpeedSummary.SetText(fmt.Sprintf("路線 %.2f 公里｜%.1f km/h｜預估 %s", distanceMeters/1000, speedKmh, compactDuration(duration)))
}

func routeDistanceMeters(points []core.Coordinate) float64 {
	var total float64
	for i := 1; i < len(points); i++ {
		total += core.HaversineDistanceMeters(points[i-1], points[i])
	}
	return total
}

func estimatedRouteDuration(distanceMeters, speedKmh float64) time.Duration {
	if distanceMeters <= 0 || speedKmh <= 0 {
		return 0
	}
	return time.Duration(distanceMeters/(speedKmh*1000/3600)) * time.Second
}

func compactDuration(duration time.Duration) string {
	duration = duration.Round(time.Second)
	if duration < time.Minute {
		return duration.String()
	}
	hours := int(duration / time.Hour)
	duration -= time.Duration(hours) * time.Hour
	minutes := int(duration / time.Minute)
	duration -= time.Duration(minutes) * time.Minute
	seconds := int(duration / time.Second)
	if hours > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", hours, minutes, seconds)
	}
	return fmt.Sprintf("%dm%02ds", minutes, seconds)
}

func resolveLocation(query string) (core.Coordinate, string, error) {
	if point, ok := parseCoordinate(query); ok {
		label := fmt.Sprintf("%.6f, %.6f", point.Lat, point.Lon)
		return point, label, nil
	}
	return geocodeAddress(query)
}

func parseCoordinate(text string) (core.Coordinate, bool) {
	tokens := strings.FieldsFunc(normalizeCoordinateText(text), func(r rune) bool {
		return r == ',' || r == ';' || r == '\u00b0' || r == '\t' || r == '\n' || r == '\r' || r == ' '
	})
	values := make([]float64, 0, 2)
	hemispheres := make([]bool, 0, 2)
	pendingHemisphere := ""
	for _, token := range tokens {
		token = strings.TrimSpace(strings.Trim(token, "()[]{}"))
		if token == "" {
			continue
		}

		if isHemisphere(token) {
			if len(values) > 0 && !hemispheres[len(hemispheres)-1] {
				values[len(values)-1] = applyHemisphere(values[len(values)-1], token)
				hemispheres[len(hemispheres)-1] = true
				continue
			}
			if pendingHemisphere != "" {
				return core.Coordinate{}, false
			}
			pendingHemisphere = strings.ToUpper(token)
			continue
		}

		match := coordinateTokenPattern.FindStringSubmatch(token)
		if match == nil {
			return core.Coordinate{}, false
		}
		hemisphere := strings.ToUpper(match[1] + match[3])
		if pendingHemisphere != "" {
			if hemisphere != "" {
				return core.Coordinate{}, false
			}
			hemisphere = pendingHemisphere
			pendingHemisphere = ""
		}
		value, err := strconv.ParseFloat(match[2], 64)
		if err != nil {
			return core.Coordinate{}, false
		}
		value = applyHemisphere(value, hemisphere)
		values = append(values, value)
		hemispheres = append(hemispheres, hemisphere != "")
	}
	if len(values) != 2 || pendingHemisphere != "" {
		return core.Coordinate{}, false
	}
	lat, lon := values[0], values[1]
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return core.Coordinate{}, false
	}
	return core.Coordinate{Lat: lat, Lon: lon}, true
}

var coordinateTokenPattern = regexp.MustCompile(`(?i)^([NSEW])?([+-]?\d+(?:\.\d+)?)(?:\x{00b0})?([NSEW])?$`)

func normalizeCoordinateText(text string) string {
	return strings.NewReplacer(
		"\u2212", "-",
		"\uff0d", "-",
		"\ufe63", "-",
		"\u2013", "-",
		"\u2014", "-",
		"\uff0c", ",",
		"\u3001", ",",
		";", ",",
	).Replace(text)
}

func absFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func isHemisphere(token string) bool {
	switch strings.ToUpper(token) {
	case "N", "S", "E", "W":
		return true
	default:
		return false
	}
}

func applyHemisphere(value float64, hemisphere string) float64 {
	switch strings.ToUpper(hemisphere) {
	case "S", "W":
		return -absFloat(value)
	case "N", "E":
		return absFloat(value)
	default:
		return value
	}
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
