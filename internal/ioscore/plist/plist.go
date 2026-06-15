package plist

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

type Value any

type Dict map[string]Value

func Marshal(dict Dict) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(xml.Header)
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">`)
	if err := writeValue(&b, dict); err != nil {
		return nil, err
	}
	b.WriteString(`</plist>`)
	return b.Bytes(), nil
}

func Unmarshal(data []byte) (Dict, error) {
	if bytes.HasPrefix(data, []byte("bplist00")) {
		value, err := parseBinary(data)
		if err != nil {
			return nil, err
		}
		dict, ok := value.(Dict)
		if !ok {
			return nil, fmt.Errorf("binary plist root is %T, want dict", value)
		}
		return dict, nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local == "dict" {
			value, err := readDict(decoder)
			if err != nil {
				return nil, err
			}
			return value, nil
		}
	}
	return nil, fmt.Errorf("plist dict not found")
}

type binaryParser struct {
	data          []byte
	offsetSize    int
	refSize       int
	numObjects    int
	topObject     int
	offsets       []int
	parsedObjects map[int]Value
}

func parseBinary(data []byte) (Value, error) {
	if len(data) < 40 {
		return nil, fmt.Errorf("binary plist too short")
	}
	trailer := data[len(data)-32:]
	parser := &binaryParser{
		data:          data,
		offsetSize:    int(trailer[6]),
		refSize:       int(trailer[7]),
		numObjects:    int(binary.BigEndian.Uint64(trailer[8:16])),
		topObject:     int(binary.BigEndian.Uint64(trailer[16:24])),
		parsedObjects: make(map[int]Value),
	}
	offsetTable := int(binary.BigEndian.Uint64(trailer[24:32]))
	if parser.offsetSize <= 0 || parser.refSize <= 0 || parser.numObjects <= 0 {
		return nil, fmt.Errorf("invalid binary plist trailer")
	}
	end := offsetTable + parser.numObjects*parser.offsetSize
	if offsetTable < 0 || end > len(data)-32 {
		return nil, fmt.Errorf("invalid binary plist offset table")
	}
	parser.offsets = make([]int, parser.numObjects)
	for i := 0; i < parser.numObjects; i++ {
		parser.offsets[i] = int(readSizedInt(data[offsetTable+i*parser.offsetSize:], parser.offsetSize))
	}
	return parser.parseObject(parser.topObject)
}

func (p *binaryParser) parseObject(index int) (Value, error) {
	if index < 0 || index >= len(p.offsets) {
		return nil, fmt.Errorf("binary plist object index out of range: %d", index)
	}
	if value, ok := p.parsedObjects[index]; ok {
		return value, nil
	}
	offset := p.offsets[index]
	if offset < 0 || offset >= len(p.data) {
		return nil, fmt.Errorf("binary plist object offset out of range: %d", offset)
	}
	marker := p.data[offset]
	kind := marker >> 4
	count := int(marker & 0x0f)
	cursor := offset + 1
	if count == 0x0f {
		extended, next, err := p.readExtendedCount(cursor)
		if err != nil {
			return nil, err
		}
		count = extended
		cursor = next
	}

	var value Value
	switch kind {
	case 0x0:
		switch marker {
		case 0x08:
			value = false
		case 0x09:
			value = true
		default:
			value = nil
		}
	case 0x1:
		size := 1 << count
		value = int64(readSizedInt(p.data[cursor:], size))
	case 0x4:
		value = append([]byte(nil), p.data[cursor:cursor+count]...)
	case 0x5:
		value = string(p.data[cursor : cursor+count])
	case 0x6:
		raw := p.data[cursor : cursor+count*2]
		chars := make([]uint16, count)
		for i := 0; i < count; i++ {
			chars[i] = binary.BigEndian.Uint16(raw[i*2:])
		}
		value = string(utf16.Decode(chars))
	case 0xa:
		items := make([]Value, count)
		for i := 0; i < count; i++ {
			ref := int(readSizedInt(p.data[cursor+i*p.refSize:], p.refSize))
			item, err := p.parseObject(ref)
			if err != nil {
				return nil, err
			}
			items[i] = item
		}
		value = items
	case 0xd:
		dict := Dict{}
		valueRefs := cursor + count*p.refSize
		for i := 0; i < count; i++ {
			keyRef := int(readSizedInt(p.data[cursor+i*p.refSize:], p.refSize))
			valRef := int(readSizedInt(p.data[valueRefs+i*p.refSize:], p.refSize))
			keyValue, err := p.parseObject(keyRef)
			if err != nil {
				return nil, err
			}
			key, ok := keyValue.(string)
			if !ok {
				return nil, fmt.Errorf("binary plist dict key is %T, want string", keyValue)
			}
			val, err := p.parseObject(valRef)
			if err != nil {
				return nil, err
			}
			dict[key] = val
		}
		value = dict
	default:
		return nil, fmt.Errorf("unsupported binary plist object marker 0x%x", marker)
	}
	p.parsedObjects[index] = value
	return value, nil
}

func (p *binaryParser) readExtendedCount(cursor int) (int, int, error) {
	if cursor >= len(p.data) {
		return 0, 0, fmt.Errorf("missing binary plist extended count")
	}
	marker := p.data[cursor]
	if marker>>4 != 0x1 {
		return 0, 0, fmt.Errorf("binary plist extended count marker is 0x%x", marker)
	}
	size := int(math.Pow(2, float64(marker&0x0f)))
	return int(readSizedInt(p.data[cursor+1:], size)), cursor + 1 + size, nil
}

func readSizedInt(data []byte, size int) uint64 {
	var value uint64
	for i := 0; i < size && i < len(data); i++ {
		value = value<<8 | uint64(data[i])
	}
	return value
}

func writeValue(b *bytes.Buffer, value Value) error {
	switch v := value.(type) {
	case Dict:
		b.WriteString("<dict>")
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			b.WriteString("<key>")
			xml.EscapeText(b, []byte(key))
			b.WriteString("</key>")
			if err := writeValue(b, v[key]); err != nil {
				return err
			}
		}
		b.WriteString("</dict>")
	case map[string]Value:
		return writeValue(b, Dict(v))
	case string:
		b.WriteString("<string>")
		xml.EscapeText(b, []byte(v))
		b.WriteString("</string>")
	case int:
		fmt.Fprintf(b, "<integer>%d</integer>", v)
	case uint32:
		fmt.Fprintf(b, "<integer>%d</integer>", v)
	case uint64:
		fmt.Fprintf(b, "<integer>%d</integer>", v)
	case float64:
		fmt.Fprintf(b, "<real>%g</real>", v)
	case bool:
		if v {
			b.WriteString("<true/>")
		} else {
			b.WriteString("<false/>")
		}
	case []Value:
		b.WriteString("<array>")
		for _, item := range v {
			if err := writeValue(b, item); err != nil {
				return err
			}
		}
		b.WriteString("</array>")
	default:
		return fmt.Errorf("unsupported plist value %T", value)
	}
	return nil
}

func readDict(decoder *xml.Decoder) (Dict, error) {
	dict := Dict{}
	var key string
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			if t.Name.Local == "key" {
				var text string
				if err := decoder.DecodeElement(&text, &t); err != nil {
					return nil, err
				}
				key = text
				continue
			}
			if key == "" {
				return nil, fmt.Errorf("plist value without key")
			}
			value, err := readValue(decoder, t)
			if err != nil {
				return nil, err
			}
			dict[key] = value
			key = ""
		case xml.EndElement:
			if t.Name.Local == "dict" {
				return dict, nil
			}
		}
	}
}

func readValue(decoder *xml.Decoder, start xml.StartElement) (Value, error) {
	switch start.Name.Local {
	case "dict":
		return readDict(decoder)
	case "string", "data", "date":
		var text string
		if err := decoder.DecodeElement(&text, &start); err != nil {
			return nil, err
		}
		return text, nil
	case "integer":
		var text string
		if err := decoder.DecodeElement(&text, &start); err != nil {
			return nil, err
		}
		value, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil {
			return nil, err
		}
		return value, nil
	case "real":
		var text string
		if err := decoder.DecodeElement(&text, &start); err != nil {
			return nil, err
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil {
			return nil, err
		}
		return value, nil
	case "true":
		return true, decoder.Skip()
	case "false":
		return false, decoder.Skip()
	case "array":
		var values []Value
		for {
			token, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			switch t := token.(type) {
			case xml.StartElement:
				value, err := readValue(decoder, t)
				if err != nil {
					return nil, err
				}
				values = append(values, value)
			case xml.EndElement:
				if t.Name.Local == "array" {
					return values, nil
				}
			}
		}
	default:
		return nil, fmt.Errorf("unsupported plist element %s", start.Name.Local)
	}
}

func String(dict Dict, key string) string {
	value, _ := dict[key].(string)
	return value
}

func Int(dict Dict, key string) int {
	switch value := dict[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case uint64:
		return int(value)
	case uint32:
		return int(value)
	}
	return 0
}

func Data(dict Dict, key string) []byte {
	value, _ := dict[key].([]byte)
	return value
}
