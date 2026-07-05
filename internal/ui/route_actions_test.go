package ui

import "testing"

func TestParseCoordinateAcceptsWesternLongitude(t *testing.T) {
	point, ok := parseCoordinate("37.7749, -122.4194")
	if !ok {
		t.Fatal("expected coordinate to parse")
	}
	if point.Lat != 37.7749 || point.Lon != -122.4194 {
		t.Fatalf("point = %+v, want lat=37.7749 lon=-122.4194", point)
	}
}

func TestParseCoordinateAcceptsUnicodeMinus(t *testing.T) {
	point, ok := parseCoordinate("37.7749, \u2212122.4194")
	if !ok {
		t.Fatal("expected coordinate to parse")
	}
	if point.Lat != 37.7749 || point.Lon != -122.4194 {
		t.Fatalf("point = %+v, want lat=37.7749 lon=-122.4194", point)
	}
}

func TestParseCoordinateAcceptsHemisphereSuffixes(t *testing.T) {
	point, ok := parseCoordinate("37.7749 N, 122.4194 W")
	if !ok {
		t.Fatal("expected coordinate to parse")
	}
	if point.Lat != 37.7749 || point.Lon != -122.4194 {
		t.Fatalf("point = %+v, want lat=37.7749 lon=-122.4194", point)
	}
}

func TestParseCoordinateRejectsAddressText(t *testing.T) {
	if _, ok := parseCoordinate("1600 Pennsylvania Ave NW"); ok {
		t.Fatal("address text should not parse as a coordinate")
	}
	if _, ok := parseCoordinate("10 Downing Street 20"); ok {
		t.Fatal("address text with two valid-range numbers should not parse as a coordinate")
	}
}
