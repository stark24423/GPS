package lockdown

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"

	"gpssim/internal/ioscore/pairrecord"
	"gpssim/internal/ioscore/plist"
	"gpssim/internal/ioscore/usbmux"
)

const coreDeviceProxyService = "com.apple.internal.devicecompute.CoreDeviceProxy"

type Client struct {
	conn      io.ReadWriteCloser
	ctx       context.Context
	mux       *usbmux.Client
	device    usbmux.Device
	sessionID string
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
	return &Client{conn: conn, ctx: ctx, mux: mux, device: device}, nil
}

func New(conn io.ReadWriteCloser) *Client {
	return &Client{conn: conn}
}

func (c *Client) Close() error {
	if c.sessionID != "" {
		_ = WritePacket(c.conn, plist.Dict{
			"Request":   "StopSession",
			"SessionID": c.sessionID,
		})
	}
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
	if err := c.StartSession(); err != nil {
		return nil, err
	}
	return c.StartService(coreDeviceProxyService)
}

func (c *Client) StartSession() error {
	record, err := pairrecord.Read(c.device.SerialNumber)
	if err != nil {
		return err
	}
	response, err := c.RoundTrip(plist.Dict{
		"Request":         "StartSession",
		"Label":           "gpssim-go",
		"HostID":          record.HostID,
		"SystemBUID":      record.SystemBUID,
		"ProtocolVersion": "2",
	})
	if err != nil {
		return err
	}
	if errText := plist.String(response, "Error"); errText != "" {
		return fmt.Errorf("start lockdown session: %s", errText)
	}
	c.sessionID = plist.String(response, "SessionID")
	if enabled, _ := response["EnableSessionSSL"].(bool); enabled {
		tlsConn, err := createTLSClient(assertNetConn(c.conn), record)
		if err != nil {
			return fmt.Errorf("lockdown TLS handshake: %w", err)
		}
		c.conn = tlsConn
	}
	if c.sessionID == "" {
		return fmt.Errorf("start lockdown session did not return SessionID")
	}
	return nil
}

func (c *Client) StartService(service string) (net.Conn, error) {
	if c.mux == nil {
		return nil, fmt.Errorf("lockdown client was not created with usbmux context")
	}
	response, err := c.RoundTrip(plist.Dict{
		"Request": "StartService",
		"Label":   "gpssim-go",
		"Service": service,
	})
	if err != nil {
		return nil, err
	}
	if errText := plist.String(response, "Error"); errText != "" {
		return nil, fmt.Errorf("start %s: %s", service, errText)
	}
	port := plist.Int(response, "Port")
	if port == 0 {
		port = plist.Int(response, "PortNumber")
	}
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("start %s did not return a valid service port", service)
	}

	// StartService 只回報服務所在 port；真正的服務資料流需要另外開一條 usbmux connection。
	// 若服務要求 SSL，必須先完成 TLS handshake，再交給上層 protocol 使用。
	serviceConn, err := c.mux.ConnectPort(c.ctx, c.device, uint16(port))
	if err != nil {
		return nil, err
	}
	if enabled, _ := response["EnableServiceSSL"].(bool); enabled {
		record, err := pairrecord.Read(c.device.SerialNumber)
		if err != nil {
			serviceConn.Close()
			return nil, err
		}
		tlsConn, err := createTLSClient(serviceConn, record)
		if err != nil {
			serviceConn.Close()
			return nil, fmt.Errorf("service TLS handshake for %s: %w", service, err)
		}
		return tlsConn, nil
	}
	return serviceConn, nil
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

func createTLSClient(conn net.Conn, record pairrecord.PairRecord) (*tls.Conn, error) {
	if conn == nil {
		return nil, fmt.Errorf("connection does not support TLS upgrade")
	}
	cert, err := tls.X509KeyPair(record.HostCertificate, record.HostPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("load pair record TLS identity: %w", err)
	}
	tlsConn := tls.Client(conn, &tls.Config{
		InsecureSkipVerify: true,
		Certificates:       []tls.Certificate{cert},
	})
	if err := tlsConn.Handshake(); err != nil {
		return nil, err
	}
	return tlsConn, nil
}

func assertNetConn(conn io.ReadWriteCloser) net.Conn {
	netConn, _ := conn.(net.Conn)
	return netConn
}
