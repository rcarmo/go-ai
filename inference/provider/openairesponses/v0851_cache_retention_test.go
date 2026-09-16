package openairesponses

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0851ResponsesExplicitPromptCacheOptionsSerialization(t *testing.T) {
	for _, tc := range []struct {
		name          string
		retention     goai.CacheRetention
		longSupported bool
		wantOptions   map[string]interface{}
		wantKey       bool
	}{
		{"none disables implicit cache with explicit mode", goai.CacheRetentionNone, true, map[string]interface{}{"mode": "explicit"}, false},
		{"long uses thirty minute ttl", goai.CacheRetentionLong, true, map[string]interface{}{"ttl": "30m"}, true},
		{"short omits explicit options", goai.CacheRetentionShort, true, nil, true},
		{"default short omits explicit options", "", true, nil, true},
		{"long without long support omits options", goai.CacheRetentionLong, false, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := v0851ResponsesCacheModel(true, tc.longSupported)
			opts := &goai.StreamOptions{APIKey: "key", SessionID: "session-2", CacheRetention: tc.retention}
			payload := marshalV0851ResponsesRequest(t, buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, opts))
			if _, ok := payload["prompt_cache_retention"]; ok {
				t.Fatalf("prompt_cache_retention emitted for explicit-cache model: %#v", payload)
			}
			gotOptions, hasOptions := payload["prompt_cache_options"]
			if tc.wantOptions == nil {
				if hasOptions {
					t.Fatalf("prompt_cache_options=%#v, want omitted", gotOptions)
				}
			} else if !hasOptions {
				t.Fatalf("prompt_cache_options omitted, want %#v; payload=%#v", tc.wantOptions, payload)
			} else {
				got, ok := gotOptions.(map[string]interface{})
				if !ok {
					t.Fatalf("prompt_cache_options type=%T", gotOptions)
				}
				for key, want := range tc.wantOptions {
					if got[key] != want {
						t.Fatalf("prompt_cache_options[%s]=%#v want %#v payload=%#v", key, got[key], want, payload)
					}
				}
			}
			_, hasKey := payload["prompt_cache_key"]
			if hasKey != tc.wantKey {
				t.Fatalf("prompt_cache_key present=%v want %v payload=%#v", hasKey, tc.wantKey, payload)
			}
		})
	}
}

func TestV0851ResponsesLegacyPromptCacheRetentionStillUses24h(t *testing.T) {
	model := v0851ResponsesCacheModel(false, true)
	payload := marshalV0851ResponsesRequest(t, buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{APIKey: "key", SessionID: "session-1", CacheRetention: goai.CacheRetentionLong}))
	if got := payload["prompt_cache_retention"]; got != "24h" {
		t.Fatalf("prompt_cache_retention=%#v want 24h payload=%#v", got, payload)
	}
	if _, ok := payload["prompt_cache_options"]; ok {
		t.Fatalf("prompt_cache_options emitted for legacy model: %#v", payload)
	}
}

func v0851ResponsesCacheModel(explicitCache, longRetention bool) *goai.Model {
	return &goai.Model{
		ID:            "gpt-6-astra",
		Name:          "GPT-6 Astra",
		Api:           goai.ApiOpenAIResponses,
		Provider:      goai.ProviderOpenAI,
		BaseURL:       "https://api.openai.com/v1",
		Reasoning:     true,
		Input:         []string{"text", "image"},
		Cost:          goai.ModelCost{Input: 10, Output: 50, CacheRead: 1, CacheWrite: 12.5},
		ContextWindow: 272000,
		MaxTokens:     128000,
		ResponsesCompat: &goai.OpenAIResponsesCompat{
			SupportsExplicitPromptCacheMode: boolPtrV0851(explicitCache),
			SupportsLongCacheRetention:      boolPtrV0851(longRetention),
		},
	}
}

func marshalV0851ResponsesRequest(t *testing.T, req responsesRequest) map[string]interface{} {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func boolPtrV0851(v bool) *bool { return &v }
