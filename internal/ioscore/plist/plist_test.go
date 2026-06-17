package plist

import "testing"

func TestMarshalUnmarshalDict(t *testing.T) {
	data, err := Marshal(Dict{
		"MessageType": "Result",
		"PortNumber":  uint32(62078),
		"Enable":      true,
		"Certificate": []byte{1, 2, 3, 4},
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
	if string(Data(got, "Certificate")) != string([]byte{1, 2, 3, 4}) {
		t.Fatalf("Certificate = %#v", Data(got, "Certificate"))
	}
}

func TestUnmarshalXMLData(t *testing.T) {
	got, err := Unmarshal([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
  <key>HostCertificate</key>
  <data>
    AQID
  </data>
</dict>
</plist>`))
	if err != nil {
		t.Fatal(err)
	}
	if string(Data(got, "HostCertificate")) != string([]byte{1, 2, 3}) {
		t.Fatalf("HostCertificate = %#v", Data(got, "HostCertificate"))
	}
}
