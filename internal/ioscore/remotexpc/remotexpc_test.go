package remotexpc

import (
	"encoding/hex"
	"testing"
)

func TestBuildEmptyWrapperMatchesPyMobileDevice3(t *testing.T) {
	got := hex.EncodeToString(buildEmptyWrapper(0x0201))
	want := "920bb0290102000000000000000000000000000000000000"
	if got != want {
		t.Fatalf("empty wrapper mismatch\n got %s\nwant %s", got, want)
	}
}

func TestCreateEmptyDictionaryWrapperMatchesPyMobileDevice3(t *testing.T) {
	got := hex.EncodeToString(createXPCWrapper(map[string]any{}, 0, false))
	want := "920bb0290100000014000000000000000000000000000000423713420500000000f000000400000000000000"
	if got != want {
		t.Fatalf("dict wrapper mismatch\n got %s\nwant %s", got, want)
	}
}

func TestParseNonDataWrapperSkipsPayload(t *testing.T) {
	raw, err := hex.DecodeString("920bb0290102000000000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	value, consumed, err := parseXPCWrapper(raw)
	if err != nil {
		t.Fatal(err)
	}
	if value != nil {
		t.Fatalf("value = %#v, want nil", value)
	}
	if consumed != len(raw) {
		t.Fatalf("consumed = %d, want %d", consumed, len(raw))
	}
}

func TestFormatUUID(t *testing.T) {
	raw, err := hex.DecodeString("00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := formatUUID(raw), "00112233-4455-6677-8899-aabbccddeeff"; got != want {
		t.Fatalf("uuid = %s, want %s", got, want)
	}
}
