package mistral

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0842MistralStrictSchemaUsesConvertedParameters(t *testing.T) {
	model := &goai.Model{ID: "mistral-large-latest", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}
	ctx := &goai.Context{Tools: []goai.Tool{{
		Name:                "lookup",
		Description:         "Lookup",
		Parameters:          json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"number"}},"required":["path"]}`),
		ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "prefer"},
	}}}
	req := buildRequest(model, ctx, &goai.StreamOptions{})
	if len(req.Tools) != 1 || !req.Tools[0].Function.Strict {
		t.Fatalf("strict tool not enabled: %#v", req.Tools)
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(req.Tools[0].Function.Parameters, &schema); err != nil {
		t.Fatal(err)
	}
	if schema["additionalProperties"] != false {
		t.Fatalf("schema not strict: %#v", schema)
	}
}
