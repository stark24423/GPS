package core

import (
	"math"
	"testing"
	"time"
)

func TestHaversineDistanceIsPositive(t *testing.T) {
	distance := HaversineDistanceMeters(Coordinate{Lat: 25.033, Lon: 121.5654}, Coordinate{Lat: 25.034, Lon: 121.5664})
	if distance <= 0 {
		t.Fatalf("distance = %f, want positive", distance)
	}
}

func TestSlowerSpeedGeneratesMoreTimedPoints(t *testing.T) {
	points := []Coordinate{{Lat: 25.033, Lon: 121.5654}, {Lat: 25.04, Lon: 121.57}}
	start := time.Unix(1700000000, 0).UTC()

	slow, err := BuildTimedRoute(points, 3, start, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	fast, err := BuildTimedRoute(points, 30, start, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if len(slow) <= len(fast) {
		t.Fatalf("slow route points = %d, fast route points = %d", len(slow), len(fast))
	}
	if slow[0].Point != points[0] || slow[len(slow)-1].Point != points[1] {
		t.Fatalf("route endpoints changed")
	}
}

func TestBuildTimedPointsUsesSubsecondDefaultTick(t *testing.T) {
	points := []Coordinate{{Lat: 25.033, Lon: 121.5654}, {Lat: 25.034, Lon: 121.5664}}

	defaultTick, err := BuildTimedPoints(points, 30, 0)
	if err != nil {
		t.Fatal(err)
	}
	oneSecond, err := BuildTimedRoute(points, 30, time.Unix(1700000000, 0).UTC(), time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if DefaultRouteTick >= time.Second {
		t.Fatalf("DefaultRouteTick = %s, want subsecond", DefaultRouteTick)
	}
	if len(defaultTick) <= len(oneSecond) {
		t.Fatalf("default tick points = %d, one-second points = %d", len(defaultTick), len(oneSecond))
	}
}

func TestRouteJitterKeepsEndpoints(t *testing.T) {
	points := []Coordinate{{Lat: 25.033, Lon: 121.5654}, {Lat: 25.04, Lon: 121.57}}
	route, err := BuildTimedRoute(points, 30, time.Unix(1700000000, 0).UTC(), time.Second)
	if err != nil {
		t.Fatal(err)
	}

	jittered := ApplyRouteJitter(route, 5, 1)
	if jittered[0].Point != route[0].Point || jittered[len(jittered)-1].Point != route[len(route)-1].Point {
		t.Fatalf("route endpoints changed")
	}
}

func TestNormalizeLongitudeWrapsToValidRange(t *testing.T) {
	tests := map[float64]float64{
		257.404162: -102.595838,
		181:        -179,
		-181:       179,
		-540:       -180,
	}
	for input, want := range tests {
		got := NormalizeLongitude(input)
		if math.Abs(got-want) > 0.000001 {
			t.Fatalf("NormalizeLongitude(%f) = %f, want %f", input, got, want)
		}
	}
}

func TestOffsetCoordinateWrapsLongitude(t *testing.T) {
	point := OffsetCoordinate(Coordinate{Lat: 0, Lon: 179.999}, 500, 0)
	if point.Lon < -180 || point.Lon > 180 {
		t.Fatalf("longitude out of range: %+v", point)
	}
	if point.Lon >= 0 {
		t.Fatalf("expected longitude to wrap west of antimeridian, got %+v", point)
	}
}
