package durable

import (
	"bytes"
	"encoding"
	"encoding/json"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// limitedJSON refuses growth before allocating beyond the encoded byte budget.
type limitedJSON struct {
	data []byte
	max  int
}

func (b *limitedJSON) Write(p []byte) (int, error) {
	if len(p) > b.max-len(b.data) {
		return 0, reject("encoded JSON byte limit")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

type jsonStructField struct {
	index     int
	name      string
	omitEmpty bool
}

var jsonStructFields sync.Map // reflect.Type -> immutable field descriptions
func structJSONFields(t reflect.Type) []jsonStructField {
	if cached, ok := jsonStructFields.Load(t); ok {
		return cached.([]jsonStructField)
	}
	fields := make([]jsonStructField, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields = append(fields, jsonStructField{i, name, options == "omitempty"})
	}
	actual, _ := jsonStructFields.LoadOrStore(t, fields)
	return actual.([]jsonStructField)
}

type jsonBudget struct {
	l      Limits
	nodes  int
	active map[uintptr]bool
	// Allocation hint only, never an acceptance gate. Saturated during the
	// existing validation walk so huge/omitted values cannot reserve huge buffers.
	encodedHint int
}

func (b *jsonBudget) hint(size int) {
	const capHint = 64 << 10
	if size > capHint-b.encodedHint {
		b.encodedHint = capHint
	} else {
		b.encodedHint += size
	}
}

func (b *jsonBudget) node() error {
	b.nodes++
	if b.nodes > b.l.MaxNodes {
		return reject("JSON node limit")
	}
	return nil
}
func (b *jsonBudget) str(s string) error {
	b.hint(len(s) + 2)
	if !utf8.ValidString(s) || len(s) > b.l.MaxStringBytes {
		return reject("invalid or oversized JSON string")
	}
	return nil
}
func (b *jsonBudget) walk(v reflect.Value, depth int, objectsOnly bool) error {
	if err := b.node(); err != nil {
		return err
	}
	if !v.IsValid() {
		b.hint(4)
		return nil
	}
	for v.Kind() == reflect.Interface {
		if v.IsNil() {
			b.hint(4)
			return nil
		}
		v = v.Elem()
	}
	if v.Type() == reflect.TypeOf(json.Number("")) {
		n := string(v.Interface().(json.Number))
		b.hint(len(n))
		if len(n) > b.l.MaxStringBytes {
			return reject("JSON numeric spelling byte limit")
		}
		return validNumber(n)
	}
	if hasMarshalAuthority(v.Type()) {
		return reject("caller-defined JSON marshal authority")
	}
	switch v.Kind() {
	case reflect.Bool:
		b.hint(5)
		return nil
	case reflect.String:
		return b.str(v.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		b.hint(20)
		return nil
	case reflect.Float32, reflect.Float64:
		b.hint(24)
		if math.IsNaN(v.Float()) || math.IsInf(v.Float(), 0) {
			return reject("nonfinite JSON number")
		}
		return nil
	case reflect.Pointer:
		if objectsOnly {
			return reject("unsupported JSON value")
		}
		if v.IsNil() {
			return nil
		}
		ptr := v.Pointer()
		if b.active[ptr] {
			return reject("cyclic JSON value")
		}
		b.active[ptr] = true
		defer delete(b.active, ptr)
		return b.walk(v.Elem(), depth, objectsOnly)
	case reflect.Map:
		if v.IsNil() {
			b.hint(4)
			return nil
		}
		b.hint(2 + 2*v.Len())
		if v.Type().Key().Kind() != reflect.String {
			return reject("JSON map keys must be strings")
		}
		if depth > b.l.MaxDepth || v.Len() > b.l.MaxMembers {
			return reject("JSON container limit")
		}
		ptr := v.Pointer()
		if b.active[ptr] {
			return reject("cyclic JSON value")
		}
		b.active[ptr] = true
		defer delete(b.active, ptr)
		it := v.MapRange()
		key := reflect.New(v.Type().Key()).Elem()
		value := reflect.New(v.Type().Elem()).Elem()
		for it.Next() {
			key.SetIterKey(it)
			value.SetIterValue(it)
			if hasMarshalAuthority(key.Type()) {
				return reject("caller-defined map key marshal authority")
			}
			if err := b.node(); err != nil {
				return err
			}
			if err := b.str(key.String()); err != nil {
				return err
			}
			if err := b.walk(value, depth+1, objectsOnly); err != nil {
				return err
			}
		}
		return nil
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			b.hint(4)
			return nil
		}
		b.hint(2 + v.Len())
		if depth > b.l.MaxDepth || v.Len() > b.l.MaxMembers {
			return reject("JSON container limit")
		}
		if v.Kind() == reflect.Slice {
			ptr := v.Pointer()
			if ptr != 0 && b.active[ptr] {
				return reject("cyclic JSON value")
			}
			b.active[ptr] = true
			defer delete(b.active, ptr)
		}
		for i := 0; i < v.Len(); i++ {
			if err := b.walk(v.Index(i), depth+1, objectsOnly); err != nil {
				return err
			}
		}
		return nil
	case reflect.Struct:
		if objectsOnly {
			return reject("unsupported JSON object value")
		}
		if depth > b.l.MaxDepth || v.NumField() > b.l.MaxMembers {
			return reject("JSON container limit")
		}
		b.hint(2)
		for _, field := range structJSONFields(v.Type()) {
			b.hint(len(field.name) + 4)
			if err := b.walk(v.Field(field.index), depth+1, objectsOnly); err != nil {
				return err
			}
		}
		return nil
	default:
		return reject("unsupported JSON value")
	}
}
func validNumber(s string) error {
	if s == "" {
		return reject("invalid JSON number")
	}
	// Parse via the strict grammar scanner, never convert exact integer tokens to
	// float64 for persistent IDs or document values.
	sc := jsonScanner{data: []byte(s), l: DefaultLimits()}
	if err := sc.number(); err != nil || sc.pos != len(s) {
		return reject("invalid JSON number")
	}
	return nil
}
func hasMarshalAuthority(t reflect.Type) bool {
	j := reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	text := reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	return t.Implements(j) || t.Implements(text) || (t.Kind() != reflect.Pointer && (reflect.PointerTo(t).Implements(j) || reflect.PointerTo(t).Implements(text)))
}
func encodeBounded(v any, l Limits, max int) ([]byte, error) {
	if max < 1 {
		return nil, reject("invalid encoding budget")
	}
	b := jsonBudget{l: l, active: map[uintptr]bool{}}
	if err := b.walk(reflect.ValueOf(v), 1, false); err != nil {
		return nil, err
	}
	// Native incremental encoding never invokes caller marshalers or allocates a
	// complete oversized escaped JSON value before the output limit is checked.
	hint := b.encodedHint
	if hint > max {
		hint = max
	}
	out := &limitedJSON{max: max, data: make([]byte, 0, hint)}
	if e := encodeValue(out, reflect.ValueOf(v)); e != nil {
		return nil, e
	}
	return out.data, nil
}
func emit(b *limitedJSON, s string) error { _, e := b.Write([]byte(s)); return e }
func encodeString(b *limitedJSON, s string) error {
	if e := emit(b, "\""); e != nil {
		return e
	}
	const hex = "0123456789abcdef"
	for i := 0; i < len(s); {
		// Most persisted text is ASCII. Emit safe spans in one bounded write
		// instead of allocating and copying each rune separately.
		start := i
		for i < len(s) && s[i] >= 32 && s[i] < utf8.RuneSelf && s[i] != '\\' && s[i] != '"' {
			i++
		}
		if i > start {
			if e := emit(b, s[start:i]); e != nil {
				return e
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		var value string
		switch r {
		case '\\':
			value = "\\\\"
		case '"':
			value = "\\\""
		case '\b':
			value = "\\b"
		case '\f':
			value = "\\f"
		case '\n':
			value = "\\n"
		case '\r':
			value = "\\r"
		case '\t':
			value = "\\t"
		default:
			if r < 32 {
				value = string([]byte{'\\', 'u', '0', '0', hex[byte(r)>>4], hex[byte(r)&15]})
			} else {
				value = string(r)
			}
		}
		if e := emit(b, value); e != nil {
			return e
		}
	}
	return emit(b, "\"")
}
func encodeValue(b *limitedJSON, v reflect.Value) error {
	if !v.IsValid() {
		return emit(b, "null")
	}
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return emit(b, "null")
		}
		v = v.Elem()
	}
	if v.Type() == reflect.TypeOf(json.Number("")) {
		return emit(b, string(v.Interface().(json.Number)))
	}
	switch v.Kind() {
	case reflect.Bool:
		return emit(b, strconv.FormatBool(v.Bool()))
	case reflect.String:
		return encodeString(b, v.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return emit(b, strconv.FormatInt(v.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return emit(b, strconv.FormatUint(v.Uint(), 10))
	case reflect.Float32, reflect.Float64:
		return emit(b, strconv.FormatFloat(v.Float(), 'g', -1, v.Type().Bits()))
	case reflect.Map:
		if v.IsNil() {
			return emit(b, "null")
		}
		if e := emit(b, "{"); e != nil {
			return e
		}
		first := true
		it := v.MapRange()
		key := reflect.New(v.Type().Key()).Elem()
		value := reflect.New(v.Type().Elem()).Elem()
		for it.Next() {
			key.SetIterKey(it)
			value.SetIterValue(it)
			if !first {
				if e := emit(b, ","); e != nil {
					return e
				}
			}
			first = false
			if e := encodeString(b, key.String()); e != nil {
				return e
			}
			if e := emit(b, ":"); e != nil {
				return e
			}
			if e := encodeValue(b, value); e != nil {
				return e
			}
		}
		return emit(b, "}")
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return emit(b, "null")
		}
		if e := emit(b, "["); e != nil {
			return e
		}
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				if e := emit(b, ","); e != nil {
					return e
				}
			}
			if e := encodeValue(b, v.Index(i)); e != nil {
				return e
			}
		}
		return emit(b, "]")
	case reflect.Struct:
		if e := emit(b, "{"); e != nil {
			return e
		}
		first := true
		for _, field := range structJSONFields(v.Type()) {
			value := v.Field(field.index)
			if field.omitEmpty && isEmpty(value) {
				continue
			}
			if !first {
				if e := emit(b, ","); e != nil {
					return e
				}
			}
			first = false
			if e := encodeString(b, field.name); e != nil {
				return e
			}
			if e := emit(b, ":"); e != nil {
				return e
			}
			if e := encodeValue(b, value); e != nil {
				return e
			}
		}
		return emit(b, "}")
	}
	return reject("unsupported JSON value")
}
func isEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64, reflect.Interface, reflect.Pointer:
		return v.IsZero()
	}
	return false
}
func copyObject(v JSON, l Limits) (JSON, error) {
	if v == nil {
		return nil, reject("document/checkpoint root must be object")
	}
	b := jsonBudget{l: l, active: map[uintptr]bool{}}
	if err := b.walk(reflect.ValueOf(v), 1, true); err != nil {
		return nil, err
	}
	p, e := encodeBounded(v, l, l.MaxRecordBytes)
	if e != nil {
		return nil, e
	}
	var n JSON
	if e = decodeStrict(p, l, l.MaxRecordBytes, &n); e != nil {
		return nil, e
	}
	return n, nil
}
func decodeStrict(p []byte, l Limits, max int, target any) error {
	if len(p) > max || !utf8.Valid(p) {
		return reject("invalid or oversized JSON bytes")
	}
	sc := jsonScanner{data: p, l: l}
	sc.space()
	if sc.pos == len(p) || p[sc.pos] != '{' {
		return reject("JSON root must be object")
	}
	if err := sc.value(1); err != nil {
		return err
	}
	sc.space()
	if sc.pos != len(p) {
		return reject("trailing JSON data")
	}
	d := json.NewDecoder(bytes.NewReader(p))
	d.UseNumber()
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return reject("JSON record schema rejected")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return reject("trailing JSON data")
	}
	return nil
}

type jsonScanner struct {
	data  []byte
	pos   int
	l     Limits
	nodes int
}

func (s *jsonScanner) space() {
	for s.pos < len(s.data) && strings.ContainsRune(" \t\r\n", rune(s.data[s.pos])) {
		s.pos++
	}
}
func (s *jsonScanner) node() error {
	s.nodes++
	if s.nodes > s.l.MaxNodes {
		return reject("JSON node limit")
	}
	return nil
}
func (s *jsonScanner) value(depth int) error {
	if err := s.node(); err != nil {
		return err
	}
	s.space()
	if s.pos >= len(s.data) {
		return reject("incomplete JSON")
	}
	switch s.data[s.pos] {
	case '{', '[':
		if depth > s.l.MaxDepth {
			return reject("JSON depth limit")
		}
		obj := s.data[s.pos] == '{'
		end := byte(']')
		if obj {
			end = '}'
		}
		s.pos++
		s.space()
		if s.pos < len(s.data) && s.data[s.pos] == end {
			s.pos++
			return nil
		}
		keys := map[string]bool{}
		members := 0
		for {
			members++
			if members > s.l.MaxMembers {
				return reject("JSON members limit")
			}
			if obj {
				if err := s.node(); err != nil {
					return err
				}
				key, e := s.string()
				if e != nil {
					return e
				}
				if keys[key] {
					return reject("duplicate JSON key")
				}
				keys[key] = true
				s.space()
				if s.pos >= len(s.data) || s.data[s.pos] != ':' {
					return reject("invalid JSON object")
				}
				s.pos++
			}
			if err := s.value(depth + 1); err != nil {
				return err
			}
			s.space()
			if s.pos >= len(s.data) {
				return reject("incomplete JSON container")
			}
			if s.data[s.pos] == end {
				s.pos++
				return nil
			}
			if s.data[s.pos] != ',' {
				return reject("invalid JSON container")
			}
			s.pos++
			s.space()
		}
	case '"':
		_, e := s.string()
		return e
	case 't':
		return s.literal("true")
	case 'f':
		return s.literal("false")
	case 'n':
		return s.literal("null")
	default:
		return s.number()
	}
}
func (s *jsonScanner) literal(v string) error {
	if !bytes.HasPrefix(s.data[s.pos:], []byte(v)) {
		return reject("invalid JSON literal")
	}
	s.pos += len(v)
	return nil
}
func (s *jsonScanner) number() error {
	start := s.pos
	if s.pos < len(s.data) && s.data[s.pos] == '-' {
		s.pos++
	}
	if s.pos >= len(s.data) {
		return reject("invalid JSON number")
	}
	if s.data[s.pos] == '0' {
		s.pos++
	} else {
		if s.data[s.pos] < '1' || s.data[s.pos] > '9' {
			return reject("invalid JSON number")
		}
		for s.pos < len(s.data) && s.data[s.pos] >= '0' && s.data[s.pos] <= '9' {
			s.pos++
		}
	}
	if s.pos < len(s.data) && s.data[s.pos] == '.' {
		s.pos++
		digits := s.pos
		for s.pos < len(s.data) && s.data[s.pos] >= '0' && s.data[s.pos] <= '9' {
			s.pos++
		}
		if digits == s.pos {
			return reject("invalid JSON fraction")
		}
	}
	if s.pos < len(s.data) && (s.data[s.pos] == 'e' || s.data[s.pos] == 'E') {
		s.pos++
		if s.pos < len(s.data) && (s.data[s.pos] == '+' || s.data[s.pos] == '-') {
			s.pos++
		}
		digits := s.pos
		for s.pos < len(s.data) && s.data[s.pos] >= '0' && s.data[s.pos] <= '9' {
			s.pos++
		}
		if digits == s.pos {
			return reject("invalid JSON exponent")
		}
	}
	if s.pos == start {
		return reject("invalid JSON number")
	}
	return nil
}
func (s *jsonScanner) string() (string, error) {
	if s.pos >= len(s.data) || s.data[s.pos] != '"' {
		return "", reject("expected JSON string")
	}
	start := s.pos
	s.pos++
	decoded := 0
	escaped := false
	for s.pos < len(s.data) {
		c := s.data[s.pos]
		s.pos++
		if c == '"' {
			if !escaped {
				// UTF-8 validity and control/byte limits were checked by the
				// scanner. Avoid a second JSON decoder for ordinary strings.
				return string(s.data[start+1 : s.pos-1]), nil
			}
			var out string
			if err := json.Unmarshal(s.data[start:s.pos], &out); err != nil {
				return "", reject("invalid JSON string")
			}
			if len(out) > s.l.MaxStringBytes {
				return "", reject("JSON string byte limit")
			}
			return out, nil
		}
		if c < 0x20 {
			return "", reject("JSON control character")
		}
		if c != '\\' {
			decoded++
			if decoded > s.l.MaxStringBytes {
				return "", reject("JSON string byte limit")
			}
			continue
		}
		escaped = true
		if s.pos >= len(s.data) {
			return "", reject("incomplete JSON escape")
		}
		esc := s.data[s.pos]
		s.pos++
		switch esc {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			decoded++
		case 'u':
			u, e := s.hex()
			if e != nil {
				return "", e
			}
			if u >= 0xd800 && u <= 0xdbff {
				if s.pos+2 > len(s.data) || s.data[s.pos] != '\\' || s.data[s.pos+1] != 'u' {
					return "", reject("unpaired JSON surrogate")
				}
				s.pos += 2
				v, e := s.hex()
				if e != nil || v < 0xdc00 || v > 0xdfff {
					return "", reject("unpaired JSON surrogate")
				}
				decoded += 4
			} else if u >= 0xdc00 && u <= 0xdfff {
				return "", reject("unpaired JSON surrogate")
			} else {
				decoded += utf8.RuneLen(rune(u))
			}
		default:
			return "", reject("invalid JSON escape")
		}
		if decoded > s.l.MaxStringBytes {
			return "", reject("JSON string byte limit")
		}
	}
	return "", reject("unterminated JSON string")
}
func (s *jsonScanner) hex() (uint64, error) {
	if s.pos+4 > len(s.data) {
		return 0, reject("incomplete JSON unicode escape")
	}
	u, e := strconv.ParseUint(string(s.data[s.pos:s.pos+4]), 16, 16)
	if e != nil {
		return 0, reject("invalid JSON unicode escape")
	}
	s.pos += 4
	return u, nil
}
