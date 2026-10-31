package bedrock

import (
	"encoding/json"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0842SanitizeBedrockDocumentOmitsEmptyObjectKeys(t *testing.T) {
	input := map[string]interface{}{
		"":       "omit",
		"keep":   "value",
		"nested": map[string]interface{}{"": "omit", "ok": true},
		"array":  []interface{}{map[string]interface{}{"": "omit", "ok": float64(1)}},
	}
	got := sanitizeBedrockDocumentValue(input)
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"array":[{"ok":1}],"keep":"value","nested":{"ok":true}}` {
		t.Fatalf("unexpected sanitized document: %s", data)
	}
}

func TestV0842BedrockStrictToolSchemaConversion(t *testing.T) {
	yes := true
	model := &goai.Model{ID: "anthropic.claude-sonnet-4-5", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream, BedrockCompat: &goai.BedrockCompat{SupportsStrictMode: &yes}}
	ctx := &goai.Context{Tools: []goai.Tool{{
		Name:                "lookup",
		Description:         "Lookup",
		Parameters:          json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"number"}},"required":["path"]}`),
		ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "prefer"},
	}}}
	input := buildConverseInput(model, ctx, &goai.StreamOptions{})
	if input.ToolConfig == nil || len(input.ToolConfig.Tools) != 1 {
		t.Fatalf("missing tool config: %#v", input.ToolConfig)
	}
	spec := input.ToolConfig.Tools[0].(*types.ToolMemberToolSpec).Value
	if spec.Strict == nil || !*spec.Strict {
		t.Fatal("emitted strict flag missing")
	}
	raw, err := spec.InputSchema.(*types.ToolInputSchemaMemberJson).Value.MarshalSmithyDocument()
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if schema["additionalProperties"] != false || len(schema["required"].([]any)) != 2 {
		t.Fatal("emitted schema not strict", schema)
	}
}
