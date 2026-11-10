package durable

import (
	"encoding/json"
	"sort"
)

// ImmutableValue is a small frozen view over a deep-owned strict JSON value.
type ImmutableValue struct {
	kind       string
	string     string
	number     json.Number
	bool       bool
	mapValue   map[string]*ImmutableValue
	arrayValue []*ImmutableValue
}

func NewImmutableValue(input any, limits Limits) (*ImmutableValue, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	owned, err := ownJSONValue(input, limits)
	if err != nil {
		return nil, err
	}
	return freezeImmutableValue(owned)
}

func freezeImmutableValue(input any) (*ImmutableValue, error) {
	switch value := input.(type) {
	case nil:
		return &ImmutableValue{kind: "null"}, nil
	case bool:
		return &ImmutableValue{kind: "bool", bool: value}, nil
	case string:
		return &ImmutableValue{kind: "string", string: value}, nil
	case json.Number:
		return &ImmutableValue{kind: "number", number: value}, nil
	case map[string]any:
		out := &ImmutableValue{kind: "object", mapValue: make(map[string]*ImmutableValue, len(value))}
		for key, child := range value {
			node, err := freezeImmutableValue(child)
			if err != nil {
				return nil, err
			}
			out.mapValue[key] = node
		}
		return out, nil
	case []any:
		out := &ImmutableValue{kind: "array", arrayValue: make([]*ImmutableValue, len(value))}
		for i, child := range value {
			node, err := freezeImmutableValue(child)
			if err != nil {
				return nil, err
			}
			out.arrayValue[i] = node
		}
		return out, nil
	default:
		return nil, reject("unsupported immutable JSON value")
	}
}

func (v *ImmutableValue) Kind() string {
	if v == nil {
		return "null"
	}
	return v.kind
}

func (v *ImmutableValue) IsObject() bool { return v != nil && v.kind == "object" }
func (v *ImmutableValue) IsArray() bool  { return v != nil && v.kind == "array" }

func (v *ImmutableValue) Get(key string) (*ImmutableValue, bool) {
	if v == nil || !v.IsObject() {
		return nil, false
	}
	node, ok := v.mapValue[key]
	return node, ok
}

func (v *ImmutableValue) Index(i int) (*ImmutableValue, bool) {
	if v == nil || !v.IsArray() || i < 0 || i >= len(v.arrayValue) {
		return nil, false
	}
	return v.arrayValue[i], true
}

func (v *ImmutableValue) String() string {
	if v == nil || v.kind != "string" {
		return ""
	}
	return v.string
}

func (v *ImmutableValue) Number() json.Number {
	if v == nil || v.kind != "number" {
		return json.Number("")
	}
	return v.number
}

func (v *ImmutableValue) Bool() bool {
	if v == nil || v.kind != "bool" {
		return false
	}
	return v.bool
}

func (v *ImmutableValue) Len() int {
	if v == nil {
		return 0
	}
	switch v.kind {
	case "object":
		return len(v.mapValue)
	case "array":
		return len(v.arrayValue)
	default:
		return 0
	}
}

func (v *ImmutableValue) Keys() []string {
	if v == nil || !v.IsObject() {
		return nil
	}
	keys := make([]string, 0, len(v.mapValue))
	for key := range v.mapValue {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (v *ImmutableValue) Copy() any {
	if v == nil {
		return nil
	}
	switch v.kind {
	case "null":
		return nil
	case "bool":
		return v.bool
	case "string":
		return v.string
	case "number":
		return json.Number(v.number)
	case "object":
		out := make(map[string]any, len(v.mapValue))
		for key, child := range v.mapValue {
			out[key] = child.Copy()
		}
		return out
	case "array":
		out := make([]any, len(v.arrayValue))
		for i, child := range v.arrayValue {
			out[i] = child.Copy()
		}
		return out
	default:
		return nil
	}
}

func (v *ImmutableValue) Object() (*ImmutableValue, bool) {
	if v == nil || !v.IsObject() {
		return nil, false
	}
	return v, true
}

type ImmutableDocument struct {
	key   string
	value *ImmutableValue
}

func NewImmutableDocument(kind string, value JSON, limits Limits) (*ImmutableDocument, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	if !validKind(kind) {
		return nil, reject("invalid immutable document kind")
	}
	if value == nil {
		return nil, reject("immutable document requires an object")
	}
	if _, err := encodeBounded(value, limits, limits.MaxDocumentBytes); err != nil {
		return nil, err
	}
	owned, err := NewImmutableValue(value, limits)
	if err != nil {
		return nil, err
	}
	return &ImmutableDocument{key: kind, value: owned}, nil
}

func (d *ImmutableDocument) Key() string            { return d.key }
func (d *ImmutableDocument) Value() *ImmutableValue { return d.value }
func (d *ImmutableDocument) Copy() any              { return d.value.Copy() }

// ReadonlyValue is a comparable JSON handle. Object/array identity is preserved
// by handle equality. It exposes neither mutable containers nor a pointer that
// callers could overwrite; Copy is the explicit detached conversion.
type ReadonlyValue interface {
	Kind() string
	IsObject() bool
	IsArray() bool
	Get(string) (ReadonlyValue, bool)
	Index(int) (ReadonlyValue, bool)
	Len() int
	Keys() []string
	String() string
	Number() json.Number
	Bool() bool
	Copy() any
	immutableNode() *ImmutableValue
}

type readonlyJSON struct{ node *ImmutableValue }

func readonlyValue(node *ImmutableValue) ReadonlyValue { return readonlyJSON{node} }
func (v readonlyJSON) immutableNode() *ImmutableValue  { return v.node }
func (v readonlyJSON) Kind() string                    { return v.node.Kind() }
func (v readonlyJSON) IsObject() bool                  { return v.node.IsObject() }
func (v readonlyJSON) IsArray() bool                   { return v.node.IsArray() }
func (v readonlyJSON) Get(key string) (ReadonlyValue, bool) {
	node, ok := v.node.Get(key)
	if !ok {
		return nil, false
	}
	return readonlyValue(node), true
}
func (v readonlyJSON) Index(index int) (ReadonlyValue, bool) {
	node, ok := v.node.Index(index)
	if !ok {
		return nil, false
	}
	return readonlyValue(node), true
}
func (v readonlyJSON) Len() int            { return v.node.Len() }
func (v readonlyJSON) Keys() []string      { return v.node.Keys() }
func (v readonlyJSON) String() string      { return v.node.String() }
func (v readonlyJSON) Number() json.Number { return v.node.Number() }
func (v readonlyJSON) Bool() bool          { return v.node.Bool() }
func (v readonlyJSON) Copy() any           { return v.node.Copy() }
func (v readonlyJSON) MarshalJSON() ([]byte, error) {
	if v.node == nil {
		return []byte("null"), nil
	}
	return v.node.MarshalJSON()
}
