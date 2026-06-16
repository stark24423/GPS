package core

import "testing"

func TestRouteTXTRoundTrip(t *testing.T) {
	points := []Coordinate{
		{Lat: 24.7808548, Lon: 121.0252718},
		{Lat: 25.033, Lon: 121.5654},
	}

	rendered, err := RenderRouteTXT(points)
	if err != nil {
		t.Fatalf("RenderRouteTXT error: %v", err)
	}
	parsed, err := ParseRouteTXT(rendered)
	if err != nil {
		t.Fatalf("ParseRouteTXT error: %v", err)
	}
	if len(parsed) != len(points) {
		t.Fatalf("parsed points = %d, want %d", len(parsed), len(points))
	}
	for i := range points {
		if parsed[i].Lat != points[i].Lat || parsed[i].Lon != points[i].Lon {
			t.Fatalf("point %d = %+v, want %+v", i, parsed[i], points[i])
		}
	}
}

func TestParseRouteTXTRejectsInvalidLine(t *testing.T) {
	if _, err := ParseRouteTXT("24.1\n"); err == nil {
		t.Fatal("expected invalid line error")
	}
}
