package openairesponses

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/inference/provider/openaicodex"
)

type replayTransportV161 func(*http.Request) (*http.Response, error)

func (f replayTransportV161) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Source: pi v1.6.1 openai-responses-tool-call-replay.test.ts:30–74.
// The source tests the shared converter with the Codex catalogue model. The
// OpenAI Responses row covers the other Go caller of the replay format.
func TestSourceV161ResponsesToolCallReplay(t *testing.T) {
	goai.RegisterBuiltinModels()
	for _, tc := range []struct {
		name, endpoint string
		provider       goai.Provider
		api            goai.Api
	}{
		{"codex", "https://chatgpt.com/backend-api/codex/responses", goai.ProviderOpenAICodex, goai.ApiOpenAICodexResponses},
		{"responses", "https://api.openai.com/v1/responses", goai.ProviderOpenAI, goai.ApiOpenAIResponses},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := goai.GetModel(tc.provider, "gpt-6.2-sol")
			if model == nil || model.Api != tc.api || model.ResponsesCompat == nil ||
				model.ResponsesCompat.SupportsOpenAIGrammarTools == nil || !*model.ResponsesCompat.SupportsOpenAIGrammarTools {
				t.Fatalf("missing published %s GPT-6.2 Sol grammar support", tc.provider)
			}
			toolResult := func(id, name string) goai.Message {
				return goai.Message{Role: goai.RoleToolResult, ToolCallID: id, ToolName: name,
					Content: []goai.ContentBlock{{Type: "text", Text: "ok"}}}
			}
			transcript := &goai.Context{Messages: []goai.Message{
				goai.UserMessage("Use the tools."),
				{Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID, Usage: &goai.Usage{}, StopReason: goai.StopReasonToolUse,
					Content: []goai.ContentBlock{
						{Type: "toolCall", ID: "call_1|fc_1", Name: "read", Arguments: map[string]any{"path": "calc.py"}},
						{Type: "toolCall", ID: "call_2|ctc_2", Name: "codemode", Arguments: map[string]any{"code": "text(1)"}},
					}},
				toolResult("call_1|fc_1", "read"), toolResult("call_2|ctc_2", "codemode"),
			}, Tools: []goai.Tool{{Name: "codemode", Parameters: json.RawMessage(`{"type":"object","required":["code"],"properties":{"code":{"type":"string"}}}`),
				ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": `start: "text(1)"`}}}}}
			key := "fixture-key"
			if tc.provider == goai.ProviderOpenAICodex {
				key = "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acc_test"}}`)) + "."
			}
			var request struct {
				Input []map[string]any `json:"input"`
			}
			requests := 0
			transport := replayTransportV161(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				requests++
				if requests != 1 || r.Method != http.MethodPost || r.URL.String() != tc.endpoint || r.Header.Get("Authorization") != "Bearer "+key {
					return nil, fmt.Errorf("unexpected scripted Responses replay request %d: %s %s", requests, r.Method, r.URL)
				}
				var body io.Reader = r.Body
				switch r.Header.Get("Content-Encoding") {
				case "zstd":
					decoder, err := zstd.NewReader(r.Body)
					if err != nil {
						return nil, err
					}
					defer decoder.Close()
					body = decoder
				case "":
				default:
					return nil, fmt.Errorf("unexpected replay encoding %q", r.Header.Get("Content-Encoding"))
				}
				if err := json.NewDecoder(body).Decode(&request); err != nil {
					return nil, err
				}
				const terminal = `data: {"type":"response.completed","response":{"id":"resp_test","status":"completed"}}` + "\n\n"
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(terminal))}, nil
			})
			zero := 0
			result, err := goai.Complete(t.Context(), model, transcript, &goai.StreamOptions{APIKey: key, Transport: goai.TransportSSE, HTTPTransport: transport, MaxRetries: &zero})
			if result == nil || result.StopReason != goai.StopReasonStop || err != nil || requests != 1 {
				t.Fatalf("replay result=%+v err=%v requests=%d, want one scripted success", result, err, requests)
			}
			var calls []map[string]any
			for _, item := range request.Input {
				if item["type"] == "function_call" || item["type"] == "custom_tool_call" {
					calls = append(calls, item)
				}
			}
			if len(calls) != 2 {
				t.Fatalf("replayed calls=%#v, want exactly two", calls)
			}
			wantFunction := map[string]any{"type": "function_call", "id": "fc_1", "call_id": "call_1", "name": "read", "arguments": `{"path":"calc.py"}`, "status": "completed"}
			if !reflect.DeepEqual(calls[0], wantFunction) {
				t.Errorf("function replay=%#v, want %#v", calls[0], wantFunction)
			}
			wantCustom := map[string]any{"type": "custom_tool_call", "id": "ctc_2", "call_id": "call_2", "name": "codemode", "input": "text(1)", "status": "completed"}
			if !reflect.DeepEqual(calls[1], wantCustom) {
				t.Errorf("custom replay=%#v, want %#v", calls[1], wantCustom)
			}
		})
	}
}
