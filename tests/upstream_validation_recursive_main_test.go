package goai_test

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Exact schema, depth, input and outcome from c9f4c7b9
// packages/ai/test/validation-probes.test.ts:767-778.
func TestUpstreamMainValidationDeepRecursiveUnion(t *testing.T) {
	tool := goai.Tool{Name: "deep", Parameters: json.RawMessage(`{"type":"object","properties":{"s":{"$ref":"#/$defs/S"}},"$defs":{"S":{"anyOf":[{"type":"object","additionalProperties":{"$ref":"#/$defs/S"},"required":["a"]},{"type":"object","additionalProperties":{"$ref":"#/$defs/S"},"required":["b"]}]}}}`)}
	var value interface{} = map[string]interface{}{"x": 1}
	for i := 0; i < 40; i++ {
		value = map[string]interface{}{"c": value}
	}
	_, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: map[string]interface{}{"s": value}})
	outcome := "accepted"
	if err != nil {
		lines := strings.Split(err.Error(), "\n")
		if len(lines) > 1 && strings.HasPrefix(err.Error(), "Validation failed") {
			outcome = "REJECT " + strings.TrimSpace(lines[1])
		} else {
			outcome = "THROW " + lines[0]
		}
	}
	if outcome != "REJECT - s.a: missing" {
		t.Fatalf("source expects REJECT - s.a: missing, got %s", outcome)
	}
}
