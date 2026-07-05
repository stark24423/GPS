package ui

import "testing"

func TestParseOSRMRoute(t *testing.T) {
	route, err := parseOSRMRoute([]byte(`{
		"code": "Ok",
		"routes": [{
			"distance": 1234.5,
			"duration": 321.0,
			"geometry": {
				"coordinates": [[121.5001, 25.0001], [121.5002, 25.0002]]
			}
		}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(route.Points) != 2 {
		t.Fatalf("points = %d, want 2", len(route.Points))
	}
	if route.Points[0].Lat != 25.0001 || route.Points[0].Lon != 121.5001 {
		t.Fatalf("unexpected first point: %+v", route.Points[0])
	}
	if route.DistanceMeters != 1234.5 {
		t.Fatalf("distance = %f", route.DistanceMeters)
	}
	if route.Duration.Seconds() != 321 {
		t.Fatalf("duration = %s", route.Duration)
	}
}

func TestParseOSRMRouteRejectsNoRoute(t *testing.T) {
	if _, err := parseOSRMRoute([]byte(`{"code":"NoRoute","message":"No route found"}`)); err == nil {
		t.Fatal("expected error")
	}
}
