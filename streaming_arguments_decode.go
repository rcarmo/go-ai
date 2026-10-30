package goai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/rcarmo/go-ai/internal/jsonparse"
)

type streamingRawField struct {
	name  string
	value json.RawMessage
}

func streamingRawFields(raw []byte) ([]streamingRawField, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('{') {
		return nil, fmt.Errorf("expected JSON object")
	}
	var fields []streamingRawField
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return nil, err
		}
		var value json.RawMessage
		if err = d.Decode(&value); err != nil {
			return nil, err
		}
		fields = append(fields, streamingRawField{token.(string), value})
	}
	return fields, nil
}
func streamingRawLookup(fields []streamingRawField, name string) json.RawMessage {
	var result json.RawMessage
	for _, f := range fields {
		if strings.EqualFold(f.name, name) {
			result = f.value
		}
	}
	return result
}

// RestoreStreamingArgumentsJSON restores UTF-16 arguments/diagnostics on already
// decoded message DTOs. Callers retain responsibility for schema/resource checks.
// preserveNumbers retains decimal spellings, including integers above 2^53.
// The common non-surrogate durable path only marks typed tool-call fields.
func RestoreStreamingArgumentsJSON(data []byte, target any, preserveNumbers bool) error {
	lossless := !preserveNumbers || bytes.Contains(data, []byte(`\ud`)) || bytes.Contains(data, []byte(`\uD`))
	type visit struct {
		typ reflect.Type
		ptr uintptr
	}
	seen := map[visit]bool{}
	var walk func(reflect.Value, []byte) error
	restore := func(args *map[string]any, diagnostic *string, mode *bool, raw []byte) error {
		if lossless && len(raw) > 0 {
			fields, err := streamingRawFields(raw)
			if err != nil {
				return err
			}
			if value := streamingRawLookup(fields, "arguments"); len(value) > 0 {
				parsed, err := jsonparse.ParseJSONValue(string(value), preserveNumbers)
				if err != nil {
					return err
				}
				if parsed == nil {
					*args = nil
				} else {
					object, ok := parsed.(map[string]any)
					if !ok {
						return fmt.Errorf("tool call arguments must be an object")
					}
					*args = object
				}
			}
			if value := streamingRawLookup(fields, "argumentsError"); len(value) > 0 && !bytes.Equal(value, []byte("null")) {
				parsed, err := jsonparse.ParseJSONValue(string(value), preserveNumbers)
				if err != nil {
					return err
				}
				switch v := parsed.(type) {
				case string:
					*diagnostic = v
				case UTF16String:
					*diagnostic = v.WTF8()
				default:
					return fmt.Errorf("tool argument diagnostic must be a string")
				}
			}
		}
		*mode = *args != nil || *diagnostic != ""
		return nil
	}
	walk = func(v reflect.Value, raw []byte) error {
		if !v.IsValid() {
			return nil
		}
		if v.Kind() == reflect.Interface {
			if v.IsNil() {
				return nil
			}
			return walk(v.Elem(), raw)
		}
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return nil
			}
			key := visit{v.Type(), v.Pointer()}
			if seen[key] {
				return nil
			}
			seen[key] = true
			return walk(v.Elem(), raw)
		}
		if v.CanAddr() && v.CanInterface() {
			switch v.Type() {
			case reflect.TypeFor[ToolCall]():
				t := v.Addr().Interface().(*ToolCall)
				return restore(&t.Arguments, &t.ArgumentsError, &t.streamingArguments, raw)
			case reflect.TypeFor[ContentBlock]():
				b := v.Addr().Interface().(*ContentBlock)
				if b.Type == "text" && (b.HasToolArgumentDiagnostic() || b.HasToolArgumentTranscript()) {
					if !lossless {
						return nil
					}
					fields, err := streamingRawFields(raw)
					if err != nil {
						return err
					}
					encoded := streamingRawLookup(fields, "text")
					if len(encoded) == 0 {
						return nil
					}
					value, err := jsonparse.ParseJSONValue(string(encoded), preserveNumbers)
					if err != nil {
						return err
					}
					switch text := value.(type) {
					case string:
						b.Text = text
					case UTF16String:
						b.Text = text.WTF8()
					default:
						return fmt.Errorf("diagnostic text must be string")
					}
					return nil
				}
				if b.Type == "toolCall" {
					return restore(&b.Arguments, &b.ArgumentsError, &b.streamingArguments, raw)
				}
				return nil
			}
		}
		switch v.Kind() {
		case reflect.Struct:
			var fields []streamingRawField
			if lossless && len(raw) > 0 {
				var err error
				fields, err = streamingRawFields(raw)
				if err != nil {
					return err
				}
			}
			for i := 0; i < v.NumField(); i++ {
				sf := v.Type().Field(i)
				if sf.PkgPath != "" {
					continue
				}
				tag := strings.Split(sf.Tag.Get("json"), ",")[0]
				if tag == "-" {
					continue
				}
				child := raw
				if !sf.Anonymous || tag != "" {
					if tag == "" {
						tag = sf.Name
					}
					child = streamingRawLookup(fields, tag)
				}
				if err := walk(v.Field(i), child); err != nil {
					return err
				}
			}
		case reflect.Slice, reflect.Array:
			if v.Type().Elem().Kind() == reflect.Uint8 {
				return nil
			}
			var items []json.RawMessage
			if lossless && len(raw) > 0 {
				if err := json.Unmarshal(raw, &items); err != nil {
					return err
				}
			}
			for i := 0; i < v.Len(); i++ {
				var child []byte
				if i < len(items) {
					child = items[i]
				}
				if err := walk(v.Index(i), child); err != nil {
					return err
				}
			}
		case reflect.Map:
			// Opaque decoded JSON maps contain no typed message DTOs.
			if v.Type().Elem().Kind() == reflect.Interface {
				return nil
			}
			if v.IsNil() {
				return nil
			}
			key := visit{v.Type(), v.Pointer()}
			if seen[key] {
				return nil
			}
			seen[key] = true
			var fields []streamingRawField
			if lossless && len(raw) > 0 {
				var err error
				fields, err = streamingRawFields(raw)
				if err != nil {
					return err
				}
			}
			it := v.MapRange()
			for it.Next() {
				value := reflect.New(v.Type().Elem()).Elem()
				value.Set(it.Value())
				var child []byte
				if it.Key().Kind() == reflect.String {
					child = streamingRawLookup(fields, it.Key().String())
				}
				if err := walk(value, child); err != nil {
					return err
				}
				v.SetMapIndex(it.Key(), value)
			}
		}
		return nil
	}
	return walk(reflect.ValueOf(target), data)
}

// UnmarshalStreamingMessage is an explicit lossless SDK boundary. Ordinary
// encoding/json decoding keeps its existing Go behaviour. Arguments use JS
// numbers; durable DTO callers use RestoreStreamingArgumentsJSON with true.
func UnmarshalStreamingMessage(data []byte) (Message, error) {
	if !utf8.Valid(data) {
		return Message{}, fmt.Errorf("invalid streaming JSON UTF-8")
	}
	var message Message
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&message); err != nil {
		return Message{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Message{}, fmt.Errorf("trailing streaming JSON data")
	}
	if err := RestoreStreamingArgumentsJSON(data, &message, false); err != nil {
		return Message{}, err
	}
	return message, nil
}
