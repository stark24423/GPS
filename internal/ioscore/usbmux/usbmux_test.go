package usbmux

import (
	"bytes"
	"testing"

	"gpssim/internal/ioscore/plist"
)

func TestMessageRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	err := WriteMessage(&buf, 7, plist.Dict{"MessageType": "ListDevices"})
	if err != nil {
		t.Fatal(err)
	}

	tag, payload, err := ReadMessage(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if tag != 7 {
		t.Fatalf("tag = %d", tag)
	}
	if plist.String(payload, "MessageType") != "ListDevices" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestHtons(t *testing.T) {
	if got := htons(62078); got != 0x7ef2 {
		t.Fatalf("htons(62078) = %#x", got)
	}
}
