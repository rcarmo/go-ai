package openairesponses

import (
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"net/http"
	"reflect"
	"testing"
)

func TestResponsesHeaderCacheAndStructuredChoiceWireMain(t *testing.T) {
	for _, provider := range []goai.Provider{goai.ProviderOpenAI, goai.ProviderOpenRouter, goai.ProviderGitHubCopilot} {
		for _, retention := range []goai.CacheRetention{goai.CacheRetentionNone, goai.CacheRetentionShort, goai.CacheRetentionLong} {
			for _, mode := range []string{"default", "override", "suppress"} {
				t.Run(fmt.Sprintf("%s/%s/%s", provider, retention, mode), func(t *testing.T) {
					model := &goai.Model{ID: "gpt-test", Api: goai.ApiOpenAIResponses, Provider: provider, Headers: map[string]string{"X-Extra": "model", "X-Initiator": "model-initiator", "Openai-Intent": "model-intent"}}
					choice := map[string]any{"type": "function", "name": "lookup"}
					opts := &goai.StreamOptions{APIKey: "test", SessionID: "logical-session", CacheRetention: retention, ToolChoice: goai.ToolChoiceAuto, SamplingParams: map[string]any{"tool_choice": choice}}
					if mode == "override" {
						opts.Headers = map[string]string{"session_id": "caller-session", "x-session-id": "caller-router", "x-client-request-id": "caller-request", "X-Extra": "caller", "X-Initiator": "", "Openai-Intent": "caller-intent"}
					}
					if mode == "suppress" {
						opts.SuppressHeaders = []string{"session_id", "x-session-id", "x-client-request-id", "X-Initiator", "Openai-Intent"}
					}
					conv := &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}, Tools: []goai.Tool{deferredTool("lookup")}}
					beforeModel, _ := json.Marshal(model)
					beforeOptions, _ := json.Marshal(opts)
					beforeContext, _ := json.Marshal(conv)
					captured := captureResponsesRequest(t, model, opts, conv)
					headers := captured.Header
					if mode == "override" {
						for key, want := range opts.Headers {
							if values, ok := headers[http.CanonicalHeaderKey(key)]; !ok || len(values) != 1 || values[0] != want {
								t.Fatal("caller header override lost", key, values, want)
							}
						}
					} else if mode == "suppress" {
						for _, key := range opts.SuppressHeaders {
							if _, ok := headers[http.CanonicalHeaderKey(key)]; ok {
								t.Fatal("suppressed header regenerated", key, headers)
							}
						}
					} else {
						if provider == goai.ProviderOpenRouter {
							if headers.Get("x-session-id") != "logical-session" || headers.Get("session_id") != "" || headers.Get("x-client-request-id") != "" {
								t.Fatal("router affinity", headers)
							}
						} else if headers.Get("session_id") != "logical-session" || headers.Get("x-client-request-id") != "logical-session" {
							t.Fatal("affinity lost with retention", retention, headers)
						}
						initiator, intent := "model-initiator", "model-intent"
						if provider == goai.ProviderGitHubCopilot {
							initiator, intent = "user", "conversation-edits"
						}
						if headers.Get("X-Initiator") != initiator || headers.Get("Openai-Intent") != intent {
							t.Fatal("dynamic/model precedence", headers)
						}
					}
					if _, ok := captured.Body["prompt_cache_key"]; retention == goai.CacheRetentionNone {
						if ok {
							t.Fatal("none retention sent prompt cache key", captured.Body)
						}
					} else if captured.Body["prompt_cache_key"] != "logical-session" {
						t.Fatal("retained cache key", captured.Body)
					}
					if value, ok := captured.Body["prompt_cache_retention"]; retention == goai.CacheRetentionLong {
						if !ok || value != "24h" {
							t.Fatal("long retention", captured.Body)
						}
					} else if ok {
						t.Fatal("unexpected retention field", captured.Body)
					}
					if captured.Body["store"] != false || !reflect.DeepEqual(captured.Body["tool_choice"], choice) {
						t.Fatal("structured choice/store projection", captured.Body)
					}
					afterModel, _ := json.Marshal(model)
					afterOptions, _ := json.Marshal(opts)
					afterContext, _ := json.Marshal(conv)
					if string(beforeModel) != string(afterModel) || string(beforeOptions) != string(afterOptions) || string(beforeContext) != string(afterContext) {
						t.Fatal("header/cache construction mutated caller state")
					}
				})
			}
		}
	}
}
