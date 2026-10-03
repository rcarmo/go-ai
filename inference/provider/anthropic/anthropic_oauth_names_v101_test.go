package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV101AnthropicOAuthNamesInitialInlineReplayAndStream(t *testing.T) {
	for _, mode := range []string{"oauth-key", "oauth-token", "ordinary"} {
		t.Run(mode, func(t *testing.T) {
			name := "read"
			if mode != "ordinary" {
				name = "Read"
			}
			var payload map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				if err = json.Unmarshal(data, &payload); err != nil {
					t.Error(err)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg\",\"usage\":{\"input_tokens\":1}}}\n\n"))
				b, _ := json.Marshal(map[string]any{"index": 0, "content_block": map[string]any{"type": "tool_use", "id": "newcall", "name": name}})
				_, _ = w.Write([]byte("event: content_block_start\ndata: " + string(b) + "\n\nevent: message_delta\ndata: {\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {}\n\n"))
			}))
			defer server.Close()
			model := &goai.Model{ID: "custom", Api: goai.ApiAnthropicMessages, Provider: goai.ProviderAnthropic, BaseURL: server.URL, MaxTokens: 4096, AnthropicCompat: &goai.AnthropicMessagesCompat{SupportsMidConvoSystemMessages: boolPtrCompat(true), SupportsMidConvoToolChanges: boolPtrCompat(true), SupportsStrictTools: boolPtrCompat(true)}}
			ctx := inlineContextV101(true)
			ctx.Messages = append(ctx.Messages, goai.Message{Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID, Content: []goai.ContentBlock{{Type: "toolCall", ID: "oldcall", Name: "read", Arguments: map[string]any{"query": "q"}}}}, goai.Message{Role: goai.RoleToolResult, ToolCallID: "oldcall", ToolName: "read", Content: []goai.ContentBlock{{Type: "text", Text: "result"}}})
			opts := &goai.StreamOptions{APIKey: "ordinary-key", Env: goai.ProviderEnv{"ANTHROPIC_AUTH_TOKEN": ""}}
			if mode == "oauth-key" {
				opts.APIKey = "sk-ant-oat-key"
			}
			if mode == "oauth-token" {
				opts.Env["ANTHROPIC_AUTH_TOKEN"] = "sk-ant-oat-env"
			}
			var done *goai.Message
			for event := range streamAnthropic(context.Background(), model, ctx, opts) {
				switch e := event.(type) {
				case *goai.ErrorEvent:
					t.Fatal(e.Err)
				case *goai.DoneEvent:
					done = e.Message
				}
			}
			if done == nil || len(done.Content) == 0 || done.Content[0].Name != "read" {
				t.Fatalf("incoming names=%#v", done)
			}
			if payload["tools"].([]any)[0].(map[string]any)["name"] != name {
				t.Fatalf("initial names=%#v", payload["tools"])
			}
			inline, replay := 0, 0
			for _, raw := range payload["messages"].([]any) {
				blocks, ok := raw.(map[string]any)["content"].([]any)
				if !ok {
					continue
				}
				for _, raw := range blocks {
					b := raw.(map[string]any)
					switch b["type"] {
					case "tool_addition":
						d := b["tool"].(map[string]any)["definition"].(map[string]any)
						if d["description"] == "redefined" {
							inline++
							if d["name"] != name {
								t.Fatalf("inline=%#v", d)
							}
						}
					case "tool_use":
						replay++
						if b["name"] != name {
							t.Fatalf("replay=%#v", b)
						}
					}
				}
			}
			if inline != 1 || replay != 1 {
				t.Fatalf("inline=%d replay=%d", inline, replay)
			}
		})
	}
}
