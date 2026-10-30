package jsonparse

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// UTF16String represents a JS string containing isolated UTF-16 code units.
// Ordinary Unicode strings remain Go strings. Prefix storage is immutable and
// shared; the optional trailing high surrogate avoids copying growing strings.
// This preview representation must not bypass ordinary persisted JSON checks.
type UTF16String struct {
	prefix string
	high   uint16
}

func (s UTF16String) CodeUnits() []uint16 {
	units, _ := StringCodeUnits(s.prefix)
	if s.high != 0 {
		units = append(units, s.high)
	}
	return units
}
func (s UTF16String) MarshalJSON() ([]byte, error) {
	var out bytes.Buffer
	err := writePreviewString(&out, s.prefix, s.high)
	return out.Bytes(), err
}

// StringCodeUnits decodes UTF-8 plus isolated surrogate encodings (WTF-8).
// The latter occur only in native preview keys; invalid byte sequences fail.
func StringCodeUnits(text string) ([]uint16, error) {
	result := make([]uint16, 0, len(text))
	for len(text) > 0 {
		r, n, ok := previewRune(text)
		if !ok {
			return nil, fmt.Errorf("invalid preview string encoding")
		}
		text = text[n:]
		if r > 0xffff {
			a, b := utf16.EncodeRune(r)
			result = append(result, uint16(a), uint16(b))
		} else {
			result = append(result, uint16(r))
		}
	}
	return result, nil
}
func previewRune(text string) (rune, int, bool) {
	if len(text) >= 3 && text[0] == 0xed && text[1] >= 0xa0 && text[1] <= 0xbf && text[2] >= 0x80 && text[2] <= 0xbf {
		return rune(text[0]&15)<<12 | rune(text[1]&63)<<6 | rune(text[2]&63), 3, true
	}
	r, n := utf8.DecodeRuneInString(text)
	return r, n, !(r == utf8.RuneError && n == 1)
}
func surrogateBytes(u uint16) string {
	return string([]byte{0xe0 | byte(u>>12), 0x80 | byte((u>>6)&63), 0x80 | byte(u&63)})
}

type utf16Buffer struct {
	text     strings.Builder
	high     uint16
	unpaired bool
}

func (b *utf16Buffer) Reset() { b.text.Reset(); b.high = 0; b.unpaired = false }
func (b *utf16Buffer) flushHigh() {
	if b.high != 0 {
		b.text.WriteString(surrogateBytes(b.high))
		b.high = 0
		b.unpaired = true
	}
}
func (b *utf16Buffer) WriteUnit(u uint16) {
	if b.high != 0 && u >= 0xdc00 && u <= 0xdfff {
		high := b.high
		b.high = 0
		b.text.WriteRune(utf16.DecodeRune(rune(high), rune(u)))
		return
	}
	b.flushHigh()
	if u >= 0xd800 && u <= 0xdbff {
		b.high = u
		return
	}
	if u >= 0xdc00 && u <= 0xdfff {
		b.text.WriteString(surrogateBytes(u))
		b.unpaired = true
		return
	}
	b.text.WriteRune(rune(u))
}
func (b *utf16Buffer) WriteText(text string) {
	if utf8.ValidString(text) {
		b.flushHigh()
		b.text.WriteString(text)
		return
	}
	for len(text) > 0 {
		r, n, ok := previewRune(text)
		if !ok {
			r = utf8.RuneError
		}
		text = text[n:]
		if r <= 0xffff {
			b.WriteUnit(uint16(r))
		} else {
			b.flushHigh()
			b.text.WriteRune(r)
		}
	}
}
func (b *utf16Buffer) Value() any {
	if b.unpaired || b.high != 0 {
		return UTF16String{prefix: b.text.String(), high: b.high}
	}
	return b.text.String()
}
func (b *utf16Buffer) Key() string { b.flushHigh(); return b.text.String() }

// AppendUTF16 accepts raw JS code units, including chunks split between a
// surrogate pair. Ordinary callers can use Append with Unicode Go strings.
func (p *StreamingJSONParser) AppendUTF16(units []uint16) []JSONChange {
	var text strings.Builder
	for _, u := range units {
		if u >= 0xd800 && u <= 0xdfff {
			text.WriteString(surrogateBytes(u))
		} else {
			text.WriteRune(rune(u))
		}
	}
	return p.Append(text.String())
}

// SnapshotJSON serialises a preview with JS JSON.stringify scalar semantics:
// isolated UTF-16 units are escaped and nonfinite numbers become null. This
// separate preview adapter does not relax the persisted JSON validators.
func (p *StreamingJSONParser) SnapshotJSON() ([]byte, bool, error) {
	value, present := p.Snapshot()
	if !present {
		return nil, false, nil
	}
	data, err := marshalPreview(value)
	return data, true, err
}
func marshalPreview(value any) ([]byte, error) {
	var out bytes.Buffer
	err := writePreview(&out, value, 0, map[previewVisit]bool{})
	return out.Bytes(), err
}

type previewVisit struct {
	typ    reflect.Type
	ptr    uintptr
	length int
}

func previewEmit(w io.Writer, text string) error {
	n, err := io.WriteString(w, text)
	if err == nil && n != len(text) {
		return io.ErrShortWrite
	}
	return err
}
func previewEscape(w io.Writer, u uint16) error {
	const hex = "0123456789abcdef"
	return previewEmit(w, string([]byte{'\\', 'u', hex[u>>12], hex[(u>>8)&15], hex[(u>>4)&15], hex[u&15]}))
}
func writePreviewString(w io.Writer, text string, high uint16) error {
	if err := previewEmit(w, "\""); err != nil {
		return err
	}
	for len(text) > 0 {
		run := 0
		for run < len(text) && text[run] >= 32 && text[run] < 128 && text[run] != '"' && text[run] != '\\' {
			run++
		}
		if run > 0 {
			if err := previewEmit(w, text[:run]); err != nil {
				return err
			}
			text = text[run:]
			continue
		}
		r, n, ok := previewRune(text)
		if !ok {
			return fmt.Errorf("invalid preview string encoding")
		}
		var err error
		switch {
		case r == '"':
			err = previewEmit(w, `\"`)
		case r == '\\':
			err = previewEmit(w, `\\`)
		case r < 32 || r >= 0xd800 && r <= 0xdfff:
			err = previewEscape(w, uint16(r))
		default:
			err = previewEmit(w, text[:n])
		}
		if err != nil {
			return err
		}
		text = text[n:]
	}
	if high != 0 {
		if err := previewEscape(w, high); err != nil {
			return err
		}
	}
	return previewEmit(w, "\"")
}
func writePreview(w io.Writer, value any, depth int, active map[previewVisit]bool) error {
	if depth > 10000 {
		return fmt.Errorf("preview JSON depth exceeded")
	}
	switch v := value.(type) {
	case nil:
		return previewEmit(w, "null")
	case bool:
		return previewEmit(w, strconv.FormatBool(v))
	case string:
		return writePreviewString(w, v, 0)
	case UTF16String:
		return writePreviewString(w, v.prefix, v.high)
	case float64:
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return previewEmit(w, "null")
		}
		if v == 0 {
			return previewEmit(w, "0")
		}
		return previewEmit(w, strconv.FormatFloat(v, 'g', -1, 64))
	case json.Number:
		if !streamNumber.MatchString(string(v)) {
			return fmt.Errorf("invalid preview JSON number")
		}
		return previewEmit(w, string(v))
	case int:
		return previewEmit(w, strconv.Itoa(v))
	case int8, int16, int32, int64:
		return previewEmit(w, strconv.FormatInt(reflect.ValueOf(v).Int(), 10))
	case uint, uint8, uint16, uint32, uint64:
		return previewEmit(w, strconv.FormatUint(reflect.ValueOf(v).Uint(), 10))
	case float32:
		n := float64(v)
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return previewEmit(w, "null")
		}
		if n == 0 {
			return previewEmit(w, "0")
		}
		return previewEmit(w, strconv.FormatFloat(n, 'g', -1, 32))
	case map[string]any:
		key := previewVisit{reflect.TypeOf(v), reflect.ValueOf(v).Pointer(), len(v)}
		if active[key] {
			return fmt.Errorf("cyclic preview JSON")
		}
		active[key] = true
		defer delete(active, key)
		if err := previewEmit(w, "{"); err != nil {
			return err
		}
		first := true
		for k, x := range v {
			if !first {
				if err := previewEmit(w, ","); err != nil {
					return err
				}
			}
			first = false
			if err := writePreviewString(w, k, 0); err != nil {
				return err
			}
			if err := previewEmit(w, ":"); err != nil {
				return err
			}
			if err := writePreview(w, x, depth+1, active); err != nil {
				return err
			}
		}
		return previewEmit(w, "}")
	case []any:
		key := previewVisit{reflect.TypeOf(v), reflect.ValueOf(v).Pointer(), len(v)}
		if active[key] {
			return fmt.Errorf("cyclic preview JSON")
		}
		active[key] = true
		defer delete(active, key)
		if err := previewEmit(w, "["); err != nil {
			return err
		}
		for i, x := range v {
			if i > 0 {
				if err := previewEmit(w, ","); err != nil {
					return err
				}
			}
			if err := writePreview(w, x, depth+1, active); err != nil {
				return err
			}
		}
		return previewEmit(w, "]")
	default:
		return fmt.Errorf("unsupported preview JSON type %T", value)
	}
}

// ArgumentsObject explicitly opts object keys into JS UTF-16 JSON semantics.
// Persistence validators must still check their own number/resource contracts.
type ArgumentsObject map[string]any

func (o ArgumentsObject) MarshalJSON() ([]byte, error) {
	if o == nil {
		return []byte("null"), nil
	}
	return marshalPreview(map[string]any(o))
}
func (s UTF16String) UTF8Size() int {
	n := len(s.prefix)
	if s.high != 0 {
		n += 3
	}
	return n
}
func (s UTF16String) WriteJSON(w io.Writer) error { return writePreviewString(w, s.prefix, s.high) }
func ValidateUTF16Key(text string) error {
	for len(text) > 0 {
		_, n, ok := previewRune(text)
		if !ok {
			return fmt.Errorf("invalid UTF-16 key encoding")
		}
		text = text[n:]
	}
	return nil
}
func WriteUTF16Key(w io.Writer, text string) error { return writePreviewString(w, text, 0) }
func UTF16Text(text string) (any, error) {
	if err := ValidateUTF16Key(text); err != nil {
		return nil, err
	}
	var b utf16Buffer
	b.WriteText(text)
	return b.Value(), nil
}

// WTF8 returns a lossless native string for a known UTF-16 text field.
func (s UTF16String) WTF8() string {
	if s.high == 0 {
		return s.prefix
	}
	return s.prefix + surrogateBytes(s.high)
}
