package goai

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestIdentity140JSONAndMerging(t *testing.T) {
	var base, override ClientIdentity
	if err := json.Unmarshal([]byte(`{"userAgent":"short","version":"app-1","attribution":{"name":"tool","url":"https://example.com","categories":["cli"]}}`), &base); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"attribution":null}`), &override); err != nil {
		t.Fatal(err)
	}
	got := MergeClientIdentity(&base, &override)
	if got.Attribution != nil || !got.AttributionSet || FormatUserAgent(got) != "pi" {
		t.Fatalf("%+v", got)
	}
	data, err := json.Marshal(got)
	if err != nil || !strings.Contains(string(data), `"attribution":null`) {
		t.Fatal(string(data), err)
	}
	got.UserAgent.Mode = "detailed"
	*got.Version = "changed"
	if base.UserAgent.Mode != "short" || *base.Version != "app-1" {
		t.Fatal("identity aliases input")
	}
	copied := MergeClientIdentity(&base)
	copied.Attribution.Categories[0] = "changed"
	if base.Attribution.Categories[0] != "cli" {
		t.Fatal("attribution aliases input")
	}
	for _, wire := range []string{`"short"`, `"detailed"`, `{"custom":"custom-agent"}`, `{"custom":""}`} {
		var m UserAgentMode
		if err := json.Unmarshal([]byte(wire), &m); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(m)
		if err != nil || string(data) != wire {
			t.Fatal(wire, string(data), err)
		}
	}
	for _, wire := range []string{`"invalid"`, `{}`, `{"custom":3}`} {
		var m UserAgentMode
		if json.Unmarshal([]byte(wire), &m) == nil {
			t.Fatalf("accepted %s", wire)
		}
	}
	if !strings.HasPrefix(FormatUserAgent(nil), "pi/1.4.0 (") {
		t.Fatal(FormatUserAgent(nil))
	}
	if MergeClientIdentity(nil, nil) != nil {
		t.Fatal("absent sources created identity")
	}
}

func TestIdentity140AttributionTargets(t *testing.T) {
	identity := &ClientIdentity{Attribution: &ClientAttribution{Name: "tool", URL: "https://example.com", Categories: []string{"cli", "agent"}}}
	for _, tc := range []struct {
		target ClientIdentityTarget
		want   map[string]string
	}{
		{ClientIdentityTarget{Provider: "openrouter"}, map[string]string{"HTTP-Referer": "https://example.com", "X-OpenRouter-Title": "tool", "X-OpenRouter-Categories": "cli,agent"}},
		{ClientIdentityTarget{BaseURL: "https://openrouter.ai/api/v1"}, map[string]string{"HTTP-Referer": "https://example.com", "X-OpenRouter-Title": "tool", "X-OpenRouter-Categories": "cli,agent"}},
		{ClientIdentityTarget{BaseURL: "https://integrate.api.nvidia.com/v1"}, map[string]string{"X-BILLING-INVOKE-ORIGIN": "Tool"}},
		{ClientIdentityTarget{Provider: "nvidia"}, map[string]string{"X-BILLING-INVOKE-ORIGIN": "Tool"}},
		{ClientIdentityTarget{BaseURL: "https://opencode.ai/zen/v1"}, map[string]string{"x-opencode-client": "tool"}},
		{ClientIdentityTarget{Provider: "opencode-go"}, map[string]string{"x-opencode-client": "tool"}},
		{ClientIdentityTarget{BaseURL: "https://opencode.ai.evil.invalid"}, map[string]string{}},
		{ClientIdentityTarget{Provider: "openai"}, map[string]string{}},
	} {
		if got := GetAttributionHeaders(tc.target, identity); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%+v: %#v", tc.target, got)
		}
	}
	if h := GetAttributionHeaders(ClientIdentityTarget{Provider: "openrouter"}, nil); len(h) != 0 {
		t.Fatal(h)
	}
	if got := WithOpenCodeSessionHeader(ProviderOpenAI, "session", nil, "https://opencode.ai/zen"); got["x-opencode-session"] != "session" {
		t.Fatal(got)
	}
	if GetOpenCodeSessionHeaders(ClientIdentityTarget{BaseURL: "https://opencode.ai.evil.invalid"}, "session") != nil {
		t.Fatal("session leaked to lookalike host")
	}
}

func TestIdentity140ClassifierHeaderPrecedence(t *testing.T) {
	custom := "identity-agent"
	opts := &ClassifierOptions{APIKey: "test", Identity: &ClientIdentity{UserAgent: &UserAgentMode{Custom: &custom}, Attribution: &ClientAttribution{Name: "app", URL: "https://example.com"}}, Headers: map[string]string{"user-agent": "request-agent"}, SuppressHeaders: []string{"X-OpenRouter-Title"}}
	model := &ClassifierModel{Provider: ClassifierProviderOpenRouter, Headers: map[string]string{"User-Agent": "model-agent"}}
	h := http.Header{}
	applyClassifierHeaders(h, model, "test", opts)
	if h.Get("User-Agent") != "request-agent" || h.Get("X-OpenRouter-Title") != "" || h.Get("HTTP-Referer") != "https://example.com" {
		t.Fatal(h)
	}
	if opts.Headers["user-agent"] != "request-agent" || model.Headers["User-Agent"] != "model-agent" {
		t.Fatal("mutated header maps")
	}
}
