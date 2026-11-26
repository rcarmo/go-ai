package google

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source: pi v1.6.1 google-thinking-level-map.test.ts:179–188,213–219,227–237.
// The TS tests capture config.thinkingConfig in the SDK payload hook. The Go
// REST equivalent is generationConfig.thinkingConfig; wire checks are separate.
func TestSourceV161GoogleThinkingXHigh(t *testing.T) {
	custom, zero := 1234, 0
	gemini4 := map[goai.ModelThinkingLevel]*string{
		"off": nil, "minimal": nil, "low": upstreamGoogleString("low"), "medium": upstreamGoogleString("medium"),
		"high": upstreamGoogleString("high"), "xhigh": upstreamGoogleString("xhigh"), "max": nil,
	}
	for _, tc := range []struct {
		name, id, effort string
		api              goai.Api
		mapping          map[goai.ModelThinkingLevel]*string
		budgets          *goai.ThinkingBudgets
		want             map[string]any
	}{
		{"genai/xhigh-level", "gemini-4-hydrogen", "xhigh", goai.ApiGoogleGenerativeAI, gemini4, nil,
			map[string]any{"includeThoughts": true, "thinkingLevel": "XHIGH"}},
		{"vertex/xhigh-level", "gemini-4-hydrogen", "xhigh", goai.ApiGoogleVertex, gemini4, nil,
			map[string]any{"includeThoughts": true, "thinkingLevel": "XHIGH"}},
		{"genai/xhigh-high-budget", "gemini-2.5-flash", "xhigh", goai.ApiGoogleGenerativeAI,
			map[goai.ModelThinkingLevel]*string{"xhigh": upstreamGoogleString("xhigh")}, &goai.ThinkingBudgets{High: &custom},
			map[string]any{"includeThoughts": true, "thinkingBudget": float64(1234)}},
		{"vertex/xhigh-high-budget", "gemini-2.5-pro", "xhigh", goai.ApiGoogleVertex,
			map[goai.ModelThinkingLevel]*string{"xhigh": upstreamGoogleString("xhigh")}, nil,
			map[string]any{"includeThoughts": true, "thinkingBudget": float64(32768)}},
		{"vertex/flash-lite-minimal-budget", "gemini-2.5-flash-lite", "minimal", goai.ApiGoogleVertex,
			map[goai.ModelThinkingLevel]*string{}, nil, map[string]any{"includeThoughts": true, "thinkingBudget": float64(512)}},
		{"genai/uppercase-xhigh-mapping", "gemini-4-hydrogen", "high", goai.ApiGoogleGenerativeAI,
			map[goai.ModelThinkingLevel]*string{"high": upstreamGoogleString("XHIGH")}, nil,
			map[string]any{"includeThoughts": true, "thinkingLevel": "XHIGH"}},
		{"vertex/xhigh-zero-custom-budget", "gemini-2.5-pro", "xhigh", goai.ApiGoogleVertex,
			map[goai.ModelThinkingLevel]*string{"xhigh": upstreamGoogleString("xhigh")}, &goai.ThinkingBudgets{High: &zero},
			map[string]any{"includeThoughts": true, "thinkingBudget": float64(0)}},
		{"genai/xhigh-dynamic-budget", "gemini-2.0-flash", "xhigh", goai.ApiGoogleGenerativeAI,
			map[goai.ModelThinkingLevel]*string{"xhigh": upstreamGoogleString("xhigh")}, nil,
			map[string]any{"includeThoughts": true, "thinkingBudget": float64(-1)}},
		{"genai/mapped-high-level", "gemini-3.7-flash", "xhigh", goai.ApiGoogleGenerativeAI,
			map[goai.ModelThinkingLevel]*string{"xhigh": upstreamGoogleString("high")}, nil,
			map[string]any{"includeThoughts": true, "thinkingLevel": "HIGH"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := upstreamGoogleThinkingModel(tc.api, tc.id, tc.mapping)
			hook := captureUpstreamGoogleThinking(t, model, tc.effort, tc.budgets)
			if !reflect.DeepEqual(hook, tc.want) {
				t.Errorf("payload hook thinkingConfig=%#v, want %#v", hook, tc.want)
			}
			level := goai.ThinkingLevel(tc.effort)
			endpoint := model.BaseURL + "/models/" + model.ID + ":streamGenerateContent?alt=sse"
			if tc.api == goai.ApiGoogleVertex {
				endpoint = model.BaseURL + "/publishers/google/models/" + model.ID + ":streamGenerateContent?alt=sse"
			}
			var wire struct {
				GenerationConfig struct {
					ThinkingConfig map[string]any `json:"thinkingConfig"`
				} `json:"generationConfig"`
			}
			requests := 0
			transport := vertexTokenWireFunc(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				requests++
				if requests != 1 || r.Method != http.MethodPost || r.URL.String() != endpoint ||
					!slices.Equal(r.Header.Values("X-Goog-Api-Key"), []string{"test"}) || len(r.Header.Values("Authorization")) != 0 {
					return nil, fmt.Errorf("unexpected scripted Google request %d: %s %s", requests, r.Method, r.URL)
				}
				if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
					return nil, err
				}
				const events = `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}` + "\n\n"
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(events))}, nil
			})
			maxRetries := 0
			message, err := goai.Complete(t.Context(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("Hello")}},
				&goai.StreamOptions{APIKey: "test", Reasoning: &level, ThinkingBudgets: tc.budgets, MaxRetries: &maxRetries, HTTPTransport: transport})
			if err != nil || message == nil || message.StopReason != goai.StopReasonStop || requests != 1 {
				t.Fatalf("result=%+v err=%v requests=%d, want one scripted completion", message, err, requests)
			}
			if got := wire.GenerationConfig.ThinkingConfig; !reflect.DeepEqual(got, tc.want) {
				t.Errorf("wire thinkingConfig=%#v, want %#v", got, tc.want)
			}
		})
	}
}
