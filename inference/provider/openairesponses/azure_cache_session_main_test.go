package openairesponses

import (
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"net/http"
	"reflect"
	"testing"
)

func TestResponsesAzureCacheSessionAPIBoundaryMain(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiOpenAIResponses, goai.ApiAzureOpenAIResponses} {
		for _, retention := range []goai.CacheRetention{goai.CacheRetentionNone, goai.CacheRetentionShort, goai.CacheRetentionLong} {
			for _, explicit := range []bool{false, true} {
				for _, mode := range []string{"default", "model", "caller", "suppress"} {
					t.Run(fmt.Sprintf("%s/%s/explicit-%v/%s", api, retention, explicit, mode), func(t *testing.T) {
						model := &goai.Model{ID: "gpt-test", Api: api, Provider: goai.ProviderOpenAI, ResponsesCompat: &goai.OpenAIResponsesCompat{SupportsExplicitPromptCacheMode: &explicit}}
						opts := &goai.StreamOptions{APIKey: "test-key", SessionID: "session", CacheRetention: retention}
						keys := []string{"session_id", "x-client-request-id", "x-ms-client-request-id"}
						if mode != "default" {
							model.Headers = map[string]string{}
							for _, key := range keys {
								model.Headers[key] = "model-ID"
							}
						}
						if mode == "caller" {
							opts.Headers = map[string]string{}
							for _, key := range keys {
								opts.Headers[key] = "caller-ID"
							}
						}
						if mode == "suppress" {
							opts.SuppressHeaders = keys
						}
						beforeModel, _ := json.Marshal(model)
						beforeOptions, _ := json.Marshal(opts)
						got := captureResponsesRequest(t, model, opts)
						for _, key := range keys {
							want := ""
							present := false
							if mode != "default" {
								want, present = "model-ID", true
							}
							if api == goai.ApiOpenAIResponses && key != "x-ms-client-request-id" {
								want, present = "session", true
							}
							if mode == "caller" {
								want, present = "caller-ID", true
							}
							if mode == "suppress" {
								want, present = "", false
							}
							_, exists := got.Header[http.CanonicalHeaderKey(key)]
							if exists != present || got.Header.Get(key) != want {
								t.Fatalf("%s=%q (present %v), want %q (%v)", key, got.Header.Get(key), exists, want, present)
							}
						}
						// Preserve native omission pending Rui's decision: pinned Azure sends a
						// session key even for cacheRetention none. Ordinary Responses omits it.
						if retention == goai.CacheRetentionNone {
							if _, ok := got.Body["prompt_cache_key"]; ok {
								t.Fatal("native cache-none omission lost", got.Body)
							}
						} else if got.Body["prompt_cache_key"] != "session" {
							t.Fatal("session cache key lost", got.Body)
						}
						wantRetention := ""
						var wantOptions any
						if api == goai.ApiOpenAIResponses {
							if explicit {
								if retention == goai.CacheRetentionNone {
									wantOptions = map[string]any{"mode": "explicit"}
								} else if retention == goai.CacheRetentionLong {
									wantOptions = map[string]any{"ttl": "30m"}
								}
							} else if retention == goai.CacheRetentionLong {
								wantRetention = "24h"
							}
						}
						// Also retain native explicit cache-none mode when capability-declared.
						if api == goai.ApiAzureOpenAIResponses && explicit && retention == goai.CacheRetentionNone {
							wantOptions = map[string]any{"mode": "explicit"}
						}
						value, present := got.Body["prompt_cache_retention"]
						if wantRetention == "" {
							if present {
								t.Fatal("unexpected retention", got.Body)
							}
						} else if value != wantRetention {
							t.Fatal("retention lost", got.Body)
						}
						value, present = got.Body["prompt_cache_options"]
						if wantOptions == nil {
							if present {
								t.Fatal("unexpected explicit cache options", got.Body)
							}
						} else if !reflect.DeepEqual(value, wantOptions) {
							t.Fatal("cache options lost", got.Body)
						}
						if got.Body["store"] != false {
							t.Fatal("store changed", got.Body)
						}
						afterModel, _ := json.Marshal(model)
						afterOptions, _ := json.Marshal(opts)
						if string(beforeModel) != string(afterModel) || string(beforeOptions) != string(afterOptions) {
							t.Fatal("caller state mutated")
						}
					})
				}
			}
		}
	}
}

func TestAzureExplicitSamplingCacheOverridesMain(t *testing.T) {
	model := &goai.Model{ID: "gpt-test", Api: goai.ApiAzureOpenAIResponses, Provider: goai.ProviderOpenAI}
	opts := &goai.StreamOptions{APIKey: "test-key", SessionID: "session", CacheRetention: goai.CacheRetentionNone, SamplingParams: map[string]any{"prompt_cache_key": "caller-key", "prompt_cache_retention": "24h", "prompt_cache_options": map[string]any{"ttl": "30m"}}}
	got := captureResponsesRequest(t, model, opts)
	for key, want := range opts.SamplingParams {
		if !reflect.DeepEqual(got.Body[key], want) {
			t.Fatal("explicit sampling override lost", key, got.Body)
		}
	}
}
