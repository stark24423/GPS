package core

import (
	"strings"
	"testing"
	"time"
)

func TestRenderSinglePointGPXContainsWaypoint(t *testing.T) {
	xml, err := RenderGPX([]TimedPoint{{
		Point: Coordinate{Lat: 25.033, Lon: 121.5654},
		Time:  time.Unix(1700000000, 0).Unix(),
	}}, "GPS Simulation")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(xml, `<wpt lat="25.03300000" lon="121.56540000">`) {
		t.Fatalf("waypoint missing from GPX:\n%s", xml)
	}
	if !strings.Contains(xml, "<time>") {
		t.Fatalf("time missing from GPX")
	}
}

func TestRenderRouteGPXContainsTrackPoints(t *testing.T) {
	xml, err := GenerateGPX([]Coordinate{
		{Lat: 25.033, Lon: 121.5654},
		{Lat: 25.034, Lon: 121.5664},
	}, 5, "GPS Simulation", 0)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(xml, "<trk>") || !strings.Contains(xml, `<trkpt lat="25.03300000" lon="121.56540000">`) {
		t.Fatalf("track missing from GPX:\n%s", xml)
	}
}
