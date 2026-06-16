package core

import (
	"fmt"
	"strconv"
	"strings"
)

func RenderRouteTXT(points []Coordinate) (string, error) {
	if len(points) == 0 {
		return "", fmt.Errorf("at least one coordinate is required")
	}

	var builder strings.Builder
	builder.WriteString("# GPS Simulator route\n")
	builder.WriteString("# lat,lon,elevation\n")
	for _, point := range points {
		builder.WriteString(fmt.Sprintf("%.8f,%.8f,%.2f\n", point.Lat, point.Lon, point.Elevation))
	}
	return builder.String(), nil
}

func ParseRouteTXT(text string) ([]Coordinate, error) {
	lines := strings.Split(text, "\n")
	points := make([]Coordinate, 0, len(lines))

	for index, line := range lines {
		lineNumber := index + 1
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := splitRouteTXTLine(line)
		if len(fields) > 0 && strings.EqualFold(fields[0], "lat") {
			continue
		}
		if len(fields) < 2 || len(fields) > 3 {
			return nil, fmt.Errorf("line %d: expected lat,lon or lat,lon,elevation", lineNumber)
		}

		lat, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid latitude %q", lineNumber, fields[0])
		}
		lon, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid longitude %q", lineNumber, fields[1])
		}
		if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
			return nil, fmt.Errorf("line %d: coordinate out of range", lineNumber)
		}

		var elevation float64
		if len(fields) == 3 {
			elevation, err = strconv.ParseFloat(fields[2], 64)
			if err != nil {
				return nil, fmt.Errorf("line %d: invalid elevation %q", lineNumber, fields[2])
			}
		}
		points = append(points, Coordinate{Lat: lat, Lon: lon, Elevation: elevation})
	}
	if len(points) == 0 {
		return nil, fmt.Errorf("TXT route does not contain any coordinates")
	}
	return points, nil
}

func splitRouteTXTLine(line string) []string {
	parts := strings.FieldsFunc(line, func(r rune) bool {
		return r == ',' || r == '\t' || r == ' '
	})
	fields := parts[:0]
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			fields = append(fields, part)
		}
	}
	return fields
}
