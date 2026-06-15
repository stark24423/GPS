package lockdown

import (
	"bytes"
	"testing"

	"gpssim/internal/ioscore/plist"
)

func TestPacketRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := WritePacket(&buf, plist.Dict{"Request": "GetValue", "Key": "ProductVersion"}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadPacket(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if plist.String(got, "Request") != "GetValue" || plist.String(got, "Key") != "ProductVersion" {
		t.Fatalf("packet = %#v", got)
	}
}
