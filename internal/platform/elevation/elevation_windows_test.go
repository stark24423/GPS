//go:build windows

package elevation

import "testing"

func TestJoinArgsQuotesSpaces(t *testing.T) {
	got := joinArgs([]string{"plain", "has space", `has"quote`})
	want := `plain "has space" "has\"quote"`
	if got != want {
		t.Fatalf("joinArgs = %q, want %q", got, want)
	}
}
