package openaicodex

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0842CodexStrictSchemaUsesConvertedParameters(t *testing.T) {
	model := &goai.Model{ID: "gpt-5.4-codex", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses}
	ctx := &goai.Context{Tools: []goai.Tool{{
		Name:                "lookup",
		Description:         "Lookup",
		Parameters:          json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"number"}},"required":["path"]}`),
		ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "prefer"},
	}}}
	req := buildCodexRequest(model, ctx, &goai.StreamOptions{})
	if len(req.Tools) != 1 || req.Tools[0].Strict == nil || !*req.Tools[0].Strict {
		t.Fatalf("strict tool not enabled: %#v", req.Tools)
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(req.Tools[0].Parameters, &schema); err != nil {
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
