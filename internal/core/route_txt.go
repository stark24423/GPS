package core

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

func RenderRouteTXT(points []Coordinate) (string, error) {
	if len(points) == 0 {
		return "", fmt.Errorf("at least one coordinate is required")
	}

	var builder strings.Builder
	builder.WriteString("# GPS route TXT\n")
	builder.WriteString("# lat,lon\n")
	for _, point := range points {
		builder.WriteString(fmt.Sprintf("%.8f,%.8f\n", point.Lat, point.Lon))
	}
	return builder.String(), nil
}

func ParseRouteTXT(text string) ([]Coordinate, error) {
	scanner := bufio.NewScanner(strings.NewReader(text))
	points := make([]Coordinate, 0)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		line = strings.NewReplacer("，", ",", "\t", ",", " ", ",").Replace(line)
		parts := strings.Split(line, ",")
		values := make([]string, 0, 2)
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				values = append(values, part)
			}
		}
		if len(values) != 2 {
			return nil, fmt.Errorf("line %d: expected lat,lon", lineNumber)
		}

		lat, err := strconv.ParseFloat(values[0], 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid latitude", lineNumber)
		}
		lon, err := strconv.ParseFloat(values[1], 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid longitude", lineNumber)
		}
		if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
			return nil, fmt.Errorf("line %d: coordinate out of range", lineNumber)
		}
		points = append(points, Coordinate{Lat: lat, Lon: lon})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(points) == 0 {
		return nil, fmt.Errorf("route TXT contains no coordinates")
	}
	return points, nil
}
