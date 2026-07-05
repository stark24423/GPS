package ui

import (
	"testing"
	"time"

	"gpssim/internal/core"
)

func TestEstimatedRouteDuration(t *testing.T) {
	got := estimatedRouteDuration(1000, 36)
	want := 100 * time.Second
	if got != want {
		t.Fatalf("estimatedRouteDuration = %s, want %s", got, want)
	}
}

func TestCompactDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		want     string
	}{
		{duration: 45 * time.Second, want: "45s"},
		{duration: 90 * time.Second, want: "1m30s"},
		{duration: 90*time.Minute + 2*time.Second, want: "1h30m02s"},
	}

	for _, tt := range tests {
		if got := compactDuration(tt.duration); got != tt.want {
			t.Fatalf("compactDuration(%s) = %q, want %q", tt.duration, got, tt.want)
		}
	}
}

func TestRouteDistanceMeters(t *testing.T) {
	points := []core.Coordinate{
		{Lat: 25.0, Lon: 121.0},
		{Lat: 25.0, Lon: 121.001},
		{Lat: 25.001, Lon: 121.001},
	}
	if got := routeDistanceMeters(points); got <= 0 {
		t.Fatalf("routeDistanceMeters = %f, want positive", got)
	}
}
