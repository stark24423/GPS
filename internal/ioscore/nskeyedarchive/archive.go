package nskeyedarchive

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"unicode/utf16"
)

type uid uint64

// Archive builds the small NSKeyedArchiver subset used by DTX calls.
// 目前只支援 DVT LocationSimulation 需要的型別：string、int、float64、map。
func Archive(value any) ([]byte, error) {
	encoder := &archiver{objects: []any{"$null"}}
	root, err := encoder.archiveObject(value)
	if err != nil {
		return nil, err
	}
	top := map[string]any{"root": root}
	return MarshalBinary(map[string]any{
		"$archiver": "NSKeyedArchiver",
		"$objects":  encoder.objects,
		"$top":      top,
		"$version":  int64(100000),
	})
}

type archiver struct {
	objects []any
}

func (a *archiver) appendObject(value any) uid {
	a.objects = append(a.objects, value)
	return uid(len(a.objects) - 1)
}

func (a *archiver) archiveObject(value any) (uid, error) {
	switch v := value.(type) {
	case string, float64:
		return a.appendObject(v), nil
	case int:
		return a.appendObject(int64(v)), nil
	case int64:
		return a.appendObject(v), nil
	case map[string]int:
		converted := make(map[string]any, len(v))
		for key, val := range v {
			converted[key] = int64(val)
		}
		return a.archiveObject(converted)
	case map[string]any:
		index := a.appendObject(nil)
		class := a.appendObject(map[string]any{
			"$classes":   []any{"NSDictionary"},
			"$classname": "NSDictionary",
		})
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		keyUIDs := make([]any, 0, len(keys))
		valueUIDs := make([]any, 0, len(keys))
		for _, key := range keys {
			keyUID := a.appendObject(key)
			valueUID, err := a.archiveObject(v[key])
			if err != nil {
				return 0, err
			}
			keyUIDs = append(keyUIDs, keyUID)
			valueUIDs = append(valueUIDs, valueUID)
		}
		a.objects[index] = map[string]any{
			"$class":     class,
			"NS.keys":    keyUIDs,
			"NS.objects": valueUIDs,
		}
		return index, nil
	default:
		return 0, fmt.Errorf("unsupported NSKeyedArchive value %T", value)
	}
}

func MarshalBinary(root any) ([]byte, error) {
	writer := &binaryPlistWriter{}
	rootID, err := writer.add(root)
	if err != nil {
		return nil, err
	}
	var table bytes.Buffer
	offsets := make([]uint64, len(writer.objects))
	for i, object := range writer.objects {
		offsets[i] = uint64(8 + table.Len())
		if err := writer.writeObject(&table, object); err != nil {
			return nil, err
		}
	}
	offsetSize := byteSize(uint64(8 + table.Len()))
	refSize := byteSize(uint64(len(writer.objects) - 1))
	offsetTableOffset := uint64(8 + table.Len())

	var out bytes.Buffer
	out.WriteString("bplist00")
	out.Write(table.Bytes())
	for _, offset := range offsets {
		writeSizedInt(&out, offset, offsetSize)
	}
	trailer := make([]byte, 32)
	trailer[6] = byte(offsetSize)
	trailer[7] = byte(refSize)
	binary.BigEndian.PutUint64(trailer[8:16], uint64(len(writer.objects)))
	binary.BigEndian.PutUint64(trailer[16:24], uint64(rootID))
	binary.BigEndian.PutUint64(trailer[24:32], offsetTableOffset)
	out.Write(trailer)
	return out.Bytes(), nil
}

type binaryPlistWriter struct {
	objects []any
}

func (w *binaryPlistWriter) add(value any) (int, error) {
	switch v := value.(type) {
	case nil, bool, string, int, int64, float64, uid:
		w.objects = append(w.objects, v)
		return len(w.objects) - 1, nil
	case []any:
		refs := make([]int, 0, len(v))
		id := len(w.objects)
		w.objects = append(w.objects, &refs)
		for _, item := range v {
			ref, err := w.add(item)
			if err != nil {
				return 0, err
			}
			refs = append(refs, ref)
		}
		w.objects[id] = refs
		return id, nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		id := len(w.objects)
		dict := plistDict{}
		w.objects = append(w.objects, dict)
		for _, key := range keys {
			keyRef, err := w.add(key)
			if err != nil {
				return 0, err
			}
			valueRef, err := w.add(v[key])
			if err != nil {
				return 0, err
			}
			dict.keys = append(dict.keys, keyRef)
			dict.values = append(dict.values, valueRef)
		}
		w.objects[id] = dict
		return id, nil
	default:
		return 0, fmt.Errorf("unsupported binary plist value %T", value)
	}
}

type plistDict struct {
	keys   []int
	values []int
}

func (w *binaryPlistWriter) writeObject(out *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case nil:
		out.WriteByte(0x00)
	case bool:
		if v {
			out.WriteByte(0x09)
		} else {
			out.WriteByte(0x08)
		}
	case string:
		if isASCII(v) {
			writeCount(out, 0x50, len(v))
			out.WriteString(v)
			return nil
		}
		encoded := utf16.Encode([]rune(v))
		writeCount(out, 0x60, len(encoded))
		for _, ch := range encoded {
			_ = binary.Write(out, binary.BigEndian, ch)
		}
	case int:
		writeIntObject(out, int64(v))
	case int64:
		writeIntObject(out, v)
	case float64:
		out.WriteByte(0x23)
		_ = binary.Write(out, binary.BigEndian, math.Float64bits(v))
	case uid:
		if v <= math.MaxUint8 {
			out.WriteByte(0x80)
			out.WriteByte(byte(v))
		} else {
			out.WriteByte(0x83)
			_ = binary.Write(out, binary.BigEndian, uint32(v))
		}
	case []int:
		writeCount(out, 0xA0, len(v))
		for _, ref := range v {
			writeSizedInt(out, uint64(ref), w.refSize())
		}
	case plistDict:
		writeCount(out, 0xD0, len(v.keys))
		for _, ref := range v.keys {
			writeSizedInt(out, uint64(ref), w.refSize())
		}
		for _, ref := range v.values {
			writeSizedInt(out, uint64(ref), w.refSize())
		}
	default:
		return fmt.Errorf("unsupported binary plist object %T", value)
	}
	return nil
}

func (w *binaryPlistWriter) refSize() int {
	return byteSize(uint64(len(w.objects) - 1))
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7f {
			return false
		}
	}
	return true
}

func writeIntObject(out *bytes.Buffer, value int64) {
	out.WriteByte(0x13)
	_ = binary.Write(out, binary.BigEndian, value)
}

func writeCount(out *bytes.Buffer, marker byte, count int) {
	if count < 15 {
		out.WriteByte(marker | byte(count))
		return
	}
	out.WriteByte(marker | 0x0f)
	writeIntObject(out, int64(count))
}

func byteSize(value uint64) int {
	switch {
	case value <= math.MaxUint8:
		return 1
	case value <= math.MaxUint16:
		return 2
	case value <= math.MaxUint32:
		return 4
	default:
		return 8
	}
}

func writeSizedInt(out *bytes.Buffer, value uint64, size int) {
	for i := size - 1; i >= 0; i-- {
		out.WriteByte(byte(value >> (uint(i) * 8)))
	}
}
