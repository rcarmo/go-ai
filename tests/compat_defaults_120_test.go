package goai_test

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestCompat120PinnedAPIDefaultsStayOutOfProviderSuperset(t *testing.T) {
	properties := func(name string) map[string]any {
		raw, err := goai.ModelSchema(name)
		if err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		return schema["properties"].(map[string]any)
	}
	for _, tc := range []struct {
		schema, key string
		want        bool
	}{{"OpenAIResponsesCompatSchema", "supportsDeveloperRole", true}, {"AnthropicMessagesCompatSchema", "supportsLongCacheRetention", true}, {"BedrockCompatSchema", "supportsStrictMode", false}} {
		props := properties(tc.schema)
		if props[tc.key].(map[string]any)["default"] != tc.want {
			t.Fatal("API default lost", tc)
		}
	}
	for _, tc := range []struct {
		schema string
		keys   []string
	}{{"ProviderCompatSchema", []string{"supportsDeveloperRole", "sendSessionAffinityHeaders", "supportsLongCacheRetention", "supportsStrictMode"}}, {"OpenAICompletionsCompatSchema", []string{"thinkingFormat", "supportsLongCacheRetention"}}, {"AnthropicMessagesCompatSchema", []string{"sendSessionAffinityHeaders"}}} {
		props := properties(tc.schema)
		for _, key := range tc.keys {
			if field, ok := props[key].(map[string]any); ok {
				if _, ok := field["default"]; ok {
					t.Fatal("unexpected compatibility default", tc.schema, key)
				}
			}
		}
	}
}
