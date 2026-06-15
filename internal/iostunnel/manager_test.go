package iostunnel

import (
	"testing"
)

func TestVersionAtLeast(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"17.3", false},
		{"17.4", true},
		{"17.4.1", true},
		{"18.0", true},
		{"16.7.8", false},
	}
	for _, tt := range tests {
		if got := versionAtLeast(tt.version, minimumIOSVersion); got != tt.want {
			t.Fatalf("versionAtLeast(%q) = %v, want %v", tt.version, got, tt.want)
		}
	}
}

func TestStopClearsStatus(t *testing.T) {
	m := NewManager()
	m.active["abc"] = TunnelInfo{UDID: "abc"}
	if err := m.Stop("abc"); err != nil {
		t.Fatal(err)
	}
	if got := m.Status(); len(got) != 0 {
		t.Fatalf("status len = %d", len(got))
	}
}
