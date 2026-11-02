// This explicitly invoked package preserves unresolved format oracles.
// Ordinary ./... excludes testdata; these are failing assertions, not skips.
package pending_test

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func regexOutcome(t *testing.T, schema, input json.RawMessage) string {
	t.Helper()
	var args map[string]interface{}
	if err := json.Unmarshal(input, &args); err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	tool := goai.Tool{Name: "regex", Parameters: schema}
	got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: args})
	after, marshalErr := json.Marshal(args)
	if marshalErr != nil || string(before) != string(after) {
		t.Fatal("caller changed")
	}
	if err != nil {
		lines := strings.Split(err.Error(), "\n")
		if strings.HasPrefix(lines[0], "Validation failed") && len(lines) > 1 {
			return "REJECT " + strings.TrimSpace(lines[1])
		}
		return "THROW " + lines[0]
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	return "ok " + string(data)
}

// Go's encoding/json HTML escapes are a serialization adaptation, not a
// different Tool result. Compare successful JSON values; keep diagnostics exact.
func assertPendingOutcome(t *testing.T, got, want string) {
	t.Helper()
	if strings.HasPrefix(got, "ok ") && strings.HasPrefix(want, "ok ") {
		var actual, expected interface{}
		if err := json.Unmarshal([]byte(got[3:]), &actual); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(want[3:]), &expected); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("got %s, want %s", got, want)
		}
		return
	}
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestPendingOutcomeJSONEscapes(t *testing.T) {
	assertPendingOutcome(t, `ok {"v":"\u003c\u003e\u0026"}`, `ok {"v":"<>&"}`)
}

func TestRegexFormatOracle(t *testing.T) {
	type row struct {
		Name          string
		Schema, Input json.RawMessage
		Want          string
		Valid         bool
	}
	var f struct {
		Upstream                     string
		Literal, Derived, Dependency []row
	}
	data, err := os.ReadFile("../validation-regex-pending.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if f.Upstream != "c9f4c7b9fcd0f3e6b5bbc35c7c9b6cda35580eb8" || len(f.Literal) != 1 || len(f.Derived) != 32 || len(f.Dependency) != 18 {
		t.Fatal("unexpected pin/count")
	}
	for _, group := range []struct {
		name string
		rows []row
	}{{"literal", f.Literal}, {"source-derived", f.Derived}} {
		t.Run(group.name, func(t *testing.T) {
			for _, tc := range group.rows {
				t.Run(tc.Name, func(t *testing.T) {
					assertPendingOutcome(t, regexOutcome(t, tc.Schema, tc.Input), tc.Want)
				})
			}
		})
	}
	// Exact original schema/input/validity retained; the wrapper adds no value
	// type. Actual source check/errors AND this public wrapper were verified.
	for _, tc := range f.Dependency {
		t.Run("dependency/"+tc.Name, func(t *testing.T) {
			schema := json.RawMessage(`{"type":"object","properties":{"v":` + string(tc.Schema) + `}}`)
			input := json.RawMessage(`{"v":` + string(tc.Input) + `}`)
			got := regexOutcome(t, schema, input)
			if strings.HasPrefix(got, "ok ") != tc.Valid {
				t.Fatalf("got %s, valid=%v", got, tc.Valid)
			}
		})
	}
}
