package lockdown

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"

	"gpssim/internal/ioscore/plist"
	"gpssim/internal/ioscore/usbmux"
)

const coreDeviceProxyService = "com.apple.internal.devicecompute.CoreDeviceProxy"

type Client struct {
	conn io.ReadWriteCloser
}

type DeviceValues struct {
	DeviceName      string
	ProductVersion  string
	DeveloperStatus string
}

func Dial(ctx context.Context, mux *usbmux.Client, device usbmux.Device) (*Client, error) {
	conn, err := mux.ConnectLockdown(ctx, device)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn}, nil
}

func New(conn io.ReadWriteCloser) *Client {
	return &Client{conn: conn}
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) QueryValues() (DeviceValues, error) {
	name, _ := c.GetValue("", "DeviceName")
	version, _ := c.GetValue("", "ProductVersion")
	devMode, _ := c.GetValue("com.apple.security.mac.amfi", "DeveloperModeStatus")
	return DeviceValues{
		DeviceName:      fmt.Sprint(name),
		ProductVersion:  fmt.Sprint(version),
		DeveloperStatus: fmt.Sprint(devMode),
	}, nil
}

func (c *Client) GetValue(domain, key string) (any, error) {
	request := plist.Dict{"Request": "GetValue"}
	if domain != "" {
		request["Domain"] = domain
	}
	if key != "" {
		request["Key"] = key
	}
	response, err := c.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if errText := plist.String(response, "Error"); errText != "" {
		return nil, fmt.Errorf("lockdown GetValue %s: %s", key, errText)
	}
	return response["Value"], nil
}

func (c *Client) StartCoreDeviceProxy() (net.Conn, error) {
	response, err := c.RoundTrip(plist.Dict{
		"Request": "StartService",
		"Service": coreDeviceProxyService,
	})
	if err != nil {
		return nil, err
	}
	if errText := plist.String(response, "Error"); errText != "" {
		return nil, fmt.Errorf("start %s: %s", coreDeviceProxyService, errText)
	}
	// For usbmux lockdown services, the existing connection becomes the service stream.
	// Some lockdownd variants instead return a port; that path can be added once validated
	// against hardware traces.
	if plist.Int(response, "Port") == 0 && plist.Int(response, "PortNumber") == 0 {
		return nil, fmt.Errorf("start %s did not return a service port", coreDeviceProxyService)
	}
	return nil, fmt.Errorf("CoreDeviceProxy service stream handoff is not implemented for raw usbmux yet")
}

func (c *Client) RoundTrip(request plist.Dict) (plist.Dict, error) {
	if err := WritePacket(c.conn, request); err != nil {
		return nil, err
	}
	return ReadPacket(c.conn)
}

func WritePacket(w io.Writer, payload plist.Dict) error {
	body, err := plist.Marshal(payload)
	if err != nil {
		return err
	}
	var header [4]byte
	// lockdown 封包前 4 bytes 是 big-endian 長度，後面才是 plist。
	// 這裡先完整寫入長度與 plist，讓對端 lockdownd 能一次解析完整 request。
	binary.BigEndian.PutUint32(header[:], uint32(len(body)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

func ReadPacket(r io.Reader) (plist.Dict, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 || length > 16*1024*1024 {
		return nil, fmt.Errorf("invalid lockdown packet length %d", length)
	}
	// 一定要讀滿 plist 長度再交給 XML parser；半包會讓錯誤訊息難以判斷。
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return plist.Unmarshal(body)
}
