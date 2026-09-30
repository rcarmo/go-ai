package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func strictAnthropicModel() *goai.Model {
	return &goai.Model{
		ID:       "claude-opus-4-8",
		Provider: goai.ProviderAnthropic,
		Api:      goai.ApiAnthropicMessages,
		Input:    []string{"text"},
		AnthropicCompat: &goai.AnthropicMessagesCompat{
			ForceAdaptiveThinking:           boolPtrCompat(true),
			SupportsStrictTools:             boolPtrCompat(true),
			SupportsEagerToolInputStreaming: boolPtrCompat(true),
		},
	}
}

func strictLookupTool(parameters string, mode string) goai.Tool {
	var cs *goai.ToolConstrainedSampling
	if mode != "" {
		cs = &goai.ToolConstrainedSampling{Type: "json_schema", Strict: mode}
	}
	return goai.Tool{Name: "lookup", Description: "Look up a value", Parameters: json.RawMessage(parameters), ConstrainedSampling: cs}
}

func captureFirstStrictTool(t *testing.T, tool goai.Tool) map[string]any {
	t.Helper()
	request := captureAnthropicRequestForCompat(t, strictAnthropicModel(), lookupToolContext(tool), &goai.StreamOptions{APIKey: "test-key", CacheRetention: goai.CacheRetentionNone})
	return firstAnthropicTool(t, request.Body)
}

func TestAnthropicStrictToolSchemaSendsFullNormalizedSchema(t *testing.T) {
	legacyParams := `{"type":"object","title":"LookupInput","additionalProperties":false,"properties":{"value":{"type":"string"}},"required":["value"]}`
	legacyTool := captureFirstStrictTool(t, strictLookupTool(legacyParams, ""))
	if legacyTool["strict"] != nil {
		t.Fatalf("legacy strict=%#v, want omitted", legacyTool["strict"])
	}
	legacySchema := legacyTool["input_schema"].(map[string]any)
	if _, ok := legacySchema["additionalProperties"]; ok {
		t.Fatalf("legacy schema should omit full strict-only metadata: %#v", legacySchema)
	}
	if _, ok := legacySchema["title"]; ok {
		t.Fatalf("legacy schema should omit title: %#v", legacySchema)
	}

	strictParams := `{"type":"object","title":"StrictLookupInput","properties":{"value":{"type":"string"},"optional":{"type":"number"}},"required":["value"]}`
	strictTool := captureFirstStrictTool(t, strictLookupTool(strictParams, "prefer"))
	if strictTool["strict"] != true {
		t.Fatalf("strict=%#v, want true", strictTool["strict"])
	}
	schema := strictTool["input_schema"].(map[string]any)
	if schema["additionalProperties"] != false || schema["title"] != "StrictLookupInput" {
		t.Fatalf("unexpected strict schema metadata: %#v", schema)
	}
	required := schema["required"].([]any)
	if strings.Join([]string{required[0].(string), required[1].(string)}, ",") != "optional,value" {
		t.Fatalf("required=%#v, want sorted optional,value", required)
	}
	optional := schema["properties"].(map[string]any)["optional"].(map[string]any)
	if _, ok := optional["anyOf"].([]any); !ok {
		t.Fatalf("optional property was not widened to nullable anyOf: %#v", optional)
	}
}

func TestAnthropicStrictToolSchemaPreferFallsBackForRejectedKeywordClasses(t *testing.T) {
	cases := map[string]string{
		"minimum maximum":    `{"type":"object","properties":{"timeoutMs":{"type":"integer","minimum":1,"maximum":300000}}}`,
		"minItems above one": `{"type":"object","properties":{"tags":{"type":"array","items":{"type":"string"},"minItems":2}}}`,
		"unsupported format": `{"type":"object","properties":{"expression":{"type":"string","format":"regex"}}}`,
	}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			tool := captureFirstStrictTool(t, strictLookupTool(params, "prefer"))
			if tool["strict"] != nil {
				t.Fatalf("strict=%#v, want omitted for %#v", tool["strict"], tool)
			}
		})
	}
}

func TestAnthropicStrictToolSchemaRequireRejectsAnthropicUnsupportedKeywords(t *testing.T) {
	tool := strictLookupTool(`{"type":"object","properties":{"timeoutMs":{"type":"integer","minimum":1}}}`, "require")
	strict, err := goai.ResolveJSONSchemaStrictSamplingWithUnsupportedKeyword(tool, true, isAnthropicStrictUnsupportedKeyword)
	if err == nil || !strings.Contains(err.Error(), "minimum") || strict != nil {
		t.Fatalf("strict=%#v err=%v, want minimum rejection", strict, err)
	}
}

func TestAnthropicStrictToolSchemaKeepsEagerInputStreamingIndependent(t *testing.T) {
	tool := captureFirstStrictTool(t, strictLookupTool(`{"type":"object","properties":{"value":{"type":"string","format":"uri"}},"required":["value"]}`, "prefer"))
	if tool["strict"] != true {
		t.Fatalf("strict=%#v, want true", tool["strict"])
	}
	if tool["eager_input_streaming"] != true {
		t.Fatalf("eager_input_streaming=%#v, want true", tool["eager_input_streaming"])
	}
}
