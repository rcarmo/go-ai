package goai_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// The original onPayload sentinel stops before HTTP. For wire assertions the
// native stream sends the one serialized request to this non-dialing transport.
// It returns a permanent error so later events cannot require a real provider.
type cacheRetentionTransportV160 struct {
	requests int
	body     map[string]json.RawMessage
}

func (w *cacheRetentionTransportV160) RoundTrip(r *http.Request) (*http.Response, error) {
	w.requests++
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&w.body); err != nil {
		return nil, fmt.Errorf("cache retention request: %w", err)
	}
	return &http.Response{
		StatusCode: http.StatusTeapot,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"offline payload captured"}}`)),
		Request:    r,
	}, nil
}

func cacheRetentionWireV160(t *testing.T, model *goai.Model, ctx *goai.Context, opts goai.StreamOptions) map[string]json.RawMessage {
	t.Helper()
	w := &cacheRetentionTransportV160{}
	opts.HTTPTransport = w
	var terminal error
	for event := range goai.Stream(t.Context(), model, ctx, &opts) {
		if failed, ok := event.(*goai.ErrorEvent); ok {
			terminal = failed.Err
		}
	}
	if w.requests != 1 || w.body == nil {
		t.Fatalf("expected one captured request, got %d: %v", w.requests, terminal)
	}
	return w.body
}

func cacheRetentionOptionsV160(env string, retention goai.CacheRetention, session string) goai.StreamOptions {
	return goai.StreamOptions{
		Env:            goai.ProviderEnv{"PI_CACHE_RETENTION": env, "OPENAI_API_KEY": "sk-fake-key", "ANTHROPIC_API_KEY": "fake-key"},
		CacheRetention: retention, SessionID: session,
	}
}

func cacheRetentionContextV160() *goai.Context {
	return &goai.Context{
		SystemPrompt: "You are a helpful assistant.",
		Messages:     []goai.Message{goai.UserMessage("Hello")},
	}
}

// The empty expected string means absent, not null. Nonempty expectations
// compare whole JSON values so additional options, even null ones, fail.
func assertCacheRetentionFieldV160(t *testing.T, body map[string]json.RawMessage, key, want string) {
	t.Helper()
	got, exists := body[key]
	if want == "" {
		if exists {
			t.Errorf("%s = %s, want omitted", key, got)
		}
		return
	}
	if !exists {
		t.Errorf("%s omitted, want %s", key, want)
		return
	}
	if !reflect.DeepEqual(cacheRetentionDecodeV160[any](t, got), cacheRetentionDecodeV160[any](t, []byte(want))) {
		t.Errorf("%s = %s, want %s", key, got, want)
	}
}

func cacheRetentionDecodeV160[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("invalid cache retention payload %q: %v", raw, err)
	}
	return out
}

// Source: pi v1.6.0 packages/ai/test/cache-retention.test.ts:53,77,99,138,167,191,217.
func TestSourceV160CacheRetentionAnthropic(t *testing.T) {
	const ephemeral = `{"type":"ephemeral"}`
	const hour = `{"type":"ephemeral","ttl":"1h"}`
	for _, tc := range []struct {
		name, env, want string
		retention       goai.CacheRetention
		proxy, optOut   bool
		lastUserBlock   bool
	}{
		{name: "default_env", want: ephemeral},
		{name: "long_env", env: "long", want: hour},
		{name: "long_env_proxy", env: "long", proxy: true, want: hour},
		{name: "proxy_long_unsupported", retention: goai.CacheRetentionLong, proxy: true, optOut: true, want: ephemeral},
		{name: "none", retention: goai.CacheRetentionNone},
		{name: "last_user_block", lastUserBlock: true, want: ephemeral},
		{name: "long_option", retention: goai.CacheRetentionLong, want: hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := *requireModel(t, goai.ProviderAnthropic, "claude-haiku-4-5")
			if tc.proxy {
				model.BaseURL = "https://my-proxy.example.com/v1"
			}
			if tc.optOut {
				no := false
				model.AnthropicCompat = &goai.AnthropicMessagesCompat{SupportsLongCacheRetention: &no}
			}
			opts := cacheRetentionOptionsV160(tc.env, tc.retention, "")
			if tc.name != "default_env" && tc.name != "long_env" {
				opts.APIKey = "fake-key"
			}
			body := cacheRetentionWireV160(t, &model, cacheRetentionContextV160(), opts)
			if tc.lastUserBlock {
				msgs := cacheRetentionDecodeV160[[]map[string]json.RawMessage](t, body["messages"])
				if len(msgs) == 0 {
					t.Fatal("missing messages")
				}
				blocks := cacheRetentionDecodeV160[[]map[string]json.RawMessage](t, msgs[len(msgs)-1]["content"])
				if len(blocks) == 0 {
					t.Fatal("missing last message content blocks")
				}
				assertCacheRetentionFieldV160(t, blocks[len(blocks)-1], "cache_control", tc.want)
				return
			}
			system := cacheRetentionDecodeV160[[]map[string]json.RawMessage](t, body["system"])
			if len(system) == 0 || system[0] == nil {
				t.Fatal("missing first system content block")
			}
			assertCacheRetentionFieldV160(t, system[0], "cache_control", tc.want)
		})
	}
}

// Source: pi v1.6.0 packages/ai/test/cache-retention.test.ts:257,279,302,334,362,389,415-443.
func TestSourceV160CacheRetentionResponses(t *testing.T) {
	for _, tc := range []struct {
		name, id, env, session string
		retention              goai.CacheRetention
		proxy, optOut          bool
		fields                 map[string]string
	}{
		{name: "default_env", id: "gpt-4o-mini", fields: map[string]string{"prompt_cache_retention": ""}},
		{name: "long_env", id: "gpt-4o-mini", env: "long", fields: map[string]string{"prompt_cache_retention": `"24h"`}},
		{name: "long_env_proxy", id: "gpt-4o-mini", env: "long", proxy: true, fields: map[string]string{"prompt_cache_retention": `"24h"`}},
		{name: "long_unsupported", id: "gpt-4o-mini", retention: goai.CacheRetentionLong, optOut: true, session: "session-compat-false", fields: map[string]string{"prompt_cache_retention": ""}},
		{name: "none_explicit", id: "gpt-5.6-sol", retention: goai.CacheRetentionNone, session: "session-1", fields: map[string]string{
			"prompt_cache_key": "", "prompt_cache_retention": "", "prompt_cache_options": `{"mode":"explicit"}`,
		}},
		{name: "none_unsupported", id: "gpt-4o-mini", retention: goai.CacheRetentionNone, session: "session-1", fields: map[string]string{
			"prompt_cache_key": "", "prompt_cache_options": "",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := *requireModel(t, goai.ProviderOpenAI, tc.id)
			if tc.proxy {
				model.BaseURL = "https://my-proxy.example.com/v1"
			}
			if tc.optOut {
				no := false
				model.ResponsesCompat = &goai.OpenAIResponsesCompat{SupportsLongCacheRetention: &no}
			}
			opts := cacheRetentionOptionsV160(tc.env, tc.retention, tc.session)
			if tc.name != "default_env" && tc.name != "long_env" {
				opts.APIKey = "sk-fake-key"
			}
			body := cacheRetentionWireV160(t, &model, cacheRetentionContextV160(), opts)
			for key, want := range tc.fields {
				assertCacheRetentionFieldV160(t, body, key, want)
			}
		})
	}
	for _, tc := range []struct{ id, retention, options string }{
		{"gpt-4o-mini", `"24h"`, ""},
		{"gpt-6-astra", "", `{"ttl":"30m"}`},
		{"gpt-6-sol", "", `{"ttl":"30m"}`},
		{"gpt-6-luna", "", `{"ttl":"30m"}`},
	} {
		t.Run(tc.id, func(t *testing.T) {
			model := requireModel(t, goai.ProviderOpenAI, tc.id)
			opts := cacheRetentionOptionsV160("", goai.CacheRetentionLong, "session-2")
			opts.APIKey = "sk-fake-key"
			body := cacheRetentionWireV160(t, model, cacheRetentionContextV160(), opts)
			assertCacheRetentionFieldV160(t, body, "prompt_cache_key", `"session-2"`)
			assertCacheRetentionFieldV160(t, body, "prompt_cache_retention", tc.retention)
			assertCacheRetentionFieldV160(t, body, "prompt_cache_options", tc.options)
		})
	}
}

// Source: pi v1.6.0 packages/ai/test/cache-retention.test.ts:464,489,514-545,547-610.
func TestSourceV160CacheRetentionCompletions(t *testing.T) {
	for _, tc := range []struct {
		name, session, key, retention string
		optOut                        bool
	}{
		{"proxy_default", "session-completions", `"session-completions"`, `"24h"`, false},
		{"proxy_long_unsupported", "session-completions-false", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &goai.Model{
				ID: "test-model", Name: "Test Model", Api: goai.ApiOpenAICompletions, Provider: "test-openai-completions",
				BaseURL: "https://my-proxy.example.com/v1", Input: []string{"text"}, ContextWindow: 128000, MaxTokens: 4096,
			}
			if tc.optOut {
				no := false
				model.CompletionsCompat = &goai.OpenAICompletionsCompat{SupportsLongCacheRetention: &no}
			}
			opts := cacheRetentionOptionsV160("", goai.CacheRetentionLong, tc.session)
			opts.APIKey = "fake-key"
			body := cacheRetentionWireV160(t, model, cacheRetentionContextV160(), opts)
			assertCacheRetentionFieldV160(t, body, "prompt_cache_key", tc.key)
			assertCacheRetentionFieldV160(t, body, "prompt_cache_retention", tc.retention)
		})
	}
	for _, id := range []string{"deepseek-v4.1-flash", "deepseek-v4-pro", "kimi-k2.5", "kimi-k2.6", "minimax-m2.7"} {
		t.Run("opencode/"+id, func(t *testing.T) {
			model := requireModel(t, "opencode", id)
			if compat := model.CompletionsCompat; compat == nil || compat.SupportsLongCacheRetention == nil || *compat.SupportsLongCacheRetention {
				t.Fatalf("supportsLongCacheRetention must be explicitly false, got %+v", compat)
			}
			opts := cacheRetentionOptionsV160("", goai.CacheRetentionLong, "session-opencode-long-cache-unsupported")
			opts.APIKey = "fake-key"
			body := cacheRetentionWireV160(t, model, cacheRetentionContextV160(), opts)
			assertCacheRetentionFieldV160(t, body, "prompt_cache_key", "")
			assertCacheRetentionFieldV160(t, body, "prompt_cache_retention", "")
		})
	}
	for _, id := range []string{"gpt-oss-120b", "qwen-3.8-27b"} {
		t.Run("cerebras/"+id, func(t *testing.T) {
			model := requireModel(t, goai.ProviderCerebras, id)
			if compat := model.CompletionsCompat; compat != nil && compat.SupportsStrictMode != nil {
				t.Fatalf("catalogue supportsStrictMode must be absent, got %t", *compat.SupportsStrictMode)
			}
			initial := goai.CreateInitialSystemMessage("test", []goai.Tool{
				{Name: "t1", Description: "strict tool", Parameters: json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}},"required":["x"]}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema"}},
				{Name: "t2", Description: "non-strict tool", Parameters: json.RawMessage(`{"type":"object","properties":{"y":{"type":"string"}},"required":["y"]}`)},
			})
			user := goai.UserMessage("hello")
			user.Timestamp = 1
			ctx := goai.ContextFromTranscript(goai.TranscriptContext{Messages: []goai.Message{*initial, user}})
			opts := cacheRetentionOptionsV160("", "", "test")
			opts.APIKey = "fake-key"
			body := cacheRetentionWireV160(t, model, ctx, opts)
			tools := cacheRetentionDecodeV160[[]map[string]json.RawMessage](t, body["tools"])
			if len(tools) != 2 {
				t.Fatalf("expected both declared tools, got %s", body["tools"])
			}
			for i, tool := range tools {
				function := cacheRetentionDecodeV160[map[string]json.RawMessage](t, tool["function"])
				assertCacheRetentionFieldV160(t, function, "name", fmt.Sprintf(`"t%d"`, i+1))
				assertCacheRetentionFieldV160(t, function, "strict", "")
			}
		})
	}
}
