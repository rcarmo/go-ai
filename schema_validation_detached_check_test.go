package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// validator.test.ts:135–138. The method value captures the prepared schema;
// the caller keeps only the check function, as in the pinned filter call.
func TestSourceV160SchemaDetachedCheck(t *testing.T) {
	check := compileV160LeafSchema(t, v160ReadDiagnosticsSchema).Check
	filter := func(t *testing.T, input []map[string]any) []map[string]any {
		t.Helper()
		selected := make([]map[string]any, 0, len(input))
		for _, value := range input {
			keep, err := check(value)
			if err != nil {
				t.Fatal(err)
			}
			if keep {
				selected = append(selected, value)
			}
		}
		return selected
	}
	t.Run("L137-filter-two-values", func(t *testing.T) {
		input := []map[string]any{{"path": "a"}, {"path": 1}}
		assertV160StandaloneJSON(t, filter(t, input), `[{"path":"a"}]`)
	})
	t.Run("supplemental-keeps-input-order", func(t *testing.T) {
		input := []map[string]any{
			{"path": 3}, {"path": "b", "offset": 2}, {"path": "bad-offset", "offset": 0},
			{"path": "a"}, {"offset": 2}, {"path": "b", "offset": 4},
		}
		assertV160StandaloneJSON(t, filter(t, input), `[{"offset":2,"path":"b"},{"path":"a"},{"offset":4,"path":"b"}]`)
	})
	t.Run("supplemental-fresh-verdict-for-same-object", func(t *testing.T) {
		value := map[string]any{"path": "a"}
		var got [3]bool
		for i, path := range []any{"a", 1, "b"} {
			value["path"] = path
			accepted, err := check(value)
			if err != nil {
				t.Fatal(err)
			}
			got[i] = accepted
		}
		if want := [3]bool{true, false, true}; got != want {
			t.Fatalf("checks of reused input = %v; want %v", got, want)
		}
	})
}

// validator.test.ts:77–81. Supply the explicit native undefined marker as
// a present map value, distinct from an omitted key or JSON null.
func TestSourceV160SchemaReadOptionalUndefined(t *testing.T) {
	compiled := compileV160LeafSchema(t, v160ReadDiagnosticsSchema)
	undefined := goai.SchemaUndefined{}
	for _, tc := range []struct {
		name  string
		input any
		want  bool
	}{
		{"L79-present-optional-undefined", map[string]any{"path": "a.ts", "offset": undefined}, true},
		{"L80-present-required-undefined", map[string]any{"path": undefined}, false},
		{"supplemental-json-null-is-present", json.RawMessage(`{"path":"a.ts","offset":null}`), false},
		{"supplemental-missing-optional-offset", json.RawMessage(`{"path":"a.ts"}`), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := compiled.Check(tc.input)
			if err != nil || got != tc.want {
				t.Fatalf("check(%#v) = %t, %v; want %t", tc.input, got, err, tc.want)
			}
		})
	}
}
