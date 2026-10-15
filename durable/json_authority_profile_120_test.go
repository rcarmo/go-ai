package durable

import (
	"encoding"
	"encoding/json"
	"reflect"
	"testing"
)

type authorityPointer120 string

func (*authorityPointer120) MarshalJSON() ([]byte, error) { return []byte(`null`), nil }

type authorityValue120 string

func (authorityValue120) MarshalText() ([]byte, error) { return []byte("x"), nil }
func TestJSON120MethodlessFastPathPreservesAuthorityRejection(t *testing.T) {
	types := []reflect.Type{reflect.TypeOf(""), reflect.TypeOf(1), reflect.TypeOf(JSON{}), reflect.TypeOf([]any{}), reflect.TypeOf(authorityPointer120("")), reflect.TypeOf((*authorityPointer120)(nil)), reflect.TypeOf(authorityValue120("")), reflect.TypeOf((*authorityValue120)(nil)), reflect.TypeOf(struct{ Value string }{})}
	for _, typ := range types {
		j := reflect.TypeOf((*json.Marshaler)(nil)).Elem()
		text := reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
		want := typ.Implements(j) || typ.Implements(text) || (typ.Kind() != reflect.Pointer && (reflect.PointerTo(typ).Implements(j) || reflect.PointerTo(typ).Implements(text)))
		if got := hasMarshalAuthority(typ); got != want {
			t.Fatal(typ, got, want)
		}
	}
	for _, value := range []any{authorityPointer120("private"), authorityValue120("private"), map[authorityPointer120]any{"key": 1}} {
		if _, err := encodeBounded(value, DefaultLimits(), DefaultLimits().MaxRecordBytes); err == nil {
			t.Fatal("marshal authority accepted", value)
		}
	}
}
