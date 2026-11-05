package durable

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// The original complete replay is an independent differential control for the
// bounded scalar-set fast path. Every intermediate revision still gets checked.
func referencePrimitiveBatch(value any, operations []Operation, limits Limits) (any, error) {
	ops, err := ownOperations(operations, limits)
	if err != nil {
		return nil, err
	}
	root, err := ownJSONValue(value, limits)
	if err != nil {
		return nil, err
	}
	if err := checkDeltaValue(root, limits); err != nil {
		return nil, err
	}
	for _, op := range ops {
		root, err = applyOperation(root, op, limits)
		if err != nil {
			return nil, err
		}
		if err := checkDeltaValue(root, limits); err != nil {
			return nil, err
		}
	}
	return root, nil
}

func TestV150PrimitiveBatchPreservesIntermediateBudgets(t *testing.T) {
	for _, name := range []string{"new-keys", "same-key", "byte-intermediate", "member-growth", "node-growth", "container-replacement", "different-parents", "array-parent"} {
		t.Run(name, func(t *testing.T) {
			limits := DefaultLimits()
			value := any(JSON{"keys": map[string]any{}})
			ops := make([]Operation, 64)
			for i := range ops {
				ops[i] = Operation{"s", []any{"keys", fmt.Sprint(i)}, i}
			}
			switch name {
			case "same-key":
				for i := range ops {
					var scalar any = json.Number("9007199254740993")
					if i%3 == 0 {
						scalar = nil
					}
					if i%3 == 1 {
						scalar = "escaped\n\"日本語"
					}
					ops[i] = Operation{"s", []any{"keys", "x"}, scalar}
				}
			case "byte-intermediate":
				limits.MaxDocumentBytes = 80
				value = JSON{"keys": map[string]any{"old": strings.Repeat("a", 50)}}
				for i := range ops {
					ops[i] = Operation{"s", []any{"keys", "old"}, "small"}
				}
				ops[0] = Operation{"s", []any{"keys", "old"}, strings.Repeat("x", 100)}
			case "member-growth":
				limits.MaxMembers = 128
				keys := map[string]any{}
				for i := 0; i < 65; i++ {
					keys[fmt.Sprint(i+1000)] = i
				}
				value = JSON{"keys": keys}
			case "node-growth":
				limits.MaxNodes = 600
				keys := map[string]any{}
				for i := 0; i < 250; i++ {
					keys[fmt.Sprint(i+1000)] = i
				}
				value = JSON{"keys": keys}
			case "container-replacement":
				value = JSON{"keys": map[string]any{"x": map[string]any{"nested": true}}}
				for i := range ops {
					ops[i] = Operation{"s", []any{"keys", "x"}, i}
				}
			case "different-parents":
				value = JSON{"a": map[string]any{}, "b": map[string]any{}}
				for i := range ops {
					parent := "a"
					if i%2 == 0 {
						parent = "b"
					}
					ops[i] = Operation{"s", []any{parent, "x"}, i}
				}
			case "array-parent":
				value = JSON{"boxes": []any{map[string]any{}}}
				for i := range ops {
					ops[i] = Operation{"s", []any{"boxes", 0, fmt.Sprint(i)}, i}
				}
			}
			before, err := ownJSONValue(value, limits)
			if err != nil {
				t.Fatal(err)
			}
			want, wantErr := referencePrimitiveBatch(value, ops, limits)
			got, gotErr := ApplyOperations(value, ops, limits)
			if (gotErr == nil) != (wantErr == nil) {
				t.Fatalf("got error=%v want=%v", gotErr, wantErr)
			}
			if gotErr == nil && !equalJSONValue(got, want) {
				t.Fatalf("result differs: got=%v want=%v", got, want)
			}
			if gotErr != nil && gotErr.Error() != wantErr.Error() {
				t.Fatalf("error differs: got=%v want=%v", gotErr, wantErr)
			}
			after, err := ownJSONValue(value, limits)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("input changed: %v", err)
			}
		})
	}
}

func BenchmarkV150PrimitiveBatchReplay(b *testing.B) {
	operations := make([]Operation, 1000)
	for i := range operations {
		operations[i] = Operation{"s", []any{"keys", fmt.Sprint(i)}, i}
	}
	limits := DefaultLimits()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := ApplyOperations(JSON{"keys": map[string]any{}}, operations, limits); err != nil {
			b.Fatal(err)
		}
	}
}
