package durable

import (
	"bytes"
	"testing"
)

// Native representation control: every readonly subtree of source arguments
// retains its JavaScript string units when encoded or independently owned.
func TestBuiltinToolUTF16ReadonlySubtrees(t *testing.T) {
	scalar := toolUTF16Scalar(t, `"\ud800"`)
	key := scalar.WTF8()
	arguments, err := NewImmutableValue(sourceToolArguments{"nested": JSON{key: scalar}, "array": []any{scalar}}, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	root := readonlyValue(arguments)
	nested := immutableViewMember(t, root, "nested")
	leaf := immutableViewMember(t, nested, key)
	array := immutableViewMember(t, root, "array")
	for _, value := range []ReadonlyValue{root, nested, leaf, array} {
		clone, err := NewImmutableValue(value, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		wire, err := clone.MarshalJSON()
		if err != nil || !bytes.Contains(wire, []byte(`\ud800`)) {
			t.Fatalf("subtree encoding %s: %v", wire, err)
		}
	}
	for _, value := range []any{scalar, JSON{key: scalar}, []any{scalar}} {
		if _, err := NewImmutableValue(value, DefaultLimits()); err == nil {
			t.Fatal("untyped value gained UTF-16 authority")
		}
	}
}
