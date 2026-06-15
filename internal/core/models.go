package core

type Coordinate struct {
	Lat       float64
	Lon       float64
	Elevation float64
}

type TimedPoint struct {
	Point Coordinate
	Time  int64
}

type DeviceInfo struct {
	ID         string
	Name       string
	Kind       string
	Connected  bool
	Connection string
}

type BridgeResult struct {
	OK      bool
	Message string
	Detail  string
}

type RequirementStatus struct {
	Name    string
	OK      bool
	Message string
}
