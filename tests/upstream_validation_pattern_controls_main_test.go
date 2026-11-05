package goai_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestMainValidationStringPatternSourceControls(t *testing.T) {
	for _, tc := range []struct {
		name, schema, input, want string
	}{
		{"astral", `{"type":"object","properties":{"s":{"type":"string","pattern":"^.$"}}}`, `{"s":"😀"}`, `ok {"s":"😀"}`},
		{"class", `{"type":"object","properties":{"s":{"type":"string","pattern":"^[a-z-]+$"}}}`, `{"s":"a-b"}`, `ok {"s":"a-b"}`},
		{"lookbehind-pass", `{"type":"object","properties":{"s":{"type":"string","pattern":"(?<!x)y"}}}`, `{"s":"y"}`, `ok {"s":"y"}`},
		{"lookbehind-reject", `{"type":"object","properties":{"s":{"type":"string","pattern":"(?<!x)y"}}}`, `{"s":"xy"}`, `REJECT - s: expected to match (?<!x)y`},
		{"legacy-identity-escape", `{"type":"object","properties":{"s":{"type":"string","pattern":"^\\a$"}}}`, `{"s":"a"}`, `ok {"s":"a"}`},
		{"legacy-identity-reject", `{"type":"object","properties":{"s":{"type":"string","pattern":"^\\a$"}}}`, `{"s":"b"}`, `REJECT - s: expected to match ^\a$`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := mainNumericOutcome(t, tc.schema, tc.input); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestMainValidationStringPatternUnionAndOwnership(t *testing.T) {
	schema := `{"type":"object","properties":{"s":{"oneOf":[{"type":"string","pattern":"^a+$"},{"type":"string","pattern":"^b+$"}]}}}`
	for _, tc := range []struct{ input, prefix string }{
		{`{"s":"aaa"}`, "ok "},
		{`{"s":"bbb"}`, "ok "},
		{`{"s":"abc"}`, "REJECT "},
	} {
		if got := mainNumericOutcome(t, schema, tc.input); !strings.HasPrefix(got, tc.prefix) {
			t.Errorf("input=%s got=%q", tc.input, got)
		}
	}
	tool := goai.Tool{Name: "pattern", Parameters: json.RawMessage(`{"type":"object","properties":{"nested":{"type":"object","properties":{"s":{"type":"string","pattern":"^a+$"},"n":{"type":"integer"}}}}}`)}
	args := map[string]interface{}{"nested": map[string]interface{}{"s": "aaa", "n": nil}}
	_, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: args})
	if err != nil || !reflect.DeepEqual(args, map[string]interface{}{"nested": map[string]interface{}{"s": "aaa", "n": nil}}) {
		t.Fatalf("input changed or validation failed: args=%#v error=%v", args, err)
	}
}
