package goai_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func mainEnumOutcome(t *testing.T, values []string, choice string) string {
	t.Helper()
	parameters, err := json.Marshal(map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{"choice": map[string]interface{}{"type": "string", "enum": values}},
		"required": []string{"choice"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tool := goai.Tool{Name: "enum", Parameters: parameters}
	args := map[string]interface{}{"choice": choice}
	got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: args})
	if args["choice"] != choice {
		t.Fatal("validation mutated caller arguments")
	}
	if err != nil {
		lines := strings.Split(err.Error(), "\n")
		if len(lines) > 1 && strings.HasPrefix(lines[0], "Validation failed") {
			return "REJECT " + strings.TrimSpace(lines[1])
		}
		return "THROW " + lines[0]
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	return "ok " + string(encoded)
}

// Both exact assertions from c9f4c7b9 validation-probes.test.ts:718-725.
func TestUpstreamMainValidationLargeEnum(t *testing.T) {
	values := make([]string, 10000)
	for i := range values {
		values[i] = fmt.Sprintf("member%d", i)
	}
	t.Run("last-member", func(t *testing.T) {
		if got := mainEnumOutcome(t, values, "member9999"); got != `ok {"choice":"member9999"}` {
			t.Fatal(got)
		}
	})
	t.Run("bounded-diagnostic", func(t *testing.T) {
		first, err := json.Marshal(values[:20])
		if err != nil {
			t.Fatal(err)
		}
		want := "REJECT - choice: expected one of " + string(first[:len(first)-1]) + ", ... and 9980 more]"
		if got := mainEnumOutcome(t, values, "x"); got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})
}

// Source-derived boundary controls for explain.ts listValues (limit 20).
func TestUpstreamMainValidationEnumDiagnosticBoundaries(t *testing.T) {
	for _, size := range []int{0, 1, 20, 21} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			values := make([]string, size)
			for i := range values {
				values[i] = fmt.Sprintf("v%d", i)
			}
			listed := values
			if size > 20 {
				listed = values[:20]
			}
			encoded, err := json.Marshal(listed)
			if err != nil {
				t.Fatal(err)
			}
			want := "REJECT - choice: expected one of " + string(encoded)
			if size > 20 {
				want = strings.TrimSuffix(want, "]") + ", ... and 1 more]"
			}
			if got := mainEnumOutcome(t, values, "missing"); got != want {
				t.Fatalf("got %s, want %s", got, want)
			}
		})
	}
	t.Run("JSON-string-escaping", func(t *testing.T) {
		values := []string{"<a&b>", "\u2028", `\u2029`, "quote\"slash\\"}
		want := "REJECT - choice: expected one of [\"<a&b>\",\"\u2028\",\"\\\\u2029\",\"quote\\\"slash\\\\\"]"
		if got := mainEnumOutcome(t, values, "missing"); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}
