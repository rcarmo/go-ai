package openaicodex

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	goai "github.com/rcarmo/go-ai"
)

// Source: pi v1.6.0 openai-codex-stream.test.ts:960–1019,1021–1074.
// The high and default-tool-choice rows are separate contrast fixtures.
func TestSourceV160CodexSSEOptions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		effort goai.ThinkingLevel
		choice goai.ToolChoice
		want   string
	}{
		{name: "simple-xhigh", effort: goai.ThinkingXHigh, want: "xhigh"},
		{name: "simple-high", effort: goai.ThinkingHigh, want: "high"},
		{name: "required-tool-choice", choice: goai.ToolChoiceRequired, want: "required"},
		{name: "default-tool-choice", want: "auto"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &goai.Model{ID: "gpt-5.5", Name: "GPT-5.5", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses,
				BaseURL: "https://chatgpt.com/backend-api", Reasoning: true, Input: []string{"text"}, ContextWindow: 400000, MaxTokens: 128000}
			opts := goai.StreamOptions{ToolChoice: tc.choice}
			transcript := &goai.Context{}
			if tc.effort != "" {
				xhigh := "xhigh"
				model.ThinkingLevelMap = map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingXHigh): &xhigh}
				// Complete selects the registered simple path when Reasoning is set.
				opts.Reasoning = &tc.effort
				transcript.SystemPrompt = "You are a helpful assistant."
				transcript.Messages = []goai.Message{goai.UserMessage("Say hello")}
			} else {
				transcript.Messages = []goai.Message{goai.UserMessage("Do not call ping. Respond with text instead.")}
				transcript.Tools = []goai.Tool{{Name: "ping", Description: "Ping",
					Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`)}}
			}
			_, payload := completeCodexSSEOptionsV160(t, model, transcript, opts)
			if tc.effort != "" {
				want := map[string]any{"effort": tc.want, "summary": "auto"}
				if !reflect.DeepEqual(payload["reasoning"], want) {
					t.Errorf("reasoning = %#v, want %#v", payload["reasoning"], want)
				}
				return
			}
			if got := payload["tool_choice"]; got != tc.want {
				t.Errorf("tool_choice = %#v, want %q", got, tc.want)
			}
			tools, _ := payload["tools"].([]any)
			if len(tools) != 1 {
				t.Fatalf("tools = %#v, want one ping declaration", payload["tools"])
			}
			tool, _ := tools[0].(map[string]any)
			if tool["type"] != "function" || tool["name"] != "ping" || tool["description"] != "Ping" {
				t.Errorf("tools[0] = %#v, want Ping function", tool)
			}
		})
	}
}

func completeCodexSSEOptionsV160(t *testing.T, model *goai.Model, transcript *goai.Context, opts goai.StreamOptions) (*goai.Message, map[string]any) {
	t.Helper()
	const accountID = "acc_test"
	token := accountJWT(accountID)
	var payload map[string]any
	requests := 0
	transport := codexTerminalTransportV160(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		requests++
		if requests != 1 || r.Method != http.MethodPost || r.URL.String() != "https://chatgpt.com/backend-api/codex/responses" {
			return nil, fmt.Errorf("unexpected Codex scripted request %d: %s %s", requests, r.Method, r.URL)
		}
		if !slices.Equal(r.Header.Values("Authorization"), []string{"Bearer " + token}) ||
			!slices.Equal(r.Header.Values("ChatGPT-Account-Id"), []string{accountID}) || len(r.Header.Values("X-Api-Key")) != 0 {
			return nil, fmt.Errorf("unexpected synthetic Codex authentication")
		}
		var reader io.Reader = r.Body
		switch encoding := r.Header.Get("Content-Encoding"); encoding {
		case "zstd":
			decoder, err := zstd.NewReader(reader)
			if err != nil {
				return nil, err
			}
			defer decoder.Close()
			reader = decoder
		case "":
		default:
			return nil, fmt.Errorf("unexpected Codex encoding %q", encoding)
		}
		if err := json.NewDecoder(reader).Decode(&payload); err != nil {
			return nil, err
		}
		const terminal = `data: {"type":"response.completed","response":{"status":"completed"}}` + "\n\n"
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(terminal))}, nil
	})
	zero := 0
	opts.APIKey, opts.Transport, opts.MaxRetries, opts.HTTPTransport = token, goai.TransportSSE, &zero, transport
	result, err := goai.Complete(t.Context(), model, transcript, &opts)
	if err != nil || result == nil || result.StopReason != goai.StopReasonStop || requests != 1 || payload["model"] != model.ID {
		t.Fatalf("result = %+v, err=%v requests=%d model=%v; want one successful scripted %s request", result, err, requests, payload["model"], model.ID)
	}
	return result, payload
}

// Source-derived from openai-codex-responses.ts:632–645. The unchanged
// stream test fixtures check other fields on unset effort and cover the
// explicit/simple-effort branch, not the raw unset reasoning shape itself.
func TestSourceV160CodexSSEReasoningDefaults(t *testing.T) {
	xhigh, off, empty := goai.ThinkingXHigh, goai.ThinkingLevel(goai.ThinkingOff), goai.ThinkingLevel("")
	minimal, none, nativeXHigh := "minimal", "none", "xhigh"
	offMap := func(value *string) map[goai.ModelThinkingLevel]*string {
		return map[goai.ModelThinkingLevel]*string{goai.ThinkingOff: value}
	}
	for _, tc := range []struct {
		name, summary, level string
		effort               *goai.ThinkingLevel
		mapping              map[goai.ModelThinkingLevel]*string
		mid, nonReasoning    bool
		want                 map[string]any
	}{
		{name: "unset", want: map[string]any{"effort": "none"}},
		{name: "summary-only", summary: "detailed", want: map[string]any{"effort": "none"}},
		{name: "empty-option", effort: &empty, want: map[string]any{"effort": "none"}},
		{name: "mapped-off", mapping: offMap(&minimal), want: map[string]any{"effort": "minimal"}},
		{name: "null-off", mapping: offMap(nil)},
		{name: "nonreasoning", nonReasoning: true},
		{name: "explicit-summary", effort: &xhigh, summary: "detailed",
			mapping: map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingXHigh): &nativeXHigh},
			want:    map[string]any{"effort": "xhigh", "summary": "detailed"}},
		{name: "explicit-off", effort: &off, want: map[string]any{"effort": "none", "summary": "auto"}},
		{name: "configured-off", mid: true, mapping: offMap(&none), level: "none", want: map[string]any{"effort": "medium", "summary": "auto"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &goai.Model{ID: "gpt-5.5", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses,
				BaseURL: "https://chatgpt.com/backend-api", Reasoning: !tc.nonReasoning, Input: []string{"text"}, ContextWindow: 400000, MaxTokens: 128000,
				ThinkingLevelMap: tc.mapping}
			if tc.mid {
				model.ID = "gpt-6-sol"
				model.ResponsesCompat = &goai.OpenAIResponsesCompat{SupportsMidConvoEffort: &tc.mid}
			}
			transcript := &goai.Context{Messages: []goai.Message{goai.UserMessage("Say hello")}}
			result, payload := completeCodexSSEOptionsV160(t, model, transcript, goai.StreamOptions{Reasoning: tc.effort, ReasoningSummary: tc.summary})
			got, present := payload["reasoning"]
			if tc.want == nil {
				if present {
					t.Errorf("reasoning = %#v, want absent member", got)
				}
			} else if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("reasoning = %#v, want %#v", got, tc.want)
			}
			wantOrder := []string{"user"}
			if tc.level != "" {
				wantOrder = append(wantOrder, "configuration_update:"+tc.level)
			}
			if order := summarizeCodexProjection(t, payload["input"]); !slices.Equal(order, wantOrder) || result.ProviderThinkingLevel != tc.level {
				t.Errorf("input/level = %v/%q, want %v/%q", order, result.ProviderThinkingLevel, wantOrder, tc.level)
			}
		})
	}
}
