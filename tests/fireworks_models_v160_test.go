package goai_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/inference/provider/openai"
)

// Source: pi v1.6.0 packages/ai/test/fireworks-models.test.ts:22,33,68.
func TestSourceV160FireworksModelProjection(t *testing.T) {
	t.Run("messages-route", func(t *testing.T) {
		model := requireModel(t, goai.ProviderFireworks, "accounts/fireworks/models/deepseek-v4p1-flash")
		if model.Api != goai.ApiAnthropicMessages || model.Provider != goai.ProviderFireworks || model.BaseURL != "https://api.fireworks.ai/inference" || !model.Reasoning || !reflect.DeepEqual(model.Input, []string{"text", "image"}) {
			t.Fatalf("DeepSeek Flash route = %+v", model)
		}
	})
	t.Run("glm-fast", func(t *testing.T) {
		base := requireModel(t, goai.ProviderFireworks, "accounts/fireworks/models/glm-5p3")
		fast := requireModel(t, goai.ProviderFireworks, "accounts/fireworks/routers/glm-5p3-fast")
		if fast.Api != base.Api || fast.BaseURL != base.BaseURL || !reflect.DeepEqual(fast.CompletionsCompat, base.CompletionsCompat) || !reflect.DeepEqual(fast.ThinkingLevelMap, base.ThinkingLevelMap) {
			t.Fatalf("GLM Fast = %+v; want API, URL, compat and thinking levels of %+v", fast, base)
		}
	})
	t.Run("kimi-effort", func(t *testing.T) {
		base := requireModel(t, goai.ProviderFireworks, "accounts/fireworks/models/kimi-k3")
		fast := requireModel(t, goai.ProviderFireworks, "accounts/fireworks/routers/kimi-k3-fast")
		enabled, disabled := true, false
		compat := &goai.OpenAICompletionsCompat{
			SupportsStore:      &disabled,
			SupportsStrictMode: &enabled,
			RequiresReasoningContentOnAssistantMessages: &enabled,
			ThinkingFormat:                 "openai",
			SupportsMidConvoSystemMessages: &enabled,
			SupportsMidConvoToolAdditions:  &enabled,
			SendSessionAffinityHeaders:     &enabled,
			SupportsLongCacheRetention:     &disabled,
		}
		low, high, maximum := "low", "high", "max"
		thinking := map[goai.ModelThinkingLevel]*string{
			"off": nil, "minimal": nil, "low": &low, "medium": nil, "high": &high, "xhigh": nil, "max": &maximum,
		}
		for _, model := range []*goai.Model{base, fast} {
			if model.Api != goai.ApiOpenAICompletions || model.BaseURL != "https://api.fireworks.ai/inference/v1" {
				t.Fatalf("%s route = %s %s", model.ID, model.Api, model.BaseURL)
			}
			if !reflect.DeepEqual(model.CompletionsCompat, compat) {
				t.Fatalf("%s compat = %#v; want exact Fireworks Kimi compat %#v", model.ID, model.CompletionsCompat, compat)
			}
			if !reflect.DeepEqual(model.ThinkingLevelMap, thinking) {
				t.Fatalf("%s thinking levels = %#v; want %#v", model.ID, model.ThinkingLevelMap, thinking)
			}
		}

		// The original aborts in OnPayload. Use loopback for the safety net so
		// a missed hook can't send a request to the real provider.
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer server.Close()
		model := *base
		model.BaseURL = server.URL
		requested := goai.ThinkingMax
		var captured map[string]json.RawMessage
		var calls int
		for range goai.GetApiProvider(model.Api).StreamSimple(t.Context(), &model, &goai.Context{Messages: []goai.Message{goai.UserMessage("test")}}, &goai.StreamOptions{
			APIKey: "test-fireworks-key", Reasoning: &requested,
			OnPayload: func(payload any, _ *goai.Model) (any, error) {
				calls++
				encoded, err := json.Marshal(payload)
				if err != nil {
					t.Error(err)
					return nil, err
				}
				if err = json.Unmarshal(encoded, &captured); err != nil {
					t.Error(err)
					return nil, err
				}
				return nil, errors.New("payload captured")
			},
		}) {
		}
		if calls != 1 || requests.Load() != 0 || string(captured["reasoning_effort"]) != `"max"` {
			t.Fatalf("payload hooks=%d requests=%d reasoning_effort=%s; want 1, 0, and max", calls, requests.Load(), captured["reasoning_effort"])
		}
	})
}
