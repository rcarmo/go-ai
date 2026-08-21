package openai

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0842OpenAICompletionsStrictSchemaUsesConvertedParameters(t *testing.T) {
	yes := true
	defs := convertToolDefs([]goai.Tool{{
		Name:                "lookup",
		Description:         "Lookup",
		Parameters:          json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"number"}},"required":["path"]}`),
		ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "prefer"},
	}}, goai.OpenAICompletionsCompat{SupportsStrictMode: &yes}, nil)
	if len(defs) != 1 || defs[0].Function == nil || !defs[0].Function.Strict {
		t.Fatalf("strict tool not enabled: %#v", defs)
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(defs[0].Function.Parameters, &schema); err != nil {
		t.Fatal(err)
	}
	if schema["additionalProperties"] != false {
		t.Fatalf("schema not strict: %#v", schema)
	}
	props := schema["properties"].(map[string]interface{})
	if _, ok := props["offset"].(map[string]interface{})["anyOf"]; !ok {
		t.Fatalf("optional offset not widened: %#v", props["offset"])
	}
}
