package bedrock

import (
	"encoding/json"
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
	model := &goai.Model{ID: "anthropic.claude-sonnet-4-5", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream, ResponsesCompat: &goai.OpenAIResponsesCompat{SupportsStrictMode: &yes}}
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
	strict, err := goai.MakeStrictJSONSchema(ctx.Tools[0].Parameters)
	if err != nil {
		t.Fatal(err)
	}
	if len(strict) == 0 {
		t.Fatal("strict schema should be non-empty")
	}
}
