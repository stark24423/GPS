package coredevice

import (
	"bytes"
	"io"
	"testing"
)

type handshakeStream struct {
	read  *bytes.Reader
	write bytes.Buffer
}

func (s *handshakeStream) Read(p []byte) (int, error) {
	return s.read.Read(p)
}

func (s *handshakeStream) Write(p []byte) (int, error) {
	return s.write.Write(p)
}

func TestExchangeParameters(t *testing.T) {
	response := append([]byte(magic), byte(len(`{"serverAddress":"fd00::1","serverRSDPort":58783,"clientParameters":{"address":"fd00::2","netmask":"ffff:ffff:ffff:ffff::","mtu":1280}}`)))
	response = append(response, []byte(`{"serverAddress":"fd00::1","serverRSDPort":58783,"clientParameters":{"address":"fd00::2","netmask":"ffff:ffff:ffff:ffff::","mtu":1280}}`)...)
	stream := &handshakeStream{read: bytes.NewReader(response)}

	got, err := ExchangeParameters(stream)
	if err != nil {
		t.Fatal(err)
	}
	if got.ServerAddress != "fd00::1" || got.ServerRSDPort != 58783 || got.ClientParameters.MTU != 1280 {
		t.Fatalf("parameters = %+v", got)
	}

	written := stream.write.Bytes()
	if !bytes.HasPrefix(written, []byte(magic)) {
		t.Fatalf("request missing magic: %q", written)
	}
	if _, err := io.ReadAll(bytes.NewReader(written)); err != nil {
		t.Fatal(err)
	}
}
