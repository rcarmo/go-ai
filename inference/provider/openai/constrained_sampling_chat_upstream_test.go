package openai

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestOpenAICompletionsGrammarToolConvertsToCustomTool(t *testing.T) {
	yes := true
	defs := convertToolDefs([]goai.Tool{{Name: "grammar", Parameters: json.RawMessage(`{"type":"object","properties":{"payload":{"type":"string"}},"required":["payload"]}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_regex": "[a-z]+"}}}}, goai.OpenAICompletionsCompat{SupportsOpenAIGrammarTools: &yes}, nil)
	if len(defs) != 1 || defs[0].Type != "custom" || defs[0].Name != "grammar" || defs[0].Format["syntax"] != "regex" || defs[0].Function != nil {
		t.Fatalf("unexpected grammar tool def: %#v", defs)
	}
}
