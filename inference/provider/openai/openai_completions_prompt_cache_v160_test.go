package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

const promptCacheProxyV160 = "https://proxy.example.com/v1"

type promptCacheRequestV160 struct {
	payload map[string]any
	headers http.Header
}

func (r promptCacheRequestV160) field(t *testing.T, name, want string) {
	t.Helper()
	got, present := r.payload[name]
	if present != (want != "") || (present && got != want) {
		t.Errorf("payload %s = %v (present=%v), want %q (empty means absent)", name, got, present, want)
	}
}

func (r promptCacheRequestV160) header(t *testing.T, name, want string) {
	t.Helper()
	got, present := r.headers[http.CanonicalHeaderKey(name)]
	if present != (want != "") || (present && !reflect.DeepEqual(got, []string{want})) {
		t.Errorf("header %s = %q (present=%v), want %q (empty means absent)", name, got, present, want)
	}
}

func capturePromptCacheV160(t *testing.T, model *goai.Model, opts goai.StreamOptions) promptCacheRequestV160 {
	t.Helper()
	return captureCompletionsWireV160(t, model, promptCacheContext(), opts)
}

func captureCompletionsWireV160(t *testing.T, model *goai.Model, ctx *goai.Context, opts goai.StreamOptions) promptCacheRequestV160 {
	t.Helper()
	return captureCompletionsWireUsingV160(t, model, ctx, opts, goai.Stream)
}

func captureCompletionsWireUsingV160(t *testing.T, model *goai.Model, ctx *goai.Context, opts goai.StreamOptions, stream goai.ProviderStream) promptCacheRequestV160 {
	t.Helper()
	var got promptCacheRequestV160
	var requestCount int
	opts.APIKey = "test-key"
	opts.HTTPTransport = openaiTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		requestCount++
		defer r.Body.Close()
		wantURL := strings.TrimRight(model.BaseURL, "/") + "/chat/completions"
		if r.Method != http.MethodPost || r.URL.String() != wantURL {
			return nil, fmt.Errorf("request authority changed: %s %s, want POST %s", r.Method, r.URL, wantURL)
		}
		if err := json.NewDecoder(r.Body).Decode(&got.payload); err != nil {
			return nil, err
		}
		got.headers = r.Header.Clone()
		return successfulSSETestResponse(r, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"prompt_tokens_details\":{\"cached_tokens\":0},\"completion_tokens_details\":{\"reasoning_tokens\":0}}}\n\ndata: [DONE]\n\n"), nil
	})
	var completed int
	for e := range stream(t.Context(), model, ctx, &opts) {
		switch e := e.(type) {
		case *goai.ErrorEvent:
			t.Errorf("unexpected stream error: %v", e.Err)
		case *goai.DoneEvent:
			completed++
		}
	}
	if requestCount != 1 || got.payload == nil || completed != 1 {
		t.Fatalf("want one captured JSON request and one completion; got requests=%d, body=%v, completions=%d", requestCount, got.payload, completed)
	}
	return got
}

func promptCacheCatalogueV160(t *testing.T, provider goai.Provider, id string) *goai.Model {
	t.Helper()
	model := goai.GetModel(provider, id)
	if model == nil {
		t.Fatalf("missing model %s/%s", provider, id)
	}
	return model
}

// Mirrors createModel: the catalogue OpenAI record supplies the endpoint and
// model details, but not its Responses compat. Explicit compat is decoded from
// the same JSON keys as upstream; no test-only transport host alias is used.
func promptCacheModelV160(t *testing.T, provider goai.Provider, url, compat string) *goai.Model {
	t.Helper()
	model := *promptCacheCatalogueV160(t, goai.ProviderOpenAI, "gpt-4o-mini")
	model.Api = goai.ApiOpenAICompletions
	model.ResponsesCompat, model.CompletionsCompat = nil, nil
	if url != "" {
		model.BaseURL = url
	}
	if provider != "" {
		model.Provider = provider
	}
	if compat != "" {
		if err := json.Unmarshal([]byte(compat), &model.CompletionsCompat); err != nil {
			t.Fatal(err)
		}
	}
	return &model
}

// Source: pi v1.6.0 packages/ai/test/openai-completions-prompt-cache.test.ts:115-244.
func TestSourceV160CompletionsPromptCacheFields(t *testing.T) {
	goai.RegisterBuiltinModels()
	for _, tc := range []struct {
		name, url, compat, env, session string
		retention                       goai.CacheRetention
		key, ttl                        string
		checkTTL                        bool
	}{
		// L115, 122, 129, 136, 143.
		{name: "direct-default", session: "session-123", key: "session-123", checkTTL: true},
		{name: "direct-long", session: "session-456", retention: goai.CacheRetentionLong, key: "session-456", ttl: "24h", checkTTL: true},
		{name: "direct-ascii-clamp", session: strings.Repeat("x", 67), key: strings.Repeat("x", 64)},
		{name: "none", session: "session-789", retention: goai.CacheRetentionNone, checkTTL: true},
		{name: "proxy-long-unsupported", url: promptCacheProxyV160, compat: `{"supportsLongCacheRetention":false}`, session: "session-proxy", retention: goai.CacheRetentionLong, checkTTL: true},
		// L187, 207, 218, 228, 238. The intervening matrices are below.
		{name: "none-over-long-env", url: promptCacheProxyV160, compat: `{"promptCacheKeyMode":"enabled"}`, env: "long", session: "session-proxy", retention: goai.CacheRetentionNone, checkTTL: true},
		{name: "proxy-disabled-key-long-ttl", url: promptCacheProxyV160, compat: `{"promptCacheKeyMode":"disabled","supportsLongCacheRetention":true}`, session: "session-proxy", retention: goai.CacheRetentionLong, ttl: "24h", checkTTL: true},
		{name: "proxy-no-session", url: promptCacheProxyV160, compat: `{"promptCacheKeyMode":"enabled"}`},
		{name: "proxy-unicode-clamp", url: promptCacheProxyV160, compat: `{"promptCacheKeyMode":"enabled"}`, session: strings.Repeat("𐐀", 67), key: strings.Repeat("𐐀", 64)},
		{name: "direct-long-env", env: "long", session: "session-env", key: "session-env", ttl: "24h", checkTTL: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PI_CACHE_RETENTION", tc.env)
			model := promptCacheModelV160(t, "", tc.url, tc.compat)
			got := capturePromptCacheV160(t, model, goai.StreamOptions{SessionID: tc.session, CacheRetention: tc.retention})
			got.field(t, "prompt_cache_key", tc.key)
			if tc.checkTTL {
				got.field(t, "prompt_cache_retention", tc.ttl)
			}
		})
	}
	t.Setenv("PI_CACHE_RETENTION", "")
	// L154: retain the original first/repeated/other request sequence.
	for _, tc := range []struct {
		name      string
		retention goai.CacheRetention
	}{{"undefined", ""}, {"short", goai.CacheRetentionShort}, {"long", goai.CacheRetentionLong}} {
		t.Run("proxy-stable/"+tc.name, func(t *testing.T) {
			model := promptCacheModelV160(t, "", promptCacheProxyV160, `{"promptCacheKeyMode":"enabled","supportsLongCacheRetention":false}`)
			opts := goai.StreamOptions{SessionID: "session-proxy", CacheRetention: tc.retention}
			first := capturePromptCacheV160(t, model, opts)
			repeated := capturePromptCacheV160(t, model, opts)
			opts.SessionID = "other-session"
			other := capturePromptCacheV160(t, model, opts)
			first.field(t, "prompt_cache_key", "session-proxy")
			if !reflect.DeepEqual(repeated.payload["prompt_cache_key"], first.payload["prompt_cache_key"]) {
				t.Errorf("repeated key %v differs from first %v", repeated.payload["prompt_cache_key"], first.payload["prompt_cache_key"])
			}
			other.field(t, "prompt_cache_key", "other-session")
			first.field(t, "prompt_cache_retention", "")
			first.header(t, "session_id", "")
		})
	}
	// L174: each variant checks both default and explicitly long retention.
	for _, tc := range []struct{ name, compat string }{{"undefined", ""}, {"auto", `{"promptCacheKeyMode":"auto"}`}} {
		t.Run("proxy-default-mode/"+tc.name, func(t *testing.T) {
			model := promptCacheModelV160(t, "", promptCacheProxyV160, tc.compat)
			short := capturePromptCacheV160(t, model, goai.StreamOptions{SessionID: "session-proxy"})
			long := capturePromptCacheV160(t, model, goai.StreamOptions{SessionID: "session-proxy", CacheRetention: goai.CacheRetentionLong})
			short.field(t, "prompt_cache_key", "")
			long.field(t, "prompt_cache_key", "session-proxy")
			long.field(t, "prompt_cache_retention", "24h")
		})
	}
	// L199: disabling the key is independent of long retention.
	for _, tc := range []struct {
		retention goai.CacheRetention
		ttl       string
	}{{goai.CacheRetentionShort, ""}, {goai.CacheRetentionLong, "24h"}} {
		t.Run("direct-disabled-key/"+string(tc.retention), func(t *testing.T) {
			model := promptCacheModelV160(t, "", "", `{"promptCacheKeyMode":"disabled"}`)
			got := capturePromptCacheV160(t, model, goai.StreamOptions{SessionID: "session-disabled", CacheRetention: tc.retention})
			got.field(t, "prompt_cache_key", "")
			got.field(t, "prompt_cache_retention", tc.ttl)
		})
	}
}

// Source: pi v1.6.0 packages/ai/test/openai-completions-prompt-cache.test.ts:246-362.
func TestSourceV160CompletionsPromptCacheAffinity(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("PI_CACHE_RETENTION", "")
	const sendAffinity = `{"sendSessionAffinityHeaders":true}`
	for _, tc := range []struct {
		name, id, url, compat string
		provider              goai.Provider
		opts                  goai.StreamOptions
		fields, headers       map[string]string
	}{
		{name: "configured-default", url: promptCacheProxyV160, compat: sendAffinity, opts: goai.StreamOptions{SessionID: "session-affinity"},
			headers: map[string]string{"session_id": "session-affinity", "x-client-request-id": "session-affinity", "x-session-affinity": "session-affinity"}},
		{name: "fireworks-model", provider: "fireworks", id: "accounts/fireworks/models/glm-5p3", opts: goai.StreamOptions{SessionID: "fireworks-session"},
			headers: map[string]string{"x-session-affinity": "fireworks-session"}},
		{name: "fireworks-router", provider: "fireworks", id: "accounts/fireworks/routers/glm-5p3-fast", opts: goai.StreamOptions{SessionID: "fireworks-session"},
			headers: map[string]string{"x-session-affinity": "fireworks-session"}},
		{name: "baseten-catalogue", provider: "baseten", id: "zai-org/GLM-5.2", opts: goai.StreamOptions{SessionID: "baseten-catalog-session"},
			headers: map[string]string{"x-client-request-id": "baseten-catalog-session", "x-session-affinity": "baseten-catalog-session"}},
		{name: "openai-nosession", compat: `{"sendSessionAffinityHeaders":true,"sessionAffinityFormat":"openai-nosession"}`, opts: goai.StreamOptions{SessionID: "session-nosession"},
			fields:  map[string]string{"session_id": "", "prompt_cache_key": "session-nosession"},
			headers: map[string]string{"session_id": "", "x-session-id": "", "x-client-request-id": "session-nosession", "x-session-affinity": "session-nosession"}},
		{name: "openrouter-explicit", url: promptCacheProxyV160, compat: `{"sendSessionAffinityHeaders":true,"sessionAffinityFormat":"openrouter"}`, opts: goai.StreamOptions{SessionID: "session-proxy"},
			fields:  map[string]string{"session_id": "", "prompt_cache_key": ""},
			headers: map[string]string{"session_id": "", "x-session-id": "session-proxy", "x-client-request-id": "", "x-session-affinity": ""}},
		{name: "openrouter-catalogue", provider: "openrouter", id: "auto", opts: goai.StreamOptions{SessionID: "session-openrouter"},
			fields:  map[string]string{"session_id": "", "prompt_cache_key": ""},
			headers: map[string]string{"session_id": "", "x-session-id": "session-openrouter", "x-client-request-id": "", "x-session-affinity": ""}},
		{name: "openrouter-disabled", provider: "openrouter", url: "https://openrouter.ai/api/v1", compat: `{"sendSessionAffinityHeaders":false}`, opts: goai.StreamOptions{SessionID: "session-openrouter"},
			fields: map[string]string{"session_id": "", "prompt_cache_key": ""}, headers: map[string]string{"x-session-id": ""}},
		{name: "retention-none", url: promptCacheProxyV160, compat: sendAffinity, opts: goai.StreamOptions{SessionID: "session-affinity", CacheRetention: goai.CacheRetentionNone},
			headers: map[string]string{"session_id": "", "x-client-request-id": "", "x-session-affinity": ""}},
		{name: "header-overrides", url: promptCacheProxyV160, compat: sendAffinity,
			opts:    goai.StreamOptions{SessionID: "session-affinity", Headers: map[string]string{"session_id": "override-session", "x-client-request-id": "override-request", "x-session-affinity": "override-affinity"}},
			headers: map[string]string{"session_id": "override-session", "x-client-request-id": "override-request", "x-session-affinity": "override-affinity"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var model *goai.Model
			if tc.id != "" {
				model = promptCacheCatalogueV160(t, tc.provider, tc.id)
			} else {
				model = promptCacheModelV160(t, tc.provider, tc.url, tc.compat)
			}
			got := capturePromptCacheV160(t, model, tc.opts)
			for name, want := range tc.fields {
				got.field(t, name, want)
			}
			for name, want := range tc.headers {
				got.header(t, name, want)
			}
		})
	}
}

// Supplemental, not an assertion from the SDK request mock. A snapshot at the
// Go OnPayload hook is different evidence from a serialized HTTP request.
func TestCompletionsPromptCachePayloadHookStopsBeforeTransport(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("PI_CACHE_RETENTION", "")
	model := promptCacheModelV160(t, "", "", "")
	abort := errors.New("payload captured before HTTP")
	var hooks, dials, errorCount int
	opts := goai.StreamOptions{
		APIKey: "test-key", SessionID: "stop-hook",
		OnPayload: func(payload any, seenModel *goai.Model) (any, error) {
			hooks++
			body, ok := payload.(chatRequest)
			if !ok || body.PromptCacheKey != "stop-hook" || seenModel != model {
				return nil, fmt.Errorf("unexpected pre-HTTP payload %T or model", payload)
			}
			return nil, abort
		},
		HTTPTransport: openaiTestRoundTripper(func(*http.Request) (*http.Response, error) {
			dials++
			return nil, errors.New("unexpected HTTP request")
		}),
	}
	for e := range goai.Stream(t.Context(), model, promptCacheContext(), &opts) {
		if failure, ok := e.(*goai.ErrorEvent); ok && errors.Is(failure.Err, abort) && failure.Reason == goai.StopReasonError {
			errorCount++
		} else {
			t.Errorf("unexpected event after payload abort: %#v", e)
		}
	}
	if hooks != 1 || dials != 0 || errorCount != 1 {
		t.Fatalf("hook abort: hooks=%d, HTTP requests=%d, sentinel errors=%d; want 1, 0, 1", hooks, dials, errorCount)
	}
}

// Supplemental: the built-in OpenRouter record explicitly opts in. Also prove
// the default from detectCompat (not inferred from that record).
func TestCompletionsPromptCacheOpenRouterDetectionV160(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("PI_CACHE_RETENTION", "")
	model := promptCacheModelV160(t, "openrouter", "https://openrouter.ai/api/v1", "")
	got := capturePromptCacheV160(t, model, goai.StreamOptions{SessionID: "detected-router"})
	got.header(t, "x-session-id", "detected-router")
	got.header(t, "session_id", "")
	got.header(t, "x-client-request-id", "")
	got.header(t, "x-session-affinity", "")
}
