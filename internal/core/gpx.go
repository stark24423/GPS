package core

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type gpxDocument struct {
	XMLName  xml.Name     `xml:"gpx"`
	Version  string       `xml:"version,attr"`
	Creator  string       `xml:"creator,attr"`
	XMLNS    string       `xml:"xmlns,attr"`
	Metadata gpxMetadata  `xml:"metadata"`
	Waypoint *gpxWaypoint `xml:"wpt,omitempty"`
	Track    *gpxTrack    `xml:"trk,omitempty"`
}

type gpxMetadata struct {
	Name string `xml:"name"`
	Time string `xml:"time"`
}

type gpxWaypoint struct {
	Lat       string `xml:"lat,attr"`
	Lon       string `xml:"lon,attr"`
	Elevation string `xml:"ele"`
	Time      string `xml:"time"`
	Name      string `xml:"name"`
}

type gpxTrack struct {
	Name    string     `xml:"name"`
	Segment gpxSegment `xml:"trkseg"`
}

type gpxSegment struct {
	Points []gpxTrackPoint `xml:"trkpt"`
}

type gpxTrackPoint struct {
	Lat       string `xml:"lat,attr"`
	Lon       string `xml:"lon,attr"`
	Elevation string `xml:"ele"`
	Time      string `xml:"time"`
}

func BuildTimedPoints(points []Coordinate, speedKmh, jitterMeters float64) ([]TimedPoint, error) {
	timed, err := BuildTimedRoute(points, speedKmh, time.Now().UTC(), time.Second)
	if err != nil {
		return nil, err
	}
	return ApplyRouteJitter(timed, jitterMeters, 0), nil
}

func GenerateGPX(points []Coordinate, speedKmh float64, name string, jitterMeters float64) (string, error) {
	timed, err := BuildTimedPoints(points, speedKmh, jitterMeters)
	if err != nil {
		return "", err
	}
	return RenderGPX(timed, name)
}

func RenderGPX(points []TimedPoint, name string) (string, error) {
	if len(points) == 0 {
		return "", fmt.Errorf("at least one timed point is required")
	}
	if name == "" {
		name = "GPS Simulation"
	}

	doc := gpxDocument{
		Version: "1.1",
		Creator: "Go iPhone GPS Simulator",
		XMLNS:   "http://www.topografix.com/GPX/1/1",
		Metadata: gpxMetadata{
			Name: name,
			Time: formatGPXTime(points[0].Time),
		},
	}

	if len(points) == 1 {
		point := points[0]
		doc.Waypoint = &gpxWaypoint{
			Lat:       fmt.Sprintf("%.8f", point.Point.Lat),
			Lon:       fmt.Sprintf("%.8f", point.Point.Lon),
			Elevation: fmt.Sprintf("%.2f", point.Point.Elevation),
			Time:      formatGPXTime(point.Time),
			Name:      name,
		}
	} else {
		trackPoints := make([]gpxTrackPoint, 0, len(points))
		for _, point := range points {
			trackPoints = append(trackPoints, gpxTrackPoint{
				Lat:       fmt.Sprintf("%.8f", point.Point.Lat),
				Lon:       fmt.Sprintf("%.8f", point.Point.Lon),
				Elevation: fmt.Sprintf("%.2f", point.Point.Elevation),
				Time:      formatGPXTime(point.Time),
			})
		}
		doc.Track = &gpxTrack{Name: name, Segment: gpxSegment{Points: trackPoints}}
	}

	var buffer bytes.Buffer
	buffer.WriteString(xml.Header)
	encoder := xml.NewEncoder(&buffer)
	encoder.Indent("", "  ")
	if err := encoder.Encode(doc); err != nil {
		return "", err
	}
	buffer.WriteByte('\n')
	return buffer.String(), nil
}

func WriteGPX(path string, points []TimedPoint, name string) error {
	rendered, err := RenderGPX(points, name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(rendered), 0644)
}

func formatGPXTime(unixSeconds int64) string {
	return time.Unix(unixSeconds, 0).UTC().Format(time.RFC3339)
}
