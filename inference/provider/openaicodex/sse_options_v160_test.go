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
			model := &goai.Model{ID: "gpt-5.5", Name: "GPT-5.5", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses,
				BaseURL: "https://chatgpt.com/backend-api", Reasoning: true, Input: []string{"text"}, ContextWindow: 400000, MaxTokens: 128000}
			zero := 0
			opts := &goai.StreamOptions{APIKey: token, Transport: goai.TransportSSE, ToolChoice: tc.choice, MaxRetries: &zero, HTTPTransport: transport}
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
			result, err := goai.Complete(t.Context(), model, transcript, opts)
			if err != nil || result == nil || result.StopReason != goai.StopReasonStop || requests != 1 || payload["model"] != "gpt-5.5" {
				t.Fatalf("result = %+v, err=%v requests=%d model=%v; want one successful GPT-5.5 scripted request", result, err, requests, payload["model"])
			}
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
