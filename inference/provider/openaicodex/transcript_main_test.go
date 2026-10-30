package openaicodex

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/klauspost/compress/zstd"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/transports/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestCodexTranscriptInstructionsWireMain(t *testing.T) {
	text := func(s string) []goai.ContentBlock { return []goai.ContentBlock{{Type: "text", Text: s}} }
	system := func(s string) goai.Message { return goai.Message{Role: goai.RoleSystem, Content: text(s)} }
	ptr := func(s string) *string { return &s }
	user := func(s string) any {
		return map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": s}}}
	}
	update := func(role, s string) any { return map[string]any{"role": role, "content": s} }
	plain := goai.Tool{Name: "keep", Parameters: json.RawMessage(`{"type":"object"}`)}
	old := plain
	old.Name = "old"
	for _, transport := range []goai.Transport{goai.TransportSSE, goai.TransportWebSocket} {
		for _, tc := range []struct {
			name           string
			context        *goai.Context
			mid, reasoning bool
			instructions   string
			input          []any
			tools          []string
		}{
			{name: "empty", context: &goai.Context{}, instructions: "You are a helpful assistant.", input: []any{}},
			{name: "legacy", context: &goai.Context{SystemPrompt: "legacy", Messages: []goai.Message{goai.UserMessage("hello")}}, instructions: "legacy", input: []any{user("hello")}},
			{name: "folded", context: &goai.Context{SystemPrompt: "legacy", Messages: []goai.Message{system("initial"), goai.UserMessage("hello"), system("later"), goai.UserMessage("again")}}, instructions: "legacy\n\ninitial\n\nlater", input: []any{user("hello"), user("again")}},
			{name: "mid_developer", context: &goai.Context{Messages: []goai.Message{system("initial"), goai.UserMessage("hello"), system("later"), goai.UserMessage("again")}}, mid: true, reasoning: true, instructions: "initial", input: []any{user("hello"), update("developer", "later"), user("again")}},
			{name: "mid_system_no_leading", context: &goai.Context{Messages: []goai.Message{goai.UserMessage("hello"), system("later")}}, mid: true, instructions: "You are a helpful assistant.", input: []any{user("hello"), update("system", "later")}},
			{name: "sections_folded", context: &goai.Context{Messages: []goai.Message{{Role: goai.RoleSystem, Content: text("initial"), Sections: map[string]*string{"a": ptr("old")}}, goai.UserMessage("hello"), {Role: goai.RoleSystem, Content: text("later"), Sections: map[string]*string{"a": ptr("new"), "b": ptr("two")}}, {Role: goai.RoleSystem, Sections: map[string]*string{"b": nil}}}}, instructions: "initial\n\nlater\n\nnew", input: []any{user("hello")}},
			{name: "sections_mid", context: &goai.Context{Messages: []goai.Message{{Role: goai.RoleSystem, Content: text("initial"), Sections: map[string]*string{"a": ptr("old")}}, goai.UserMessage("hello"), {Role: goai.RoleSystem, Content: text("later"), Sections: map[string]*string{"a": ptr("new")}}, {Role: goai.RoleSystem, Sections: map[string]*string{"a": nil}}}}, mid: true, instructions: "initial\n\nold", input: []any{user("hello"), update("system", "later\n\nUpdated system prompt section \"a\":\n\nnew"), update("system", "Removed system prompt section \"a\".")}},
			{name: "whitespace", context: &goai.Context{SystemPrompt: " \n"}, instructions: " \n", input: []any{}},
			{name: "whitespace_tools", context: &goai.Context{SystemPrompt: " \n", Tools: []goai.Tool{plain}}, instructions: " \n", input: []any{}, tools: []string{"keep"}},
			{name: "content_empty_blocks", context: &goai.Context{Messages: []goai.Message{{Role: goai.RoleSystem, Content: []goai.ContentBlock{{Type: "text", Text: ""}, {Type: "text", Text: "initial"}, {Type: "text", Text: ""}}}}}, instructions: "\ninitial\n", input: []any{}},
			{name: "tool_snapshot", context: &goai.Context{Tools: []goai.Tool{old}, Messages: []goai.Message{goai.UserMessage("hello"), {Role: goai.RoleSystem, ToolsRemoved: []goai.ToolReference{{Name: "old"}}, ToolsAdded: []goai.Tool{plain}}}}, instructions: "You are a helpful assistant.", input: []any{user("hello")}, tools: []string{"keep"}},
		} {
			t.Run(fmt.Sprintf("%s/%s", transport, tc.name), func(t *testing.T) {
				captured := make(chan map[string]any, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					frame := `{"type":"response.completed","response":{"status":"completed","output":[]}}`
					if transport == goai.TransportWebSocket {
						conn, err := websocket.Accept(w, r, nil)
						if err != nil {
							t.Error(err)
							return
						}
						defer conn.CloseNow()
						ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
						defer cancel()
						_, raw, err := conn.Read(ctx)
						if err != nil {
							t.Error(err)
							return
						}
						var body map[string]any
						if err = json.Unmarshal(raw, &body); err != nil {
							t.Error(err)
							return
						}
						captured <- body
						_ = conn.Write(ctx, websocket.MessageText, []byte(frame))
						_, _, _ = conn.Read(ctx)
						return
					}
					var reader io.Reader = r.Body
					if r.Header.Get("Content-Encoding") == "zstd" {
						decoder, err := zstd.NewReader(r.Body)
						if err != nil {
							t.Error(err)
							return
						}
						defer decoder.Close()
						reader = decoder
					}
					var body map[string]any
					if err := json.NewDecoder(reader).Decode(&body); err != nil {
						t.Error(err)
						return
					}
					captured <- body
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "data: %s\n\n", frame)
				}))
				defer server.Close()
				model := codexGrammarModel()
				model.BaseURL = server.URL
				model.Reasoning = tc.reasoning
				model.ResponsesCompat.SupportsMidConvoSystemMessages = &tc.mid
				before, _ := json.Marshal(tc.context)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				hooks, done := 0, 0
				for event := range streamCodex(ctx, model, tc.context, &goai.StreamOptions{APIKey: payloadJWT, Transport: transport, OnPayload: func(body any, _ *goai.Model) (any, error) { hooks++; return body, nil }}) {
					switch e := event.(type) {
					case *goai.ErrorEvent:
						t.Error(e.Err)
					case *goai.DoneEvent:
						done++
					}
				}
				if done != 1 || hooks != 1 {
					t.Fatal("missing completion or hook replay", done, hooks)
				}
				var body map[string]any
				select {
				case body = <-captured:
				case <-ctx.Done():
					t.Fatal("missing request")
				}
				if body["instructions"] != tc.instructions || !reflect.DeepEqual(body["input"], tc.input) {
					t.Fatal("transcript projection differs", body["instructions"], body["input"], tc.instructions, tc.input)
				}
				var names []string
				if tools, ok := body["tools"].([]any); ok {
					for _, tool := range tools {
						names = append(names, tool.(map[string]any)["name"].(string))
					}
				}
				if !reflect.DeepEqual(names, tc.tools) {
					t.Fatal("effective tool snapshot differs", names, tc.tools)
				}
				after, _ := json.Marshal(tc.context)
				if string(before) != string(after) {
					t.Fatal("projection mutated caller context")
				}
			})
		}
	}
}
