package iosbridge

import "testing"

func TestBridgeStoresUDID(t *testing.T) {
	b := New()
	b.SetUDID("abc")
	if got := b.UDID(); got != "abc" {
		t.Fatalf("UDID = %q", got)
	}
}
