package goai

import "testing"

func TestSourceV152SchemaEqual(t *testing.T) {
	undefined := SchemaUndefined{}
	for _, tc := range []struct {
		name string
		a, b any
		want bool
	}{
		{"number", 1, 1, true},
		{"different strings", "a", "b", false},
		{"null", nil, nil, true},
		{"null and undefined", nil, undefined, false},
		{"object order", map[string]any{"a": 1, "b": []any{1, map[string]any{"c": 2}}}, map[string]any{"b": []any{1, map[string]any{"c": 2}}, "a": 1}, true},
		{"extra object key", map[string]any{"a": 1}, map[string]any{"a": 1, "b": 2}, false},
		{"array order", []any{1, 2}, []any{2, 1}, false},
		{"array and indexed object", []any{1}, map[string]any{"0": 1}, false},
		{"empty object and array", map[string]any{}, []any{}, false},
		{"present undefined and missing", map[string]any{"a": undefined}, map[string]any{}, false},
		{"undefined identity", undefined, undefined, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := SchemaEqual(tc.a, tc.b); got != tc.want {
				t.Fatalf("equal = %v, want %v", got, tc.want)
			}
		})
	}
}
