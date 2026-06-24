package anthropic

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func cacheRetentionContext() *goai.Context {
	return &goai.Context{SystemPrompt: "You are a helpful assistant.", Messages: []goai.Message{goai.UserMessage("Hello")}, Tools: []goai.Tool{{Name: "read", Description: "Read", Parameters: json.RawMessage(`{"type":"object"}`)}}}
}

func cacheRetentionAnthropicModel(mut func(*goai.Model)) *goai.Model {
	m := &goai.Model{ID: "claude-haiku-4-5", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, BaseURL: "https://api.anthropic.com"}
	if mut != nil {
		mut(m)
	}
	return m
}

func firstSystemCacheControl(t *testing.T, req anthropicRequest) *cacheControl {
	t.Helper()
	var sys []struct {
		CacheControl *cacheControl `json:"cache_control"`
	}
	if err := json.Unmarshal(req.System, &sys); err != nil {
		t.Fatal(err)
	}
	if len(sys) == 0 {
		t.Fatal("missing system block")
	}
	return sys[0].CacheControl
}

func TestCacheRetentionAnthropicDefaultNoTTL(t *testing.T) {
	req := buildRequest(cacheRetentionAnthropicModel(nil), cacheRetentionContext(), nil)
	cc := firstSystemCacheControl(t, req)
	if cc == nil || cc.Type != "ephemeral" || cc.TTL != "" {
		t.Fatalf("cache_control=%#v", cc)
	}
}

func TestCacheRetentionAnthropicLongUsesOneHourTTL(t *testing.T) {
	req := buildRequest(cacheRetentionAnthropicModel(nil), cacheRetentionContext(), &goai.StreamOptions{CacheRetention: goai.CacheRetentionLong})
	cc := firstSystemCacheControl(t, req)
	if cc == nil || cc.Type != "ephemeral" || cc.TTL != "1h" {
		t.Fatalf("cache_control=%#v", cc)
	}
}

func TestCacheRetentionAnthropicOmitTTLWhenLongUnsupported(t *testing.T) {
	noLong := false
	req := buildRequest(cacheRetentionAnthropicModel(func(m *goai.Model) {
		m.AnthropicCompat = &goai.AnthropicMessagesCompat{SupportsLongCacheRetention: &noLong}
	}), cacheRetentionContext(), &goai.StreamOptions{CacheRetention: goai.CacheRetentionLong})
	cc := firstSystemCacheControl(t, req)
	if cc == nil || cc.Type != "ephemeral" || cc.TTL != "" {
		t.Fatalf("cache_control=%#v", cc)
	}
}

func TestCacheRetentionAnthropicNoneOmitsCacheControl(t *testing.T) {
	req := buildRequest(cacheRetentionAnthropicModel(nil), cacheRetentionContext(), &goai.StreamOptions{CacheRetention: goai.CacheRetentionNone})
	if cc := firstSystemCacheControl(t, req); cc != nil {
		t.Fatalf("cache_control=%#v, want nil", cc)
	}
}

func TestCacheRetentionAnthropicAddsCacheControlToLastUserMessageAndTool(t *testing.T) {
	req := buildRequest(cacheRetentionAnthropicModel(nil), cacheRetentionContext(), nil)
	last := req.Messages[len(req.Messages)-1]
	blocks, ok := last.Content.([]anthropicContentBlock)
	if !ok || len(blocks) == 0 || blocks[len(blocks)-1].CacheControl == nil || blocks[len(blocks)-1].CacheControl.Type != "ephemeral" {
		t.Fatalf("last message content=%#v", last.Content)
	}
	if len(req.Tools) != 1 || req.Tools[0].CacheControl == nil || req.Tools[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("tools=%#v", req.Tools)
	}
}
