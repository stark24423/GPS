package usbmux

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"

	"gpssim/internal/ioscore/plist"
)

const (
	defaultAddress = "127.0.0.1:27015"

	protocolVersion  = uint32(1)
	messageTypePlist = uint32(8)
)

type Client struct {
	Address string
	tag     atomic.Uint32
}

type Device struct {
	DeviceID       uint32
	SerialNumber   string
	ConnectionType string
}

func New(address string) *Client {
	if address == "" {
		address = defaultAddress
	}
	return &Client{Address: address}
}

func (c *Client) ListDevices(ctx context.Context) ([]Device, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	response, err := c.roundTrip(conn, plist.Dict{"MessageType": "ListDevices", "ClientVersionString": "gpssim-go"})
	if err != nil {
		return nil, err
	}

	rawList, ok := response["DeviceList"].([]plist.Value)
	if !ok {
		return nil, fmt.Errorf("usbmux DeviceList missing")
	}
	devices := make([]Device, 0, len(rawList))
	for _, raw := range rawList {
		item, ok := raw.(plist.Dict)
		if !ok {
			continue
		}
		props, _ := item["Properties"].(plist.Dict)
		devices = append(devices, Device{
			DeviceID:       uint32(plist.Int(item, "DeviceID")),
			SerialNumber:   plist.String(props, "SerialNumber"),
			ConnectionType: plist.String(props, "ConnectionType"),
		})
	}
	return devices, nil
}

func (c *Client) ConnectLockdown(ctx context.Context, device Device) (net.Conn, error) {
	return c.ConnectPort(ctx, device, 62078)
}

func (c *Client) ConnectPort(ctx context.Context, device Device, port uint16) (net.Conn, error) {
	muxConn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}

	// usbmux 的 PortNumber 使用 network byte order，但整包 plist 仍包在 little-endian usbmux header 裡。
	// 這個轉換錯誤時會連到錯誤 port，lockdown/service 會表現成無回應。
	response, err := c.roundTrip(muxConn, plist.Dict{
		"MessageType":         "Connect",
		"ClientVersionString": "gpssim-go",
		"DeviceID":            int(device.DeviceID),
		"PortNumber":          uint32(htons(port)),
	})
	if err != nil {
		muxConn.Close()
		return nil, err
	}
	if number := plist.Int(response, "Number"); number != 0 {
		muxConn.Close()
		return nil, fmt.Errorf("usbmux connect failed with Number=%d", number)
	}
	return muxConn, nil
}

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", c.Address)
	if err != nil {
		return nil, fmt.Errorf("connect usbmux at %s: %w", c.Address, err)
	}
	return conn, nil
}

func (c *Client) roundTrip(conn net.Conn, payload plist.Dict) (plist.Dict, error) {
	tag := c.tag.Add(1)
	if err := WriteMessage(conn, tag, payload); err != nil {
		return nil, err
	}
	_, response, err := ReadMessage(conn)
	return response, err
}

func WriteMessage(w io.Writer, tag uint32, payload plist.Dict) error {
	body, err := plist.Marshal(payload)
	if err != nil {
		return err
	}
	var header bytes.Buffer
	length := uint32(16 + len(body))
	// usbmux header 固定 16 bytes 且為 little-endian；payload 才是 XML plist。
	// 測試保留這層 framing，避免未來改 protocol 時破壞 device list/connect。
	for _, value := range []uint32{length, protocolVersion, messageTypePlist, tag} {
		if err := binary.Write(&header, binary.LittleEndian, value); err != nil {
			return err
		}
	}
	if _, err := w.Write(header.Bytes()); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

func ReadMessage(r io.Reader) (uint32, plist.Dict, error) {
	header := make([]byte, 16)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}
	length := binary.LittleEndian.Uint32(header[0:4])
	version := binary.LittleEndian.Uint32(header[4:8])
	messageType := binary.LittleEndian.Uint32(header[8:12])
	tag := binary.LittleEndian.Uint32(header[12:16])
	if version != protocolVersion || messageType != messageTypePlist {
		return 0, nil, fmt.Errorf("unexpected usbmux header version=%d type=%d", version, messageType)
	}
	if length < 16 || length > 16*1024*1024 {
		return 0, nil, fmt.Errorf("invalid usbmux message length %d", length)
	}
	body := make([]byte, int(length)-16)
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}
	payload, err := plist.Unmarshal(body)
	return tag, payload, err
}

func htons(port uint16) uint16 {
	return (port<<8)&0xff00 | port>>8
}
