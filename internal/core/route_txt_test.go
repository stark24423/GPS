package core

import (
	"strings"
	"testing"
)

func TestRouteTXTRoundTrip(t *testing.T) {
	points := []Coordinate{
		{Lat: 24.7808548, Lon: 121.0252718, Elevation: 8.25},
		{Lat: 25.033, Lon: 121.5654, Elevation: 12.5},
	}

	rendered, err := RenderRouteTXT(points)
	if err != nil {
		t.Fatalf("RenderRouteTXT error: %v", err)
	}
	if !strings.Contains(rendered, "25.03300000,121.56540000,12.50") {
		t.Fatalf("route text missing coordinate:\n%s", rendered)
	}
	parsed, err := ParseRouteTXT(rendered)
	if err != nil {
		t.Fatalf("ParseRouteTXT error: %v", err)
	}
	if len(parsed) != len(points) {
		t.Fatalf("parsed points = %d, want %d", len(parsed), len(points))
	}
	for i := range points {
		if parsed[i].Lat != points[i].Lat || parsed[i].Lon != points[i].Lon || parsed[i].Elevation != points[i].Elevation {
			t.Fatalf("point %d = %+v, want %+v", i, parsed[i], points[i])
		}
	}
}

func TestParseRouteTXTHeaderAndWhitespace(t *testing.T) {
	points, err := ParseRouteTXT(`
# lat,lon,elevation
lat,lon,elevation
25.033,121.5654,12.5
25.034 121.5664
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 {
		t.Fatalf("expected 2 points, got %d", len(points))
	}
	if points[0].Lat != 25.033 || points[0].Lon != 121.5654 || points[0].Elevation != 12.5 {
		t.Fatalf("unexpected first point: %+v", points[0])
	}
	if points[1].Lat != 25.034 || points[1].Lon != 121.5664 {
		t.Fatalf("unexpected second point: %+v", points[1])
	}
}

func TestParseRouteTXTRejectsInvalidLine(t *testing.T) {
	if _, err := ParseRouteTXT("24.1\n"); err == nil {
		t.Fatal("expected invalid line error")
	}
}

func TestParseRouteTXTRejectsEmptyRoute(t *testing.T) {
	if _, err := ParseRouteTXT("# empty\n"); err == nil {
		t.Fatal("expected empty route error")
	}
}
