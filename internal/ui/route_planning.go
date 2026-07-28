package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"

	"gpssim/internal/core"
)

type plannedRoute struct {
	Points         []core.Coordinate
	DistanceMeters float64
	Duration       time.Duration
}

type osrmRouteResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Routes  []struct {
		Distance float64 `json:"distance"`
		Duration float64 `json:"duration"`
		Geometry struct {
			Coordinates [][]float64 `json:"coordinates"`
		} `json:"geometry"`
	} `json:"routes"`
}

func (a *Application) planABRoute() {
	opID := a.nextOperationID("route")
	started := time.Now()
	points := a.selectedPoints()
	if len(points) < 2 {
		a.logf("[%s] route planning rejected: points=%d", opID, len(points))
		dialog.ShowInformation("缺少起點與終點", "請先加入至少兩個定位點再規劃路線。", a.window)
		return
	}
	start := points[0]
	end := points[len(points)-1]

	a.setLocationButtonsEnabled(false)
	a.setStatus("正在規劃 A–B 路線…")
	a.logf("[%s] planning A-B route: %.6f, %.6f -> %.6f, %.6f", opID, start.Lat, start.Lon, end.Lat, end.Lon)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		route, err := fetchOSRMRoute(ctx, start, end)
		fyne.Do(func() {
			a.setLocationButtonsEnabled(true)
			if err != nil {
				a.setStatus("路線規劃失敗")
				a.logf("[%s] route planning failed after %s: %s", opID, time.Since(started).Round(time.Millisecond), err)
				dialog.ShowError(err, a.window)
				return
			}
			a.applyLoadedRoute(route.Points)
			a.setStatus("A–B 路線已完成")
			a.logf(
				"[%s] A-B route planned after %s: %d points, %.1f km, %.0f min",
				opID,
				time.Since(started).Round(time.Millisecond),
				len(route.Points),
				route.DistanceMeters/1000,
				route.Duration.Minutes(),
			)
		})
	}()
}

func fetchOSRMRoute(ctx context.Context, start, end core.Coordinate) (plannedRoute, error) {
	endpoint := fmt.Sprintf(
		"https://router.project-osrm.org/route/v1/driving/%.8f,%.8f;%.8f,%.8f?overview=full&geometries=geojson&steps=false",
		start.Lon,
		start.Lat,
		end.Lon,
		end.Lat,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return plannedRoute{}, err
	}
	req.Header.Set("User-Agent", "gpssim-go-fyne/1.0")

	client := http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return plannedRoute{}, fmt.Errorf("route planning request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return plannedRoute{}, fmt.Errorf("route planning failed: HTTP %d %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return plannedRoute{}, err
	}
	return parseOSRMRoute(data)
}

func parseOSRMRoute(data []byte) (plannedRoute, error) {
	var response osrmRouteResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return plannedRoute{}, err
	}
	if response.Code != "Ok" {
		if response.Message != "" {
			return plannedRoute{}, fmt.Errorf("route planning failed: %s", response.Message)
		}
		return plannedRoute{}, fmt.Errorf("route planning failed: %s", response.Code)
	}
	if len(response.Routes) == 0 {
		return plannedRoute{}, fmt.Errorf("route planning returned no routes")
	}

	route := response.Routes[0]
	points := make([]core.Coordinate, 0, len(route.Geometry.Coordinates))
	for index, coordinate := range route.Geometry.Coordinates {
		if len(coordinate) < 2 {
			return plannedRoute{}, fmt.Errorf("route coordinate %d is invalid", index+1)
		}
		lon := coordinate[0]
		lat := coordinate[1]
		if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
			return plannedRoute{}, fmt.Errorf("route coordinate %d is out of range", index+1)
		}
		points = append(points, core.Coordinate{Lat: lat, Lon: lon})
	}
	if len(points) < 2 {
		return plannedRoute{}, fmt.Errorf("route planning returned too few points")
	}
	return plannedRoute{
		Points:         points,
		DistanceMeters: route.Distance,
		Duration:       time.Duration(route.Duration * float64(time.Second)),
	}, nil
}
