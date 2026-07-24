package openaicodex

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestCodexResponsesGrammarToolConvertsToCustomTool(t *testing.T) {
	yes := true
	model := &goai.Model{ID: "gpt-5.4-codex", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, ResponsesCompat: &goai.OpenAIResponsesCompat{SupportsOpenAIGrammarTools: &yes}}
	ctx := &goai.Context{Tools: []goai.Tool{{Name: "grammar", Parameters: json.RawMessage(`{"type":"object","properties":{"payload":{"type":"string"}},"required":["payload"]}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": "start: /[a-z]+/"}}}}}
	req := buildCodexRequest(model, ctx, &goai.StreamOptions{})
	if len(req.Tools) != 1 || req.Tools[0].Type != "custom" || req.Tools[0].Name != "grammar" || req.Tools[0].Format["syntax"] != "lark" || req.Tools[0].Parameters != nil {
		t.Fatalf("unexpected codex grammar tool: %#v", req.Tools)
	}
}
