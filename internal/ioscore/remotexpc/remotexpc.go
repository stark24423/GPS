package remotexpc

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	rootStream  = 1
	replyStream = 3

	frameData         = 0x0
	frameHeaders      = 0x1
	frameSettings     = 0x4
	frameWindowUpdate = 0x8

	flagEndHeaders = 0x4
	flagAck        = 0x1

	xpcWrapperMagic = 0x29B00B92
	xpcPayloadMagic = 0x42133742
	xpcProtocol     = 0x00000005
	xpcDataPresent  = 0x00000100
)

type Service struct {
	Name          string
	Port          int
	AvailableKeys []string
	PeerInfoKeys  []string
}

func DiscoverService(ctx context.Context, address string, port int, serviceName string) (Service, error) {
	return DiscoverAnyService(ctx, address, port, serviceName)
}

func DiscoverAnyService(ctx context.Context, address string, port int, serviceNames ...string) (Service, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(address, fmt.Sprint(port)))
	if err != nil {
		return Service{}, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	}
	if err := handshake(conn); err != nil {
		return Service{}, err
	}
	peerInfo, err := receivePeerInfo(conn)
	if err != nil {
		return Service{}, err
	}
	services, _ := peerInfo["Services"].(map[string]any)
	available := sortedKeys(services)
	peerKeys := sortedKeys(peerInfo)
	for _, serviceName := range serviceNames {
		raw, ok := services[serviceName].(map[string]any)
		if !ok {
			continue
		}
		portValue, ok := intValue(raw["Port"])
		if !ok || portValue <= 0 {
			return Service{AvailableKeys: available, PeerInfoKeys: peerKeys}, fmt.Errorf("RSD service %q has invalid Port: %v (%T)", serviceName, raw["Port"], raw["Port"])
		}
		return Service{Name: serviceName, Port: portValue, AvailableKeys: available, PeerInfoKeys: peerKeys}, nil
	}
	if len(serviceNames) == 0 {
		return Service{AvailableKeys: available, PeerInfoKeys: peerKeys}, fmt.Errorf("no RSD service candidates provided")
	}
	return Service{AvailableKeys: available, PeerInfoKeys: peerKeys}, fmt.Errorf("RSD services %q not found; peer keys: %s; available services: %s", strings.Join(serviceNames, ", "), strings.Join(peerKeys, ", "), strings.Join(available, ", "))
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func handshake(conn net.Conn) error {
	if _, err := conn.Write([]byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")); err != nil {
		return err
	}
	settings := make([]byte, 12)
	binary.BigEndian.PutUint16(settings[0:2], 0x3)
	binary.BigEndian.PutUint32(settings[2:6], 100)
	binary.BigEndian.PutUint16(settings[6:8], 0x4)
	binary.BigEndian.PutUint32(settings[8:12], 16*1024*1024)
	if err := writeFrame(conn, frameSettings, 0, 0, settings); err != nil {
		return err
	}
	window := make([]byte, 4)
	binary.BigEndian.PutUint32(window, 16*1024*1024-65535)
	if err := writeFrame(conn, frameWindowUpdate, 0, 0, window); err != nil {
		return err
	}
	if err := writeFrame(conn, frameHeaders, flagEndHeaders, rootStream, nil); err != nil {
		return err
	}
	if err := writeFrame(conn, frameData, 0, rootStream, createXPCWrapper(map[string]any{}, 0, false)); err != nil {
		return err
	}
	if err := writeFrame(conn, frameHeaders, flagEndHeaders, replyStream, nil); err != nil {
		return err
	}
	if err := writeFrame(conn, frameData, 0, rootStream, buildEmptyWrapper(0x0201)); err != nil {
		return err
	}
	if err := writeFrame(conn, frameData, 0, replyStream, buildEmptyWrapper(0x00400001)); err != nil {
		return err
	}
	for {
		frame, err := readFrame(conn)
		if err != nil {
			return err
		}
		if frame.typ == frameSettings {
			return writeFrame(conn, frameSettings, flagAck, 0, nil)
		}
	}
}

func receiveDictionary(conn net.Conn) (map[string]any, error) {
	var pending []byte
	for {
		frame, err := readFrame(conn)
		if err != nil {
			return nil, err
		}
		if frame.typ != frameData {
			continue
		}
		pending = append(pending, frame.payload...)
		for len(pending) > 0 {
			wrapper, consumed, err := parseXPCWrapper(pending)
			if err != nil {
				if err == io.ErrUnexpectedEOF {
					break
				}
				return nil, err
			}
			pending = pending[consumed:]
			if wrapper == nil {
				continue
			}
			return wrapper, nil
		}
	}
}

func receivePeerInfo(conn net.Conn) (map[string]any, error) {
	for {
		dict, err := receiveDictionary(conn)
		if err != nil {
			return nil, err
		}
		if _, ok := dict["Services"].(map[string]any); ok {
			return dict, nil
		}
	}
}

type h2Frame struct {
	typ     byte
	flags   byte
	stream  uint32
	payload []byte
}

func writeFrame(w io.Writer, typ byte, flags byte, stream uint32, payload []byte) error {
	header := make([]byte, 9)
	length := len(payload)
	header[0] = byte(length >> 16)
	header[1] = byte(length >> 8)
	header[2] = byte(length)
	header[3] = typ
	header[4] = flags
	binary.BigEndian.PutUint32(header[5:9], stream&0x7fffffff)
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func readFrame(r io.Reader) (h2Frame, error) {
	header := make([]byte, 9)
	if _, err := io.ReadFull(r, header); err != nil {
		return h2Frame{}, err
	}
	length := int(header[0])<<16 | int(header[1])<<8 | int(header[2])
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return h2Frame{}, err
	}
	return h2Frame{
		typ:     header[3],
		flags:   header[4],
		stream:  binary.BigEndian.Uint32(header[5:9]) & 0x7fffffff,
		payload: payload,
	}, nil
}

func createXPCWrapper(dict map[string]any, messageID uint64, wantingReply bool) []byte {
	flags := uint32(0x1)
	if len(dict) > 0 {
		flags |= 0x100
	}
	if wantingReply {
		flags |= 0x10000
	}
	payload := encodeXPCPayload(dict)
	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, uint32(xpcWrapperMagic))
	_ = binary.Write(&out, binary.LittleEndian, flags)
	_ = binary.Write(&out, binary.LittleEndian, uint64(len(payload)))
	_ = binary.Write(&out, binary.LittleEndian, messageID)
	out.Write(payload)
	return out.Bytes()
}

func buildEmptyWrapper(flags uint32) []byte {
	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, uint32(xpcWrapperMagic))
	_ = binary.Write(&out, binary.LittleEndian, flags)
	// XpcWrapper 的 length 欄位只記 payload 長度；message_id 仍固定佔 8 bytes。
	_ = binary.Write(&out, binary.LittleEndian, uint64(0))
	_ = binary.Write(&out, binary.LittleEndian, uint64(0))
	return out.Bytes()
}

func encodeXPCPayload(dict map[string]any) []byte {
	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, uint32(xpcPayloadMagic))
	_ = binary.Write(&out, binary.LittleEndian, uint32(xpcProtocol))
	encodeXPCObject(&out, dict)
	return out.Bytes()
}

func encodeXPCObject(out *bytes.Buffer, value any) {
	switch v := value.(type) {
	case map[string]any:
		_ = binary.Write(out, binary.LittleEndian, uint32(0x0000F000))
		var body bytes.Buffer
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		_ = binary.Write(&body, binary.LittleEndian, uint32(len(keys)))
		for _, key := range keys {
			body.WriteString(key)
			body.WriteByte(0)
			for body.Len()%4 != 0 {
				body.WriteByte(0)
			}
			encodeXPCObject(&body, v[key])
		}
		_ = binary.Write(out, binary.LittleEndian, uint32(body.Len()))
		out.Write(body.Bytes())
	default:
		_ = binary.Write(out, binary.LittleEndian, uint32(0x00001000))
	}
}

func parseXPCWrapper(data []byte) (map[string]any, int, error) {
	if len(data) < 16 {
		return nil, 0, io.ErrUnexpectedEOF
	}
	if binary.LittleEndian.Uint32(data[0:4]) != xpcWrapperMagic {
		return nil, 0, fmt.Errorf("invalid XPC wrapper magic")
	}
	flags := binary.LittleEndian.Uint32(data[4:8])
	size := int(binary.LittleEndian.Uint64(data[8:16]))
	if size < 0 {
		return nil, 0, fmt.Errorf("invalid XPC wrapper size %d", size)
	}
	total := 24 + size
	if len(data) < total {
		return nil, 0, io.ErrUnexpectedEOF
	}
	if size == 0 || flags&xpcDataPresent == 0 {
		return nil, total, nil
	}
	payload := data[24:total]
	reader := bytes.NewReader(payload)
	var magic uint32
	var protocol uint32
	_ = binary.Read(reader, binary.LittleEndian, &magic)
	_ = binary.Read(reader, binary.LittleEndian, &protocol)
	if magic != xpcPayloadMagic || protocol != xpcProtocol {
		return nil, 0, fmt.Errorf("invalid XPC payload header flags=0x%x size=%d prefix=%s", flags, size, hexPrefix(payload, 24))
	}
	value, err := decodeXPCObject(reader)
	if err != nil {
		return nil, 0, err
	}
	dict, ok := value.(map[string]any)
	if !ok {
		return nil, 0, fmt.Errorf("XPC root is %T, want dictionary", value)
	}
	return dict, total, nil
}

func decodeXPCObject(r *bytes.Reader) (any, error) {
	var typ uint32
	if err := binary.Read(r, binary.LittleEndian, &typ); err != nil {
		return nil, err
	}
	switch typ {
	case 0x00001000:
		return nil, nil
	case 0x00002000:
		var v uint32
		_ = binary.Read(r, binary.LittleEndian, &v)
		return v != 0, nil
	case 0x00003000:
		var v int64
		_ = binary.Read(r, binary.LittleEndian, &v)
		return v, nil
	case 0x00004000:
		var v uint64
		_ = binary.Read(r, binary.LittleEndian, &v)
		return v, nil
	case 0x00005000:
		var v float64
		_ = binary.Read(r, binary.LittleEndian, &v)
		return v, nil
	case 0x00007000:
		var v uint64
		_ = binary.Read(r, binary.LittleEndian, &v)
		return v, nil
	case 0x00009000:
		return readXPCString(r)
	case 0x0000A000:
		raw := make([]byte, 16)
		if _, err := io.ReadFull(r, raw); err != nil {
			return nil, err
		}
		return formatUUID(raw), nil
	case 0x0000B000:
		var v uint32
		_ = binary.Read(r, binary.LittleEndian, &v)
		return int64(v), nil
	case 0x0000C000:
		var length uint32
		var identifier uint32
		_ = binary.Read(r, binary.LittleEndian, &length)
		_ = binary.Read(r, binary.LittleEndian, &identifier)
		return map[string]any{"length": int64(length), "id": int64(identifier)}, nil
	case 0x0000F000:
		return readXPCDictionary(r)
	case 0x0000E000:
		return readXPCArray(r)
	case 0x00008000:
		return readXPCData(r)
	default:
		return nil, fmt.Errorf("unsupported XPC type 0x%x", typ)
	}
}

func readXPCString(r *bytes.Reader) (string, error) {
	var length uint32
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return "", err
	}
	raw := make([]byte, length)
	if _, err := io.ReadFull(r, raw); err != nil {
		return "", err
	}
	alignReader(r)
	if len(raw) > 0 && raw[len(raw)-1] == 0 {
		raw = raw[:len(raw)-1]
	}
	return string(raw), nil
}

func readXPCData(r *bytes.Reader) ([]byte, error) {
	var length uint32
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return nil, err
	}
	raw := make([]byte, length)
	if _, err := io.ReadFull(r, raw); err != nil {
		return nil, err
	}
	alignReader(r)
	return raw, nil
}

func readXPCArray(r *bytes.Reader) ([]any, error) {
	var length uint32
	var count uint32
	_ = binary.Read(r, binary.LittleEndian, &length)
	_ = binary.Read(r, binary.LittleEndian, &count)
	items := make([]any, 0, count)
	for i := uint32(0); i < count; i++ {
		item, err := decodeXPCObject(r)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func readXPCDictionary(r *bytes.Reader) (map[string]any, error) {
	var length uint32
	var count uint32
	_ = binary.Read(r, binary.LittleEndian, &length)
	_ = binary.Read(r, binary.LittleEndian, &count)
	result := make(map[string]any, count)
	for i := uint32(0); i < count; i++ {
		key, err := readAlignedCString(r)
		if err != nil {
			return nil, err
		}
		value, err := decodeXPCObject(r)
		if err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, nil
}

func readAlignedCString(r *bytes.Reader) (string, error) {
	var raw []byte
	for {
		b, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if b == 0 {
			break
		}
		raw = append(raw, b)
	}
	alignReader(r)
	return string(raw), nil
}

func alignReader(r *bytes.Reader) {
	read := r.Size() - int64(r.Len())
	for read%4 != 0 {
		_, _ = r.ReadByte()
		read++
	}
}

func intValue(value any) (int, bool) {
	if value == nil {
		return 0, false
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(reflected.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return int(reflected.Uint()), true
	}
	switch v := value.(type) {
	case float64:
		return int(v), true
	case string:
		parsed, err := strconv.Atoi(v)
		if err == nil {
			return parsed, true
		}
		return 0, false
	default:
		return 0, false
	}
}

func hexPrefix(data []byte, limit int) string {
	if len(data) < limit {
		limit = len(data)
	}
	return hex.EncodeToString(data[:limit])
}

func formatUUID(raw []byte) string {
	if len(raw) != 16 {
		return hex.EncodeToString(raw)
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}
