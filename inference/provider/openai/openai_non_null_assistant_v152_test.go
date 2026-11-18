package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Mirrors all three v1.5.2 cases at the emitted HTTP body, including explicit
// null versus the empty string and the independent after-tool-result message.
func TestSourceV152NonNullAssistantContent(t *testing.T) {
	for _, tc := range []struct {
		name, compat string
		roles        []string
		contents     []any
	}{
		{"default", `{}`, []string{"user", "assistant", "tool", "user"}, []any{nil}},
		{"non-null", `{"requiresNonNullAssistantContent":true}`, []string{"user", "assistant", "tool", "user"}, []any{""}},
		{"after tool", `{"requiresAssistantAfterToolResult":true}`, []string{"user", "assistant", "tool", "assistant", "user"}, []any{"", "I have processed the tool results."}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var compat goai.OpenAICompletionsCompat
			if err := json.Unmarshal([]byte(tc.compat), &compat); err != nil {
				t.Fatal(err)
			}
			var got struct {
				Messages []map[string]any `json:"messages"`
			}
			request := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				request <- json.NewDecoder(r.Body).Decode(&got)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
			}))
			defer server.Close()
			model := &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, BaseURL: server.URL, MaxTokens: 100, CompletionsCompat: &compat}
			user, thanks := goai.UserMessage("Run the command"), goai.UserMessage("Thanks")
			user.Timestamp, thanks.Timestamp = 1, 4
			ctx := &goai.Context{Messages: []goai.Message{
				user,
				{Role: goai.RoleAssistant, Provider: model.Provider, Api: model.Api, Model: model.ID, StopReason: goai.StopReasonToolUse, Timestamp: 2, Content: []goai.ContentBlock{{Type: "toolCall", ID: "tool-1", Name: "bash", Arguments: map[string]any{"command": "true"}}}},
				{Role: goai.RoleToolResult, ToolCallID: "tool-1", ToolName: "bash", Timestamp: 3, Content: []goai.ContentBlock{{Type: "text", Text: "ok"}}},
				thanks,
			}}
			for ev := range streamOpenAI(context.Background(), model, ctx, &goai.StreamOptions{APIKey: "test"}) {
				if e, ok := ev.(*goai.ErrorEvent); ok {
					t.Fatal(e.Err)
				}
			}
			select {
			case err := <-request:
				if err != nil {
					t.Fatal(err)
				}
			default:
				t.Fatal("no HTTP request")
			}
			var roles []string
			var contents []any
			for _, message := range got.Messages {
				role := message["role"].(string)
				roles = append(roles, role)
				if role == "assistant" {
					content, present := message["content"]
					if !present {
						t.Fatal("missing assistant content property")
					}
					contents = append(contents, content)
				}
			}
			if !reflect.DeepEqual(roles, tc.roles) || !reflect.DeepEqual(contents, tc.contents) {
				t.Fatalf("roles=%q content=%#v, want roles=%q content=%#v", roles, contents, tc.roles, tc.contents)
			}
		})
	}
}
