package jsonparse

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// JSONChange is one incremental edit. Paths contain object keys and array indices.
// Consumers must not mutate values shared with snapshots or subsequent edits.
type JSONChange struct {
	Kind  string `json:"kind"`
	Path  []any  `json:"path"`
	Value any    `json:"value,omitempty"`
	Text  any    `json:"text,omitempty"`
}
type streamSlot struct {
	kind  byte
	key   string
	index int
}
type streamFrame struct {
	value  any
	slot   *streamSlot
	path   []any
	inside bool
	size   int
	key    string
	hasKey bool
	phase  byte
}
type streamString struct {
	key        bool
	slot       *streamSlot
	path       []any
	text       utf16Buffer
	escape     string
	change     int
	changeText utf16Buffer
}

// StreamingJSONParser incrementally consumes chunks and copies only open
// containers for snapshots. Copy credit bounds repeated wide-container previews.
// Previews preserve JS UTF-16 units and number overflow; they are not validated
// persisted JSON. Completed calls still need the final-argument contract.
type StreamingJSONParser struct {
	root             any
	present, done    bool
	stack            []*streamFrame
	str              *streamString
	scalar           strings.Builder
	scalarSlot       *streamSlot
	pathCost, credit int
	dirty            bool
	snapshot         any
	snapshotPresent  bool
	changes          []JSONChange
	revision         uint64
	preserveNumbers  bool
}

func (p *StreamingJSONParser) Append(text string) []JSONChange {
	units := 0
	for rest := text; len(rest) > 0; {
		r, n, _ := previewRune(rest)
		rest = rest[n:]
		units++
		if r > 0xffff {
			units++
		}
	}
	p.credit += 256 + 8*units
	p.changes = []JSONChange{}
	if p.str != nil {
		p.str.change = -1
		p.str.changeText.Reset()
	}
	for i := 0; i < len(text) && !p.done; {
		if p.str != nil {
			i = p.readString(text, i)
			continue
		}
		if p.scalar.Len() != 0 {
			end := i
			for end < len(text) && !streamDelimiter(text[end]) {
				end++
			}
			if end > i {
				p.scalar.WriteString(text[i:end])
				if p.scalarSlot != nil {
					p.dirty = true
				}
			}
			i = end
			if end < len(text) {
				p.endScalar()
			}
			continue
		}
		c := text[i]
		i++
		switch c {
		case ' ', '\t', '\n', '\r':
		case '"':
			p.startString()
		case '{', '[':
			p.openContainer(c == '[')
		case '}', ']':
			p.closeContainer()
		case ':':
			if f := p.top(); f != nil && f.phase == 'c' {
				f.phase = 'v'
			}
		case ',':
			if f := p.top(); f != nil {
				if _, array := f.value.([]any); array {
					f.phase = 'v'
				} else {
					f.phase = 'k'
				}
			}
		default:
			p.scalar.WriteByte(c)
			p.scalarSlot = p.valueSlot()
			if p.scalarSlot != nil {
				p.dirty = true
			}
		}
	}
	return p.changes
}

// Snapshot returns (value, true), including (nil, true) for JSON null. False
// means no preview exists yet. Previously returned values are never mutated.
func (p *StreamingJSONParser) Snapshot() (any, bool) {
	if !p.dirty {
		return p.snapshot, p.snapshotPresent
	}
	slot, value, valid := p.pendingScalar()
	cost := p.pathCost
	if valid {
		cost++
	}
	if cost > p.credit {
		return p.snapshot, p.snapshotPresent
	}
	p.credit -= cost
	previous, previousPresent := p.snapshot, p.snapshotPresent
	defer func() {
		if previousPresent != p.snapshotPresent || !samePreviewScalar(previous, p.snapshot) {
			p.revision++
		}
	}()
	p.dirty = false
	if len(p.stack) == 0 {
		p.snapshot, p.snapshotPresent = p.root, p.present
		if valid && slot.kind == 'r' {
			p.snapshot, p.snapshotPresent = value, true
		}
		return p.snapshot, p.snapshotPresent
	}
	var child any
	var childSlot *streamSlot
	hasChild := false
	for i := len(p.stack) - 1; i >= 0; i-- {
		f := p.stack[i]
		var copy any
		switch v := f.value.(type) {
		case []any:
			copy = append(make([]any, 0, len(v)), v...)
		case map[string]any:
			m := make(map[string]any, len(v))
			for k, x := range v {
				m[k] = x
			}
			copy = m
		}
		if i == len(p.stack)-1 && valid {
			copy = streamSet(copy, slot, value)
		}
		if hasChild && childSlot != nil {
			copy = streamSet(copy, childSlot, child)
		}
		child, childSlot, hasChild = copy, f.slot, true
	}
	p.snapshot, p.snapshotPresent = p.root, p.present
	if childSlot != nil && childSlot.kind == 'r' {
		p.snapshot, p.snapshotPresent = child, true
	}
	return p.snapshot, p.snapshotPresent
}
func (p *StreamingJSONParser) top() *streamFrame {
	if len(p.stack) == 0 {
		return nil
	}
	return p.stack[len(p.stack)-1]
}
func (p *StreamingJSONParser) valueSlot() *streamSlot {
	f := p.top()
	if f == nil {
		if !p.present {
			return &streamSlot{kind: 'r'}
		}
		return nil
	}
	if v, ok := f.value.([]any); ok {
		return &streamSlot{kind: 'e', index: len(v)}
	}
	if (f.phase == 'v' || f.phase == 'c') && f.hasKey {
		return &streamSlot{kind: 'm', key: f.key}
	}
	return nil
}
func (p *StreamingJSONParser) path(slot *streamSlot) []any {
	if slot == nil {
		return nil
	}
	if slot.kind == 'r' {
		return []any{}
	}
	f := p.top()
	if !f.inside {
		return nil
	}
	path := make([]any, len(f.path)+1)
	copy(path, f.path)
	if slot.kind == 'm' {
		path[len(f.path)] = slot.key
	} else {
		path[len(f.path)] = slot.index
	}
	return path
}
func (p *StreamingJSONParser) place(slot *streamSlot, value any) {
	if slot == nil {
		return
	}
	p.dirty = true
	if slot.kind == 'r' {
		p.root, p.present = value, true
		return
	}
	f := p.top()
	if slot.kind == 'e' {
		v := f.value.([]any)
		if slot.index == len(v) {
			v = append(v, value)
			f.size++
			p.pathCost++
		} else {
			v[slot.index] = value
		}
		f.value = v
		return
	}
	v := f.value.(map[string]any)
	if _, ok := v[slot.key]; !ok {
		f.size++
		p.pathCost++
	}
	v[slot.key] = value
}
func streamSet(container any, slot *streamSlot, value any) any {
	if slot.kind == 'e' {
		v := container.([]any)
		if slot.index == len(v) {
			v = append(v, value)
		} else {
			v[slot.index] = value
		}
		return v
	} else if slot.kind == 'm' {
		container.(map[string]any)[slot.key] = value
	}
	return container
}
func (p *StreamingJSONParser) ended() {
	if f := p.top(); f != nil {
		f.phase = ','
	} else if p.present {
		p.done = true
	}
}
func (p *StreamingJSONParser) openContainer(array bool) {
	slot := p.valueSlot()
	path := p.path(slot)
	var value any = map[string]any{}
	phase := byte('k')
	if array {
		value = []any{}
		phase = 'v'
	}
	p.place(slot, value)
	p.stack = append(p.stack, &streamFrame{value: value, slot: slot, path: path, inside: path != nil && len(path) < 32, phase: phase})
	p.pathCost++
	p.dirty = true
	if path != nil {
		var empty any = map[string]any{}
		if array {
			empty = []any{}
		}
		p.changes = append(p.changes, JSONChange{Kind: "set", Path: path, Value: empty})
	}
}
func (p *StreamingJSONParser) closeContainer() {
	f := p.top()
	if f == nil {
		return
	}
	p.stack = p.stack[:len(p.stack)-1]
	p.pathCost -= f.size + 1
	p.dirty = true
	p.place(f.slot, f.value)
	if f.path != nil && !f.inside {
		p.changes = append(p.changes, JSONChange{Kind: "set", Path: f.path, Value: f.value})
	}
	p.ended()
}
func (p *StreamingJSONParser) startString() {
	f := p.top()
	if f != nil {
		if _, array := f.value.([]any); !array && (f.phase == 'k' || f.phase == ',') {
			p.str = &streamString{key: true, change: -1}
			return
		}
	}
	slot := p.valueSlot()
	path := p.path(slot)
	s := &streamString{slot: slot, path: path, change: -1}
	p.str = s
	p.place(slot, "")
	if path != nil {
		p.changes = append(p.changes, JSONChange{Kind: "set", Path: path, Value: ""})
		s.change = len(p.changes) - 1
	}
}
func (p *StreamingJSONParser) publishString(s *streamString, add func(*utf16Buffer)) {
	if s.key {
		return
	}
	value := s.text.Value()
	p.place(s.slot, value)
	if s.path == nil {
		return
	}
	if s.change >= 0 && s.change == len(p.changes)-1 {
		c := &p.changes[s.change]
		if c.Kind == "set" {
			c.Value = value
		} else {
			add(&s.changeText)
			c.Text = s.changeText.Value()
		}
		return
	}
	s.changeText.Reset()
	add(&s.changeText)
	p.changes = append(p.changes, JSONChange{Kind: "append", Path: s.path, Text: s.changeText.Value()})
	s.change = len(p.changes) - 1
}
func (p *StreamingJSONParser) stringText(s *streamString, text string) {
	s.text.WriteText(text)
	p.publishString(s, func(b *utf16Buffer) { b.WriteText(text) })
}
func (p *StreamingJSONParser) unicode(s *streamString, u uint16) {
	s.text.WriteUnit(u)
	p.publishString(s, func(b *utf16Buffer) { b.WriteUnit(u) })
}
func (p *StreamingJSONParser) readString(text string, start int) int {
	s := p.str
	run := start
	for i := start; i < len(text); i++ {
		c := text[i]
		if s.escape != "" {
			consumed := false
			if s.escape == "\\" {
				if decoded, ok := streamEscapes[c]; ok {
					s.escape = ""
					p.stringText(s, decoded)
					consumed = true
				} else if c == 'u' {
					s.escape = "\\u"
					consumed = true
				} else {
					s.escape = ""
					p.stringText(s, "\\")
				}
			} else if streamHex(c) {
				s.escape += string(c)
				consumed = true
				if len(s.escape) == 6 {
					u, _ := strconv.ParseUint(s.escape[2:], 16, 16)
					s.escape = ""
					p.unicode(s, uint16(u))
				}
			} else {
				literal := s.escape
				s.escape = ""
				p.stringText(s, literal)
			}
			if consumed {
				run = i + 1
				continue
			}
			run = i
		}
		if c == '"' {
			if i > run {
				p.stringText(s, text[run:i])
			}
			p.str = nil
			if s.key {
				if f := p.top(); f != nil {
					f.key, f.hasKey, f.phase = s.text.Key(), true, 'c'
				}
			} else {
				p.ended()
			}
			return i + 1
		}
		if c == '\\' {
			if i > run {
				p.stringText(s, text[run:i])
			}
			s.escape = "\\"
			run = i + 1
		}
	}
	if len(text) > run {
		p.stringText(s, text[run:])
	}
	return len(text)
}
func (p *StreamingJSONParser) endScalar() {
	token, slot := p.scalar.String(), p.scalarSlot
	p.scalar.Reset()
	p.scalarSlot = nil
	value, ok := p.scalarValue(token, false)
	if ok {
		p.place(slot, value)
		if path := p.path(slot); path != nil {
			p.changes = append(p.changes, JSONChange{Kind: "set", Path: path, Value: value})
		}
	}
	p.ended()
}
func (p *StreamingJSONParser) pendingScalar() (*streamSlot, any, bool) {
	if p.scalarSlot == nil || p.scalar.Len() == 0 || p.scalar.Len() > 64 {
		return nil, nil, false
	}
	value, ok := p.scalarValue(p.scalar.String(), true)
	return p.scalarSlot, value, ok
}

var streamNumber = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)
var streamExponent = regexp.MustCompile(`^(-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?)[eE][+-]?$`)

func streamScalar(token string, pending bool) (any, bool) {
	for _, literal := range []struct {
		text  string
		value any
	}{{"true", true}, {"false", false}, {"null", nil}} {
		if token == literal.text || pending && strings.HasPrefix(literal.text, token) {
			return literal.value, true
		}
	}
	if pending {
		if match := streamExponent.FindStringSubmatch(token); match != nil {
			token = match[1]
		}
	}
	if !streamNumber.MatchString(token) {
		return nil, false
	}
	value, err := strconv.ParseFloat(token, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return nil, false
	}
	return value, true
}
func streamDelimiter(c byte) bool { return strings.ContainsRune(" \t\n\r,:{}[]\"", rune(c)) }
func streamHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

var streamEscapes = map[byte]string{'"': "\"", '\\': "\\", '/': "/", 'b': "\b", 'f': "\f", 'n': "\n", 'r': "\r", 't': "\t"}

func (c JSONChange) MarshalJSON() ([]byte, error) {
	value := map[string]any{"kind": c.Kind, "path": c.Path}
	if c.Kind == "set" {
		value["value"] = c.Value
	} else {
		value["text"] = c.Text
	}
	return marshalPreview(value)
}

func samePreviewScalar(a, b any) bool {
	switch a.(type) {
	case nil:
		return b == nil
	case bool, string, float64, UTF16String:
		return a == b
	default:
		return false
	}
}

func (p *StreamingJSONParser) scalarValue(token string, pending bool) (any, bool) {
	if p.preserveNumbers {
		number := token
		if pending {
			if match := streamExponent.FindStringSubmatch(number); match != nil {
				number = match[1]
			}
		}
		if streamNumber.MatchString(number) {
			return json.Number(number), true
		}
	}
	return streamScalar(token, pending)
}
