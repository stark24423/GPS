package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderSinglePointGPXContainsWaypoint(t *testing.T) {
	xml, err := RenderGPX([]TimedPoint{{
		Point: Coordinate{Lat: 25.033, Lon: 121.5654},
		Time:  time.Unix(1700000000, 0).Unix(),
	}}, "GPS Simulation")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(xml, `<wpt lat="25.03300000" lon="121.56540000">`) {
		t.Fatalf("waypoint missing from GPX:\n%s", xml)
	}
	if !strings.Contains(xml, "<time>") {
		t.Fatalf("time missing from GPX")
	}
}

func TestRenderRouteGPXContainsTrackPoints(t *testing.T) {
	xml, err := GenerateGPX([]Coordinate{
		{Lat: 25.033, Lon: 121.5654},
		{Lat: 25.034, Lon: 121.5664},
	}, 5, "GPS Simulation", 0)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(xml, "<trk>") || !strings.Contains(xml, `<trkpt lat="25.03300000" lon="121.56540000">`) {
		t.Fatalf("track missing from GPX:\n%s", xml)
	}
}

func TestPruneGeneratedFilesKeepsNewestMatches(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		path := filepath.Join(dir, fmt.Sprintf("simulation_%02d.gpx", i))
		if err := os.WriteFile(path, []byte("gpx"), 0644); err != nil {
			t.Fatal(err)
		}
		mtime := time.Unix(int64(i), 0)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	manualPath := filepath.Join(dir, "manual_route.gpx")
	if err := os.WriteFile(manualPath, []byte("manual"), 0644); err != nil {
		t.Fatal(err)
	}

	deleted, err := PruneGeneratedFiles(dir, "simulation_*.gpx", 2)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 3 {
		t.Fatalf("deleted = %d, want 3", deleted)
	}
	for _, name := range []string{"simulation_03.gpx", "simulation_04.gpx", "manual_route.gpx"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s should remain: %v", name, err)
		}
	}
	for _, name := range []string{"simulation_00.gpx", "simulation_01.gpx", "simulation_02.gpx"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should be removed, stat err=%v", name, err)
		}
	}
}
