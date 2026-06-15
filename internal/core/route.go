package core

import (
	"errors"
	"math"
	"math/rand"
	"time"
)

const EarthRadiusMeters = 6371000.0

func HaversineDistanceMeters(start, end Coordinate) float64 {
	lat1 := degreesToRadians(start.Lat)
	lat2 := degreesToRadians(end.Lat)
	dLat := degreesToRadians(end.Lat - start.Lat)
	dLon := degreesToRadians(end.Lon - start.Lon)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * EarthRadiusMeters * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func InterpolatePoints(start, end Coordinate, stepMeters float64) ([]Coordinate, error) {
	if stepMeters <= 0 {
		return nil, errors.New("stepMeters must be greater than zero")
	}

	distance := HaversineDistanceMeters(start, end)
	if distance == 0 {
		return []Coordinate{start}, nil
	}

	segments := int(math.Ceil(distance / stepMeters))
	if segments < 1 {
		segments = 1
	}

	points := make([]Coordinate, 0, segments+1)
	for i := 0; i <= segments; i++ {
		ratio := float64(i) / float64(segments)
		points = append(points, Coordinate{
			Lat:       start.Lat + (end.Lat-start.Lat)*ratio,
			Lon:       start.Lon + (end.Lon-start.Lon)*ratio,
			Elevation: start.Elevation + (end.Elevation-start.Elevation)*ratio,
		})
	}

	return points, nil
}

func BuildTimedRoute(points []Coordinate, speedKmh float64, start time.Time, tick time.Duration) ([]TimedPoint, error) {
	if len(points) == 0 {
		return nil, errors.New("at least one coordinate is required")
	}
	if speedKmh <= 0 {
		return nil, errors.New("speedKmh must be greater than zero")
	}
	if tick <= 0 {
		return nil, errors.New("tick must be greater than zero")
	}
	if start.IsZero() {
		start = time.Now().UTC()
	}
	start = start.UTC()

	speedMps := speedKmh * 1000 / 3600
	stepMeters := speedMps * tick.Seconds()
	current := start
	timed := []TimedPoint{{Point: points[0], Time: current.Unix()}}

	for i := 0; i < len(points)-1; i++ {
		segmentPoints, err := InterpolatePoints(points[i], points[i+1], stepMeters)
		if err != nil {
			return nil, err
		}
		for _, point := range segmentPoints[1:] {
			current = current.Add(tick)
			timed = append(timed, TimedPoint{Point: point, Time: current.Unix()})
		}
	}

	return timed, nil
}

func JitterCoordinate(point Coordinate, radiusMeters float64, rng *rand.Rand) Coordinate {
	if radiusMeters <= 0 {
		return point
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	distance := rng.Float64() * radiusMeters
	bearing := rng.Float64() * 2 * math.Pi
	northMeters := math.Cos(bearing) * distance
	eastMeters := math.Sin(bearing) * distance

	latOffset := northMeters / 111320
	lonScale := math.Max(0.01, math.Cos(degreesToRadians(point.Lat)))
	lonOffset := eastMeters / (111320 * lonScale)

	return Coordinate{
		Lat:       point.Lat + latOffset,
		Lon:       point.Lon + lonOffset,
		Elevation: point.Elevation,
	}
}

func ApplyRouteJitter(points []TimedPoint, radiusMeters float64, seed int64) []TimedPoint {
	if radiusMeters <= 0 || len(points) < 3 {
		return points
	}

	var rng *rand.Rand
	if seed != 0 {
		rng = rand.New(rand.NewSource(seed))
	}

	jittered := make([]TimedPoint, len(points))
	copy(jittered, points)
	for i := 1; i < len(points)-1; i++ {
		jittered[i].Point = JitterCoordinate(points[i].Point, radiusMeters, rng)
	}

	return jittered
}

func TimedPointsCoordinates(points []TimedPoint) []Coordinate {
	coords := make([]Coordinate, len(points))
	for i, point := range points {
		coords[i] = point.Point
	}
	return coords
}

func OffsetCoordinate(point Coordinate, eastMeters, northMeters float64) Coordinate {
	latOffset := northMeters / 111320
	lonScale := math.Max(0.01, math.Cos(degreesToRadians(point.Lat)))
	lonOffset := eastMeters / (111320 * lonScale)
	return Coordinate{Lat: point.Lat + latOffset, Lon: point.Lon + lonOffset, Elevation: point.Elevation}
}

func degreesToRadians(value float64) float64 {
	return value * math.Pi / 180
}
