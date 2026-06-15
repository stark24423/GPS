package plist

import "testing"

func TestMarshalUnmarshalDict(t *testing.T) {
	data, err := Marshal(Dict{
		"MessageType": "Result",
		"PortNumber":  uint32(62078),
		"Enable":      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if String(got, "MessageType") != "Result" {
		t.Fatalf("MessageType = %q", String(got, "MessageType"))
	}
	if Int(got, "PortNumber") != 62078 {
		t.Fatalf("PortNumber = %d", Int(got, "PortNumber"))
	}
	if got["Enable"] != true {
		t.Fatalf("Enable = %#v", got["Enable"])
	}
}
