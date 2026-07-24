package openairesponses

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestConstrainedSamplingConvertsStrictAndGrammarTools(t *testing.T) {
	strictTool := goai.Tool{Name: "strict_tool", Description: "strict", Parameters: json.RawMessage(`{"type":"object","properties":{"payload":{"type":"string"}},"required":["payload"]}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "prefer"}}
	strictDef, err := convertResponsesTool(strictTool, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if strictDef.Type != "function" || strictDef.Strict == nil || !*strictDef.Strict {
		t.Fatalf("strict tool=%#v", strictDef)
	}

	grammarTool := goai.Tool{Name: "grammar_tool", Description: "grammar", Parameters: json.RawMessage(`{"type":"object","properties":{"payload":{"type":"string"}},"required":["payload"]}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": "start: /[a-z]+/"}}}
	grammarDef, err := convertResponsesTool(grammarTool, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if grammarDef.Type != "custom" || grammarDef.Format["syntax"] != "lark" || grammarDef.Format["definition"] != "start: /[a-z]+/" || grammarDef.Parameters != nil {
		t.Fatalf("grammar tool=%#v", grammarDef)
	}
	fallback, err := convertResponsesTool(grammarTool, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if fallback.Type != "function" || fallback.Strict != nil || fallback.Format != nil {
		t.Fatalf("fallback tool=%#v", fallback)
	}
}

func TestConstrainedSamplingRejectsRequiredStrictOrInvalidGrammar(t *testing.T) {
	strictRequired := goai.Tool{Name: "strict_required", Parameters: json.RawMessage(`{"type":"object"}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "require"}}
	_, err := convertResponsesTool(strictRequired, false, true)
	if err == nil || !strings.Contains(err.Error(), "requires JSON-schema constrained sampling") {
		t.Fatalf("err=%v", err)
	}
	badGrammar := goai.Tool{Name: "bad_grammar", Parameters: json.RawMessage(`{"type":"object","properties":{"payload":{"type":"number"}},"required":["payload"]}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_regex": "[a-z]+"}}}
	_, err = convertResponsesTool(badGrammar, true, true)
	if err == nil || !strings.Contains(err.Error(), "must have type string") {
		t.Fatalf("err=%v", err)
	}
}
