package ui

import "testing"

func TestFormatDeviceChoiceLabelShowsConnectionType(t *testing.T) {
	got := formatDeviceChoiceLabel("Stark iPhone", "Network", "00008140")
	want := "Stark iPhone [Wi-Fi] (00008140)"
	if got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
}

func TestDisplayDeviceConnection(t *testing.T) {
	tests := []struct {
		connection string
		want       string
	}{
		{connection: "USB", want: "USB"},
		{connection: "Network", want: "Wi-Fi"},
		{connection: "wireless", want: "Wi-Fi"},
		{connection: "", want: "Unknown"},
	}

	for _, tt := range tests {
		if got := displayDeviceConnection(tt.connection); got != tt.want {
			t.Fatalf("displayDeviceConnection(%q) = %q, want %q", tt.connection, got, tt.want)
		}
	}
}
