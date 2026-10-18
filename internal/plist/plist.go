// Package plist implements the small subset of Apple XML property lists
// needed to talk to usbmuxd (dict, array, string, integer, bool, data, real).
package plist

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Marshal encodes v (map[string]any, []any, string, ints, bool, []byte, float64)
// as an XML plist document.
func Marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n")
	if err := encode(&b, v); err != nil {
		return nil, err
	}
	b.WriteString("</plist>\n")
	return b.Bytes(), nil
}

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func encode(b *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case map[string]any:
		b.WriteString("<dict>")
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString("<key>" + esc(k) + "</key>")
			if err := encode(b, t[k]); err != nil {
				return err
			}
		}
		b.WriteString("</dict>")
	case []any:
		b.WriteString("<array>")
		for _, e := range t {
			if err := encode(b, e); err != nil {
				return err
			}
		}
		b.WriteString("</array>")
	case string:
		b.WriteString("<string>" + esc(t) + "</string>")
	case bool:
		if t {
			b.WriteString("<true/>")
		} else {
			b.WriteString("<false/>")
		}
	case int:
		b.WriteString("<integer>" + strconv.FormatInt(int64(t), 10) + "</integer>")
	case int64:
		b.WriteString("<integer>" + strconv.FormatInt(t, 10) + "</integer>")
	case uint16:
		b.WriteString("<integer>" + strconv.FormatUint(uint64(t), 10) + "</integer>")
	case uint32:
		b.WriteString("<integer>" + strconv.FormatUint(uint64(t), 10) + "</integer>")
	case uint64:
		b.WriteString("<integer>" + strconv.FormatUint(t, 10) + "</integer>")
	case float64:
		b.WriteString("<real>" + strconv.FormatFloat(t, 'g', -1, 64) + "</real>")
	case []byte:
		b.WriteString("<data>" + base64.StdEncoding.EncodeToString(t) + "</data>")
	default:
		return fmt.Errorf("plist: unsupported type %T", v)
	}
	return nil
}

// Unmarshal decodes an XML plist into generic Go values:
// map[string]any, []any, string, int64 (uint64 if too large), bool, []byte, float64.
func Unmarshal(data []byte) (any, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = false
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, fmt.Errorf("plist: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Local == "plist" {
				continue
			}
			return decodeValue(d, se)
		}
	}
}

func readText(d *xml.Decoder) (string, error) {
	var sb strings.Builder
	for {
		tok, err := d.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			sb.Write(t)
		case xml.EndElement:
			return sb.String(), nil
		}
	}
}

func nextStart(d *xml.Decoder) (*xml.StartElement, error) {
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			return &t, nil
		case xml.EndElement:
			return nil, nil
		}
	}
}

func decodeValue(d *xml.Decoder, se xml.StartElement) (any, error) {
	switch se.Name.Local {
	case "dict":
		m := map[string]any{}
		for {
			k, err := nextStart(d)
			if err != nil {
				return nil, err
			}
			if k == nil {
				return m, nil
			}
			if k.Name.Local != "key" {
				return nil, fmt.Errorf("plist: expected key, got %s", k.Name.Local)
			}
			key, err := readText(d)
			if err != nil {
				return nil, err
			}
			vs, err := nextStart(d)
			if err != nil {
				return nil, err
			}
			if vs == nil {
				return nil, io.ErrUnexpectedEOF
			}
			v, err := decodeValue(d, *vs)
			if err != nil {
				return nil, err
			}
			m[key] = v
		}
	case "array":
		var arr []any
		for {
			vs, err := nextStart(d)
			if err != nil {
				return nil, err
			}
			if vs == nil {
				return arr, nil
			}
			v, err := decodeValue(d, *vs)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
	case "string":
		return readText(d)
	case "integer":
		s, err := readText(d)
		if err != nil {
			return nil, err
		}
		s = strings.TrimSpace(s)
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return i, nil
		}
		u, err := strconv.ParseUint(s, 10, 64)
		return u, err
	case "real":
		s, err := readText(d)
		if err != nil {
			return nil, err
		}
		return strconv.ParseFloat(strings.TrimSpace(s), 64)
	case "true", "false":
		if _, err := readText(d); err != nil {
			return nil, err
		}
		return se.Name.Local == "true", nil
	case "data":
		s, err := readText(d)
		if err != nil {
			return nil, err
		}
		s = strings.Map(func(r rune) rune {
			if r == ' ' || r == '\n' || r == '\t' || r == '\r' {
				return -1
			}
			return r
		}, s)
		return base64.StdEncoding.DecodeString(s)
	case "date":
		return readText(d)
	}
	return nil, fmt.Errorf("plist: unsupported element %s", se.Name.Local)
}

// Int extracts an integer from a decoded plist value.
func Int(v any) (int64, bool) {
	switch t := v.(type) {
	case int64:
		return t, true
	case uint64:
		return int64(t), true
	case float64:
		return int64(t), true
	}
	return 0, false
}
