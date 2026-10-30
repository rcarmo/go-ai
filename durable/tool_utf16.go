package durable

import (
	"io"
	"reflect"
	"regexp"
	"unicode/utf16"
	"unicode/utf8"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

var nativeToolCheckpointType = reflect.TypeFor[toolCheckpoint]()

type checkpointArgumentWire struct {
	arguments  JSON
	diagnostic string
	chunks     []string
}

func copyScopedToolArguments(args JSON, native bool, l Limits) (JSON, error) {
	if !native {
		return copyObject(args, l)
	}
	block := goai.ContentBlock{Type: "toolCall"}
	block.SetStreamingArguments(map[string]any(args), "")
	return copyToolArguments(block, l)
}
func (cp toolCheckpoint) argumentCall(args JSON) goai.ToolCall {
	call := goai.ToolCall{Type: "toolCall", ID: cp.CallID, Name: cp.Offer.Name, Arguments: map[string]any(args), ArgumentsError: cp.ArgumentsError}
	if cp.StreamingArguments {
		call.SetStreamingArguments(map[string]any(args), cp.ArgumentsError)
	}
	return call
}

func (b *limitedJSON) checkpointJSONField(v reflect.Value, field jsonStructField) (reflect.Value, error) {
	if v.Type() != nativeToolCheckpointType || field.name != "arguments" && field.name != "argumentsError" && field.name != "utf16Arguments" {
		return b.receiptJSONField(v, field)
	}
	cp := v.Interface().(toolCheckpoint)
	if !cp.StreamingArguments {
		if field.name == "utf16Arguments" {
			return reflect.Zero(v.Field(field.index).Type()), nil
		}
		return nativeJSONField(v, field)
	}
	b.projectedReceipt = true // also validate the physical ownership flag/node budget
	if b.checkpointViews == nil {
		b.checkpointViews = map[reflect.Value]*checkpointArgumentWire{}
	}
	view := b.checkpointViews[v]
	if view == nil {
		view = &checkpointArgumentWire{arguments: cp.Arguments, diagnostic: cp.ArgumentsError}
		if containsUTF16(reflect.ValueOf(cp.Arguments)) || !utf8.ValidString(cp.ArgumentsError) {
			var err error
			view.chunks, err = encodeArgumentCarrier(cp.Arguments, cp.ArgumentsError, b.l, b.max-len(b.data))
			if err != nil {
				return reflect.Value{}, err
			}
			view.arguments = JSON{}
			view.diagnostic = ""
		}
		b.checkpointViews[v] = view
	}
	switch field.name {
	case "arguments":
		return reflect.ValueOf(view.arguments), nil
	case "argumentsError":
		return reflect.ValueOf(view.diagnostic), nil
	default:
		return reflect.ValueOf(view.chunks), nil
	}
}
func restoreCheckpointUTF16(cp *toolCheckpoint, l Limits) error {
	if len(cp.UTF16Arguments) == 0 {
		return nil
	}
	if !cp.StreamingArguments || cp.Arguments == nil || len(cp.Arguments) != 0 || cp.ArgumentsError != "" {
		return reject("inconsistent tool argument placeholder")
	}
	carrier, err := decodeArgumentCarrier(cp.UTF16Arguments, l)
	if err != nil {
		return err
	}
	cp.Arguments = JSON(carrier.Arguments)
	cp.ArgumentsError = carrier.ArgumentsError
	return nil
}

// RuneReader preserves isolated units as distinct rune values. Replacing them
// with U+FFFD would make literal replacement-character patterns match falsely.
// Valid surrogate pairs count and match as one Unicode code point.
type schemaUTF16Reader struct {
	units []uint16
	at    int
}

func (r *schemaUTF16Reader) ReadRune() (rune, int, error) {
	if r.at == len(r.units) {
		return 0, 0, io.EOF
	}
	unit := r.units[r.at]
	r.at++
	if unit >= 0xd800 && unit <= 0xdbff && r.at < len(r.units) {
		low := r.units[r.at]
		if low >= 0xdc00 && low <= 0xdfff {
			r.at++
			return utf16.DecodeRune(rune(unit), rune(low)), 4, nil
		}
	}
	size := utf8.RuneLen(rune(unit))
	if size < 0 {
		size = 3
	}
	return rune(unit), size, nil
}
func schemaStringLength(value any) int {
	if text, ok := value.(string); ok {
		return utf8.RuneCountInString(text)
	}
	reader := schemaUTF16Reader{units: value.(goai.UTF16String).CodeUnits()}
	n := 0
	for {
		_, _, err := reader.ReadRune()
		if err != nil {
			return n
		}
		n++
	}
}
func schemaStringMatches(pattern string, value any) bool {
	if text, ok := value.(string); ok {
		matched, _ := regexp.MatchString(pattern, text)
		return matched
	}
	reader := schemaUTF16Reader{units: value.(goai.UTF16String).CodeUnits()}
	matched, _ := regexp.MatchReader(pattern, &reader)
	return matched
}
func equalUTF16Values(a, b any) (bool, bool) {
	_, left := a.(goai.UTF16String)
	_, right := b.(goai.UTF16String)
	if !left && !right {
		return false, false
	}
	units := func(v any) ([]uint16, bool) {
		switch x := v.(type) {
		case goai.UTF16String:
			return x.CodeUnits(), true
		case string:
			u, err := jsonparse.StringCodeUnits(x)
			return u, err == nil
		}
		return nil, false
	}
	x, ok := units(a)
	if !ok {
		return false, true
	}
	y, ok := units(b)
	if !ok || len(x) != len(y) {
		return false, true
	}
	for i := range x {
		if x[i] != y[i] {
			return false, true
		}
	}
	return true, true
}
