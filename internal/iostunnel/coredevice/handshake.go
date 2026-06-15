package coredevice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const (
	magic      = "CDTunnel\x00"
	defaultMTU = 1280
)

type Parameters struct {
	ServerAddress    string `json:"serverAddress"`
	ServerRSDPort    uint64 `json:"serverRSDPort"`
	ClientParameters struct {
		Address string `json:"address"`
		Netmask string `json:"netmask"`
		MTU     uint64 `json:"mtu"`
	} `json:"clientParameters"`
}

func ExchangeParameters(stream io.ReadWriter) (Parameters, error) {
	request, err := json.Marshal(map[string]any{
		"type": "clientHandshakeRequest",
		"mtu":  defaultMTU,
	})
	if err != nil {
		return Parameters{}, err
	}
	if len(request) > 255 {
		return Parameters{}, fmt.Errorf("CoreDevice handshake request too large: %d", len(request))
	}

	var packet bytes.Buffer
	packet.WriteString(magic)
	packet.WriteByte(byte(len(request)))
	packet.Write(request)
	if _, err := stream.Write(packet.Bytes()); err != nil {
		return Parameters{}, fmt.Errorf("write CoreDevice handshake: %w", err)
	}

	header := make([]byte, len(magic)+1)
	if _, err := io.ReadFull(stream, header); err != nil {
		return Parameters{}, fmt.Errorf("read CoreDevice handshake header: %w", err)
	}
	if string(header[:len(magic)]) != magic {
		return Parameters{}, fmt.Errorf("invalid CoreDevice handshake magic %q", string(header[:len(magic)]))
	}
	bodyLen := int(header[len(header)-1])
	if bodyLen == 0 {
		return Parameters{}, fmt.Errorf("empty CoreDevice handshake body")
	}

	body := make([]byte, bodyLen)
	if _, err := io.ReadFull(stream, body); err != nil {
		return Parameters{}, fmt.Errorf("read CoreDevice handshake body: %w", err)
	}

	var parameters Parameters
	if err := json.Unmarshal(body, &parameters); err != nil {
		return Parameters{}, fmt.Errorf("parse CoreDevice handshake body: %w", err)
	}
	if parameters.ServerAddress == "" || parameters.ServerRSDPort == 0 || parameters.ClientParameters.MTU == 0 {
		return Parameters{}, fmt.Errorf("CoreDevice handshake response missing required fields")
	}
	return parameters, nil
}
