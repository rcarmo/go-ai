package openai

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// pi v1.6.1 openai-completions-non-null-assistant-content.test.ts:87–112.
// This compares the real registry models through the public payload hook,
// stopping before any transport rather than testing a hand-built compat flag.
func TestSourceV161WorkersAIToolCallReplayContent(t *testing.T) {
	goai.RegisterBuiltinModels()
	for _, tc := range []struct{ id, content string }{
		{"@cf/ibm-granite/granite-4.0-h-micro", `""`},
		{"@cf/meta/llama-3.3-70b-instruct-fp8-fast", `""`},
		{"@cf/openai/gpt-oss-120b", `""`},
		{"@cf/openai/gpt-oss-20b", `""`},
		{"@cf/zai-org/glm-5.3", `null`},
	} {
		t.Run(tc.id, func(t *testing.T) {
			model := goai.GetModel(goai.ProviderCloudflareWorkersAI, tc.id)
			if model == nil || model.Api != goai.ApiOpenAICompletions {
				t.Fatalf("expected Workers AI Completions catalog entry %q, got %+v", tc.id, model)
			}
			user, thanks := goai.UserMessage("Run the command"), goai.UserMessage("Thanks")
			user.Timestamp, thanks.Timestamp = 1, 4
			transcript := &goai.Context{Messages: []goai.Message{
				user,
				{Role: goai.RoleAssistant, Provider: model.Provider, Api: model.Api, Model: model.ID, StopReason: goai.StopReasonToolUse, Timestamp: 2, Content: []goai.ContentBlock{{Type: "toolCall", ID: "tool-1", Name: "bash", Arguments: map[string]any{"command": "true"}}}},
				{Role: goai.RoleToolResult, ToolCallID: "tool-1", ToolName: "bash", Timestamp: 3, Content: []goai.ContentBlock{{Type: "text", Text: "ok"}}},
				thanks,
			}}
			var body struct {
				Messages []struct {
					Role      string          `json:"role"`
					Content   json.RawMessage `json:"content"`
					ToolCalls []struct {
						ID string `json:"id"`
					} `json:"tool_calls"`
				} `json:"messages"`
			}
			captured := errors.New("payload captured")
			var sends atomic.Int32
			hooks, stop := 0, false
			for ev := range goai.Stream(t.Context(), model, transcript, &goai.StreamOptions{
				APIKey: "test", Env: goai.ProviderEnv{"CLOUDFLARE_ACCOUNT_ID": "test"},
				HTTPTransport: promptCacheTransportV160(func(*http.Request) (*http.Response, error) {
					sends.Add(1)
					return nil, errors.New("unexpected request after payload hook")
				}),
				OnPayload: func(payload any, _ *goai.Model) (any, error) {
					hooks++
					data, err := json.Marshal(payload)
					if err != nil {
						return nil, err
					}
					if err := json.Unmarshal(data, &body); err != nil {
						return nil, err
					}
					return nil, captured
				},
			}) {
				if event, ok := ev.(*goai.ErrorEvent); ok {
					if !errors.Is(event.Err, captured) {
						t.Fatalf("unexpected provider outcome: %v", event.Err)
					}
					stop = true
				}
			}
			if hooks != 1 || !stop || sends.Load() != 0 {
				t.Fatalf("hooks=%d stopped=%v transports=%d; want one payload capture and no sends", hooks, stop, sends.Load())
			}
			var roles []string
			for _, msg := range body.Messages {
				roles = append(roles, msg.Role)
			}
			if !reflect.DeepEqual(roles, []string{"user", "assistant", "tool", "user"}) {
				t.Fatalf("roles=%q; want user, assistant, tool, user", roles)
			}
			assistant := body.Messages[1]
			if string(assistant.Content) != tc.content || len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "tool-1" {
				t.Errorf("assistant content=%s calls=%+v; want content %s and tool-1", assistant.Content, assistant.ToolCalls, tc.content)
			}
		})
	}
}
