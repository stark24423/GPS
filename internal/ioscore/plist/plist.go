package plist

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
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
