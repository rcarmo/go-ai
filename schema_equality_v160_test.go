package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// equal.test.ts:8–28. Both pinned implementations produced this ten-entry
// vector. The typed sentinel preserves the two non-JSON undefined inputs.
func TestSourceV160SchemaStructuralEquality(t *testing.T) {
	undefined := goai.SchemaUndefined{}
	cases := [10][2]any{
		{float64(1), float64(1)},
		{"a", "b"},
		{nil, nil},
		{nil, undefined},
		// These JSON inputs list identical object members in opposite order.
		{
			decodeV160DiagnosticInput(t, `{"a":1,"b":[1,{"c":2}]}`),
			decodeV160DiagnosticInput(t, `{"b":[1,{"c":2}],"a":1}`),
		},
		{map[string]any{"a": float64(1)}, map[string]any{"a": float64(1), "b": float64(2)}},
		{[]any{float64(1), float64(2)}, []any{float64(2), float64(1)}},
		{[]any{float64(1)}, map[string]any{"0": float64(1)}},
		{map[string]any{}, []any{}},
		{map[string]any{"a": undefined}, map[string]any{}},
	}
	want := [10]bool{true, false, true, false, true, false, false, false, false, false}
	var got, reversed [10]bool
	for i, pair := range cases {
		got[i] = goai.SchemaEqual(pair[0], pair[1])
		reversed[i] = goai.SchemaEqual(pair[1], pair[0])
	}
	if got != want {
		t.Fatalf("equality in source order = %v; want %v", got, want)
	}
	if reversed != want {
		t.Fatalf("equality with operands reversed = %v; want %v", reversed, want)
	}
}
