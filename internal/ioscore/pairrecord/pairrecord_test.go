package pairrecord

import "testing"

func TestPairRecordRequiresUDID(t *testing.T) {
	if _, err := Read(""); err == nil {
		t.Fatalf("expected error")
	}
}
