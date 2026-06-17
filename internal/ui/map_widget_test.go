package ui

import (
	"math"
	"testing"

	"fyne.io/fyne/v2"

	"gpssim/internal/core"
)

func TestMapWidgetScrolledZoomsAroundPointer(t *testing.T) {
	m := NewMapWidget(nil)
	size := fyne.NewSize(800, 600)
	pos := fyne.NewPos(600, 150)
	m.Resize(size)

	before := m.coordinateAt(pos, size)
	m.Scrolled(&fyne.ScrollEvent{
		PointEvent: fyne.PointEvent{Position: pos},
		Scrolled:   fyne.NewDelta(0, 1),
	})
	after := m.coordinateAt(pos, size)

	if m.zoom != 18 {
		t.Fatalf("expected zoom 18 after scroll up, got %d", m.zoom)
	}
	if math.Abs(before.Lat-after.Lat) > 0.000001 || math.Abs(before.Lon-after.Lon) > 0.000001 {
		t.Fatalf("expected pointer coordinate to stay anchored, before=%+v after=%+v", before, after)
	}
}

func TestMapWidgetTappedScalesEventPositionToRenderedPixels(t *testing.T) {
	var gotPosition bool
	var gotLat, gotLon float64
	m := NewMapWidget(func(point core.Coordinate) {
		gotPosition = true
		gotLat = point.Lat
		gotLon = point.Lon
	})
	m.Resize(fyne.NewSize(400, 300))
	m.setRenderSize(800, 600)

	expected := m.coordinateAt(fyne.NewPos(600, 150), fyne.NewSize(800, 600))
	m.Tapped(&fyne.PointEvent{Position: fyne.NewPos(300, 75)})

	if !gotPosition {
		t.Fatal("expected tap callback")
	}
	if math.Abs(expected.Lat-gotLat) > 0.000001 || math.Abs(expected.Lon-gotLon) > 0.000001 {
		t.Fatalf("expected scaled tap coordinate %+v, got lat=%f lon=%f", expected, gotLat, gotLon)
	}
}
