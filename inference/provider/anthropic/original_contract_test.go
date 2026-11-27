package anthropic

import (
	"context"
	"io"
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Pinned source locators: packages/ai/test/anthropic-auth-token.test.ts:79-178.
func TestAnthropicOriginalAuthTokenAndOAuthRequestShaping(t *testing.T) {
	zero := 0
	for _, tc := range []struct {
		name          string
		env           goai.ProviderEnv
		headers       map[string]string
		wantAuth      string
		wantOAuthBeta bool
	}{
		{name: "auth token", env: goai.ProviderEnv{"ANTHROPIC_AUTH_TOKEN": "gateway-token"}, wantAuth: "Bearer gateway-token"},
		{name: "oauth token", env: goai.ProviderEnv{"ANTHROPIC_OAUTH_TOKEN": "sk-ant-oat-test"}, wantAuth: "Bearer sk-ant-oat-test", wantOAuthBeta: true},
		{name: "explicit header", env: goai.ProviderEnv{"ANTHROPIC_AUTH_TOKEN": "gateway-token"}, headers: map[string]string{"Authorization": "Bearer explicit-token"}, wantAuth: "Bearer explicit-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var authorization, beta, userAgent, xApp string
			var system any
			model := federationModel("https://api.anthropic.test/v1")
			result, err := goai.Complete(t.Context(), model, &goai.Context{SystemPrompt: "System prompt.", Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{
				Env: tc.env, Headers: tc.headers, CacheRetention: goai.CacheRetentionNone, MaxRetries: &zero,
				HTTPTransport: anthropicThreadTestTransport(func(r *http.Request) (*http.Response, error) {
					authorization = r.Header.Get("Authorization")
					beta = r.Header.Get("Anthropic-Beta")
					userAgent = r.Header.Get("User-Agent")
					xApp = r.Header.Get("x-app")
					body, err := io.ReadAll(r.Body)
					if err != nil {
						return nil, err
					}
					system = string(body)
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(anthropicThreadReplyV160("msg_1", false)))}, nil
				}),
			})
			if err != nil || result == nil || result.StopReason != goai.StopReasonStop {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			if authorization != tc.wantAuth || !strings.Contains(system.(string), `"system":[`) {
				t.Fatalf("authorization=%q body=%s", authorization, system)
			}
			if got := strings.Contains(beta, anthropicFederationOAuthBeta); got != tc.wantOAuthBeta {
				t.Fatalf("oauth beta=%t in %q", got, beta)
			}
			if tc.wantOAuthBeta && (userAgent != "claude-cli/"+claudeCodeVersion || xApp != "cli") {
				t.Fatalf("oauth request headers User-Agent=%q x-app=%q", userAgent, xApp)
			}
		})
	}
}

// Pinned source locator: packages/ai/test/anthropic-cache-write-1h-cost.test.ts:70-96.
func TestAnthropicOriginalCacheWriteBreakdownFromDelta(t *testing.T) {
	start := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n"
	delta := "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"input_tokens\":3,\"output_tokens\":4,\"cache_creation_input_tokens\":6535,\"cache_creation\":{\"ephemeral_5m_input_tokens\":0,\"ephemeral_1h_input_tokens\":6535}}}\n\n"
	body := io.NopCloser(strings.NewReader(start + delta + "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	ch := make(chan goai.Event, 16)
	model := &goai.Model{ID: "anthropic/claude-haiku-4.5", Provider: goai.ProviderVercelAIGateway, Api: goai.ApiAnthropicMessages, Cost: goai.ModelCost{Input: 1.25}}
	processAnthropicStream(body, model, nil, "", goai.NewAssistantEventSender(ch))
	close(ch)
	for event := range ch {
		message, ok := event.(*goai.DoneEvent)
		if !ok {
			continue
		}
		if message.Message.Usage.CacheWrite != 6535 || message.Message.Usage.CacheWrite1h != 6535 {
			t.Fatalf("cache usage=%#v", message.Message.Usage)
		}
		want := 6535 * model.Cost.Input * 2 / 1_000_000
		if math.Abs(message.Message.Usage.Cost.CacheWrite-want) > 1e-12 {
			t.Fatalf("cache cost=%f want %f", message.Message.Usage.Cost.CacheWrite, want)
		}
		return
	}
	t.Fatal("missing done event")
}

// Pinned source locators: packages/ai/test/anthropic-empty-thinking-signature-compat.test.ts:69-103.
func TestAnthropicOriginalEmptyThinkingSignatureCatalogAndReplay(t *testing.T) {
	payload := buildRequest(makeEmptySignatureModel(nil), makeEmptySignatureContext("signed-thinking"), &goai.StreamOptions{})
	assistant := firstAssistantContent(t, payload)
	if len(assistant) != 1 || assistant[0].Type != "thinking" || assistant[0].Signature != "signed-thinking" {
		t.Fatalf("signed empty-thinking content=%#v", assistant)
	}
	goai.RegisterBuiltinModels()
	models := goai.ListModels(goai.ProviderVercelAIGateway)
	if len(models) == 0 {
		t.Fatal("no Vercel AI Gateway models")
	}
	for _, model := range models {
		if model.AnthropicCompat == nil || model.AnthropicCompat.AllowEmptySignature == nil || !*model.AnthropicCompat.AllowEmptySignature {
			t.Fatalf("Vercel model %s does not allow empty signatures", model.ID)
		}
	}
	for _, id := range []string{"accounts/fireworks/models/deepseek-v4p1-flash", "accounts/fireworks/models/qwen3p8-max", "accounts/fireworks/models/qwen3p8-2p4t-a95b", "accounts/fireworks/models/ember-1"} {
		model := goai.GetModel(goai.ProviderFireworks, id)
		if model == nil || model.AnthropicCompat == nil || model.AnthropicCompat.AllowEmptySignature == nil || !*model.AnthropicCompat.AllowEmptySignature {
			t.Fatalf("Fireworks model %s lacks empty-signature compatibility", id)
		}
	}
}

// Pinned source locators: packages/ai/test/anthropic-error-tool-result-images.test.ts:69-136.
func TestAnthropicOriginalErrorToolResultWithoutImagesIsUnchanged(t *testing.T) {
	model := opusDeferredModel()
	ctx := deferredContext([]goai.Tool{deferredTool("base_tool")}, nil)
	ctx.Messages[2].IsError = true
	ctx.Messages[2].Content = []goai.ContentBlock{{Type: "text", Text: "failed"}}
	request := buildRequest(model, ctx, &goai.StreamOptions{})
	blocks := request.Messages[2].Content.([]anthropicContentBlock)
	want := []anthropicContentBlock{{Type: "tool_result", ToolUseID: "call_1", Content: "failed", IsError: boolPtrCompat(true)}}
	if !reflect.DeepEqual(blocks, want) {
		t.Fatalf("content=%#v want %#v", blocks, want)
	}
}

// Pinned source locators: packages/ai/test/abort.test.ts:35-88,93-114.
func TestAnthropicOriginalImmediateAbortCanBeReplacedByNewMessage(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	model := federationModel("https://api.anthropic.test/v1")
	conv := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
	aborted, err := goai.Complete(ctx, model, conv, &goai.StreamOptions{APIKey: "test-key"})
	if err == nil || aborted == nil || aborted.StopReason != goai.StopReasonAborted || len(aborted.Content) != 0 {
		t.Fatalf("aborted=%#v err=%v", aborted, err)
	}
	conv.Messages = append(conv.Messages, *aborted, goai.UserMessage("What is 2+2?"))
	requests := 0
	zero := 0
	followUp, err := goai.Complete(t.Context(), model, conv, &goai.StreamOptions{APIKey: "test-key", MaxRetries: &zero, HTTPTransport: anthropicThreadTestTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(anthropicThreadReplyV160("msg_2", false)))}, nil
	})})
	if err != nil || followUp == nil || followUp.StopReason != goai.StopReasonStop || len(followUp.Content) == 0 || requests != 1 {
		t.Fatalf("follow-up=%#v err=%v requests=%d", followUp, err, requests)
	}
}
