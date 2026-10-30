package durable

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

type utf16JSONRange struct{ start, end int }
type rangeRawField struct {
	name       string
	raw        json.RawMessage
	start, end int
}

func rangeObject(raw []byte, base int) ([]rangeRawField, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('{') {
		return nil, nil
	}
	var fields []rangeRawField
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return nil, err
		}
		var value json.RawMessage
		if err = d.Decode(&value); err != nil {
			return nil, err
		}
		end := base + int(d.InputOffset())
		fields = append(fields, rangeRawField{key.(string), value, end - len(value), end})
	}
	return fields, nil
}

// Only typed tool-call arguments/diagnostics opt into UTF-16 string semantics.
// Ordinary JSON, identifiers, content text and unknown schema fields stay strict.
func streamingArgumentRanges(raw []byte, target reflect.Type, l Limits) ([]utf16JSONRange, error) {
	if target == nil {
		return nil, reject("invalid JSON target")
	}
	var result []utf16JSONRange
	type schemaVisit struct {
		typ          reflect.Type
		base, length int
	}
	seen := map[schemaVisit]bool{}
	var walk func([]byte, int, reflect.Type, int) error
	walk = func(raw []byte, base int, t reflect.Type, depth int) error {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil
		}
		if depth > l.MaxDepth {
			return reject("JSON depth limit")
		}
		key := schemaVisit{t, base, len(raw)}
		if seen[key] {
			return nil
		}
		seen[key] = true
		switch t.Kind() {
		case reflect.Struct:
			fields, err := rangeObject(raw, base)
			if err != nil {
				return err
			}
			native := t == nativeToolCallType
			if t == nativeContentBlockType {
				var kind string
				for _, f := range fields {
					if strings.EqualFold(f.name, "type") {
						if err = json.Unmarshal(f.raw, &kind); err != nil {
							return err
						}
					}
				}
				native = kind == "toolCall"
			}
			if native {
				for _, f := range fields {
					if strings.EqualFold(f.name, "arguments") || strings.EqualFold(f.name, "argumentsError") {
						result = append(result, utf16JSONRange{f.start, f.end})
					}
				}
				return nil
			}
			for i := 0; i < t.NumField(); i++ {
				sf := t.Field(i)
				if sf.PkgPath != "" {
					continue
				}
				tag := strings.Split(sf.Tag.Get("json"), ",")[0]
				if tag == "-" {
					continue
				}
				if sf.Anonymous && tag == "" {
					if err = walk(raw, base, sf.Type, depth); err != nil {
						return err
					}
					continue
				}
				if tag == "" {
					tag = sf.Name
				}
				for _, f := range fields {
					if strings.EqualFold(f.name, tag) {
						if err = walk(f.raw, f.start, sf.Type, depth+1); err != nil {
							return err
						}
					}
				}
			}
		case reflect.Slice, reflect.Array:
			if t.Elem().Kind() == reflect.Uint8 {
				return nil
			}
			d := json.NewDecoder(bytes.NewReader(raw))
			token, err := d.Token()
			if err != nil {
				return err
			}
			if token != json.Delim('[') {
				return nil
			}
			for d.More() {
				var value json.RawMessage
				if err = d.Decode(&value); err != nil {
					return err
				}
				end := base + int(d.InputOffset())
				if err = walk(value, end-len(value), t.Elem(), depth+1); err != nil {
					return err
				}
			}
		case reflect.Map:
			if t.Elem().Kind() == reflect.Interface {
				return nil
			}
			fields, err := rangeObject(raw, base)
			if err != nil {
				return err
			}
			for _, f := range fields {
				if err = walk(f.raw, f.start, t.Elem(), depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(raw, 0, target, 1); err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool { return result[i].start < result[j].start })
	return result, nil
}
func (s *jsonScanner) allowsUTF16(pos int) bool {
	for s.utf16Index < len(s.utf16Ranges) && s.utf16Ranges[s.utf16Index].end <= pos {
		s.utf16Index++
	}
	return s.utf16Index < len(s.utf16Ranges) && s.utf16Ranges[s.utf16Index].start <= pos && pos < s.utf16Ranges[s.utf16Index].end
}

// Enforce syntax and all resource limits before allocating schema projections.
// This preflight never authorises a string: decodeStrict subsequently scans
// again with only the ranges selected by the target schema.
func preflightStreamingLimits(raw []byte, l Limits) error {
	s := jsonScanner{data: raw, l: l, utf16Ranges: []utf16JSONRange{{0, len(raw)}}}
	s.space()
	if err := s.value(1); err != nil {
		return err
	}
	s.space()
	if s.pos != len(raw) {
		return reject("trailing JSON")
	}
	return nil
}
