package dtx

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"gpssim/internal/ioscore/nskeyedarchive"
)

const (
	locationService = "com.apple.instruments.server.services.LocationSimulation"

	fragmentMagic = 0x1F3D5B79

	msgOK       = 0
	msgDispatch = 2
	msgObject   = 3
	msgError    = 4

	flagExpectsReply = 1
)

type Client struct {
	conn      net.Conn
	nextID    uint32
	nextCode  int32
	replyChan chan message
	logger    func(format string, args ...any)
}

func DialLocation(ctx context.Context, address string, port int, logger func(format string, args ...any)) (*Client, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(address, fmt.Sprint(port)))
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	}
	client := &Client{
		conn:      conn,
		nextID:    1,
		nextCode:  1,
		replyChan: make(chan message, 16),
		logger:    logger,
	}
	go client.readLoop()
	if err := client.handshake(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	if err := client.openChannel(ctx, locationService); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) SetLocation(ctx context.Context, latitude, longitude float64) error {
	return c.dispatch(ctx, 1, "simulateLocationWithLatitude:longitude:", []auxValue{
		archiveArg(latitude),
		archiveArg(longitude),
	}, true)
}

func (c *Client) ClearLocation(ctx context.Context) error {
	return c.dispatch(ctx, 1, "stopLocationSimulation", nil, false)
}

func (c *Client) handshake(ctx context.Context) error {
	capabilities := map[string]any{
		"com.apple.private.DTXBlockCompression":                                      int64(0),
		"com.apple.private.DTXConnection":                                            int64(1),
		"com.apple.instruments.client.processcontrol.capability.terminationCallback": int64(1),
	}
	if err := c.dispatch(ctx, 0, "_notifyOfPublishedCapabilities:", []auxValue{archiveArg(capabilities)}, false); err != nil {
		return err
	}
	// 等待裝置端 capability 通知；若裝置沒有送或封包順序不同，後續 open channel 仍可驗證連線是否可用。
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(300 * time.Millisecond):
		return nil
	case <-c.replyChan:
		return nil
	}
}

func (c *Client) openChannel(ctx context.Context, identifier string) error {
	code := c.nextCode
	c.nextCode++
	return c.dispatch(ctx, 0, "_requestChannelWithCode:identifier:", []auxValue{
		int32Value(code),
		archiveArg(identifier),
	}, true)
}

func (c *Client) dispatch(ctx context.Context, channelCode int32, selector string, args []auxValue, expectsReply bool) error {
	payload, err := nskeyedarchive.Archive(selector)
	if err != nil {
		return err
	}
	aux, err := encodeAux(args)
	if err != nil {
		return err
	}
	id := c.nextID
	c.nextID++
	flags := uint32(0)
	if expectsReply {
		flags = flagExpectsReply
	}
	if err := c.writeMessage(message{
		id:          id,
		msgType:     msgDispatch,
		channelCode: channelCode,
		flags:       flags,
		payload:     payload,
		aux:         aux,
	}); err != nil {
		return err
	}
	if !expectsReply {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case reply := <-c.replyChan:
			if reply.id != id {
				continue
			}
			if reply.msgType == msgError {
				return fmt.Errorf("DVT returned error for %s", selector)
			}
			if reply.msgType == msgOK || reply.msgType == msgObject {
				return nil
			}
		}
	}
}

func (c *Client) writeMessage(msg message) error {
	var payloadHeader bytes.Buffer
	payloadHeader.WriteByte(byte(msg.msgType))
	payloadHeader.Write([]byte{0, 0, 0})
	_ = binary.Write(&payloadHeader, binary.LittleEndian, uint32(len(msg.aux)))
	_ = binary.Write(&payloadHeader, binary.LittleEndian, uint32(len(msg.aux)+len(msg.payload)))
	_ = binary.Write(&payloadHeader, binary.LittleEndian, uint32(0))
	body := append(payloadHeader.Bytes(), msg.aux...)
	body = append(body, msg.payload...)

	header := make([]byte, 32)
	binary.LittleEndian.PutUint32(header[0:4], fragmentMagic)
	binary.LittleEndian.PutUint32(header[4:8], 32)
	binary.LittleEndian.PutUint16(header[8:10], 0)
	binary.LittleEndian.PutUint16(header[10:12], 1)
	binary.LittleEndian.PutUint32(header[12:16], uint32(len(body)))
	binary.LittleEndian.PutUint32(header[16:20], msg.id)
	binary.LittleEndian.PutUint32(header[20:24], msg.conversation)
	binary.LittleEndian.PutUint32(header[24:28], uint32(msg.channelCode))
	binary.LittleEndian.PutUint32(header[28:32], msg.flags)
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(body)
	return err
}

func (c *Client) readLoop() {
	for {
		msg, err := readMessage(c.conn)
		if err != nil {
			return
		}
		if msg.conversation != 0 && (msg.msgType == msgOK || msg.msgType == msgObject || msg.msgType == msgError) {
			c.replyChan <- msg
			continue
		}
		if msg.flags&flagExpectsReply != 0 {
			_ = c.writeMessage(message{
				id:           msg.id,
				conversation: msg.conversation + 1,
				msgType:      msgOK,
				channelCode:  msg.channelCode,
			})
		}
		if c.logger != nil {
			c.logger("DVT native: received async message type=%d channel=%d id=%d", msg.msgType, msg.channelCode, msg.id)
		}
	}
}

type message struct {
	id           uint32
	conversation uint32
	channelCode  int32
	flags        uint32
	msgType      uint8
	aux          []byte
	payload      []byte
}

func readMessage(r io.Reader) (message, error) {
	header := make([]byte, 32)
	if _, err := io.ReadFull(r, header); err != nil {
		return message{}, err
	}
	if binary.LittleEndian.Uint32(header[0:4]) != fragmentMagic {
		return message{}, fmt.Errorf("invalid DTX fragment magic")
	}
	size := int(binary.LittleEndian.Uint32(header[12:16]))
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return message{}, err
	}
	if len(body) < 16 {
		return message{}, fmt.Errorf("DTX body too short")
	}
	auxSize := int(binary.LittleEndian.Uint32(body[4:8]))
	totalSize := int(binary.LittleEndian.Uint32(body[8:12]))
	if totalSize+16 > len(body) || auxSize > totalSize {
		return message{}, fmt.Errorf("DTX body size mismatch")
	}
	return message{
		id:           binary.LittleEndian.Uint32(header[16:20]),
		conversation: binary.LittleEndian.Uint32(header[20:24]),
		channelCode:  int32(binary.LittleEndian.Uint32(header[24:28])),
		flags:        binary.LittleEndian.Uint32(header[28:32]),
		msgType:      body[0],
		aux:          body[16 : 16+auxSize],
		payload:      body[16+auxSize : 16+totalSize],
	}, nil
}

type auxValue interface {
	write(*bytes.Buffer) error
}

type archiveValue struct {
	value any
}
type int32Value int32

func archiveArg(value any) archiveValue {
	return archiveValue{value: value}
}

func (v archiveValue) write(out *bytes.Buffer) error {
	data, err := nskeyedarchive.Archive(v.value)
	if err != nil {
		return err
	}
	_ = binary.Write(out, binary.LittleEndian, uint32(2))
	_ = binary.Write(out, binary.LittleEndian, uint32(len(data)))
	out.Write(data)
	return nil
}

func (v int32Value) write(out *bytes.Buffer) error {
	_ = binary.Write(out, binary.LittleEndian, uint32(3))
	_ = binary.Write(out, binary.LittleEndian, int32(v))
	return nil
}

func encodeAux(values []auxValue) ([]byte, error) {
	if len(values) == 0 {
		return nil, nil
	}
	var body bytes.Buffer
	for _, value := range values {
		_ = binary.Write(&body, binary.LittleEndian, uint32(10))
		if err := value.write(&body); err != nil {
			return nil, err
		}
	}
	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, uint32(0x1F0))
	_ = binary.Write(&out, binary.LittleEndian, uint32(0))
	_ = binary.Write(&out, binary.LittleEndian, uint64(body.Len()))
	out.Write(body.Bytes())
	return out.Bytes(), nil
}
