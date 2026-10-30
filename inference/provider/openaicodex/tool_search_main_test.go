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

func TestCodexToolSearchFoldPlanMain(t *testing.T) {
	tool := func(name string) goai.Tool {
		return goai.Tool{Name: name, Parameters: json.RawMessage(`{"type":"object"}`)}
	}
	add := func(names ...string) goai.Message {
		m := goai.Message{Role: goai.RoleSystem}
		for _, name := range names {
			m.ToolsAdded = append(m.ToolsAdded, tool(name))
		}
		return m
	}
	remove := goai.Message{Role: goai.RoleSystem, ToolsRemoved: []goai.ToolReference{{Name: "a"}}}
	for _, tc := range []struct {
		name     string
		messages []goai.Message
		folded   int
		names    []string
	}{
		{name: "initial_additions", messages: []goai.Message{add("a"), goai.UserMessage("hello"), add("b")}, folded: 1, names: []string{"a"}},
		{name: "no_initial", messages: []goai.Message{goai.UserMessage("hello"), add("b")}, folded: 0},
		{name: "same_redeclared", messages: []goai.Message{add("a"), goai.UserMessage("hello"), add("a"), add("b")}, folded: 2, names: []string{"a"}},
		{name: "removed_then_add", messages: []goai.Message{add("a"), goai.UserMessage("hello"), remove, add("b")}, folded: 2},
		{name: "removed_readded", messages: []goai.Message{add("a"), goai.UserMessage("hello"), remove, add("a"), add("b")}, folded: 3, names: []string{"a"}},
		{name: "prefix_order", messages: []goai.Message{add("a", "b"), add("a"), remove, add("c")}, folded: 3, names: []string{"b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tools, folded := codexToolSearchPlan(tc.messages)
			var names []string
			for _, tool := range tools {
				names = append(names, tool.Name)
			}
			if folded != tc.folded || !reflect.DeepEqual(names, tc.names) {
				t.Fatal("fold boundary differs", folded, names, tc.folded, tc.names)
			}
		})
	}
}

func TestCodexToolSearchWireMain(t *testing.T) {
	for _, transport := range []goai.Transport{goai.TransportSSE, goai.TransportWebSocket} {
		for _, custom := range []bool{false, true} {
			for _, mid := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/custom_%t/mid_%t", transport, custom, mid), func(t *testing.T) {
					a := goai.Tool{Name: "a", Parameters: json.RawMessage(`{"type":"object"}`)}
					b := a
					b.Name = "b"
					if custom {
						b = codexPayloadTool("program")
						b.Name = "b"
					}
					conv := &goai.Context{Tools: []goai.Tool{a}, Messages: []goai.Message{goai.UserMessage("hello"), {Role: goai.RoleSystem, ToolsAdded: []goai.Tool{b}}}}
					before, _ := json.Marshal(conv)
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
					model.ContextWindow = 5000
					model.MaxTokens = 4000
					enabled := true
					model.ResponsesCompat.SupportsMidConvoSystemMessages = &mid
					model.ResponsesCompat.SupportsToolSearch = &enabled
					maximum := 2000
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					done, hooks := 0, 0
					for event := range streamCodex(ctx, model, conv, &goai.StreamOptions{APIKey: payloadJWT, Transport: transport, MaxTokens: &maximum, OnPayload: func(body any, _ *goai.Model) (any, error) { hooks++; return body, nil }}) {
						switch e := event.(type) {
						case *goai.ErrorEvent:
							t.Error(e.Err)
						case *goai.DoneEvent:
							done++
						}
					}
					if done != 1 || hooks != 1 {
						t.Fatal("completion/hook mismatch", done, hooks)
					}
					var body map[string]any
					select {
					case body = <-captured:
					case <-ctx.Done():
						t.Fatal("no request")
					}
					tools := body["tools"].([]any)
					input := body["input"].([]any)
					if mid {
						if len(tools) != 1 || tools[0].(map[string]any)["name"] != "a" || len(input) != 3 {
							t.Fatal("search declaration split differs", body)
						}
						call := input[1].(map[string]any)
						output := input[2].(map[string]any)
						wantID := "pi_tool_load_ngzlf2gp9pex"
						if call["type"] != "tool_search_call" || call["call_id"] != wantID || call["execution"] != "client" || call["status"] != "completed" || !reflect.DeepEqual(call["arguments"], map[string]any{"query": "b", "limit": float64(1)}) {
							t.Fatal("load call differs from pinned vector", call)
						}
						if output["type"] != "tool_search_output" || output["call_id"] != wantID || output["execution"] != "client" || output["status"] != "completed" {
							t.Fatal("load output mismatch", output)
						}
						loaded := output["tools"].([]any)
						definition := loaded[0].(map[string]any)
						wantType := "function"
						if custom {
							wantType = "custom"
						}
						if len(loaded) != 1 || definition["name"] != "b" || definition["type"] != wantType || definition["defer_loading"] != true {
							t.Fatal("load definition mismatch", loaded)
						}
						if _, exists := tools[0].(map[string]any)["defer_loading"]; exists {
							t.Fatal("request tool marked as search result", tools)
						}
						wire, _ := json.Marshal(input)
						budget := &goai.Context{SystemPrompt: "You are a helpful assistant.", Messages: []goai.Message{goai.UserMessage(string(wire))}, Tools: []goai.Tool{a}}
						expected := goai.ClampStreamMaxTokensPtr(model, budget, &goai.StreamOptions{MaxTokens: &maximum})
						if expected == nil || body["max_output_tokens"] != float64(*expected) {
							t.Fatal("search/prefix budget mismatch", body["max_output_tokens"], expected)
						}
					} else { // Keep the pre-existing safe snapshot; pinned plans lose b in this combination.
						if len(tools) != 2 || tools[0].(map[string]any)["name"] != "a" || tools[1].(map[string]any)["name"] != "b" || len(input) != 1 {
							t.Fatal("snapshot fallback lost available tools", body)
						}
					}
					after, _ := json.Marshal(conv)
					if string(before) != string(after) {
						t.Fatal("search projection mutated caller context")
					}
				})
			}
		}
	}
}
