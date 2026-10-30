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

func TestCodexAdditionalToolsWireMain(t *testing.T) {
	for _, transport := range []goai.Transport{goai.TransportSSE, goai.TransportWebSocket} {
		for _, reasoning := range []bool{false, true} {
			for _, custom := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/reasoning_%t/custom_%t", transport, reasoning, custom), func(t *testing.T) {
					tool := func(name string) goai.Tool {
						return goai.Tool{Name: name, Parameters: json.RawMessage(`{"type":"object"}`)}
					}
					a, b, c := tool("a"), tool("b"), tool("c")
					if custom {
						c = codexPayloadTool("program")
						c.Name = "c"
					}
					c2 := c
					c2.Description = "replacement"
					system := func(added []goai.Tool, removed []goai.ToolReference, text string) goai.Message {
						return goai.Message{Role: goai.RoleSystem, ToolsAdded: added, ToolsRemoved: removed, Content: []goai.ContentBlock{{Type: "text", Text: text}}}
					}
					conv := &goai.Context{SystemPrompt: "initial", Tools: []goai.Tool{a, b}, Messages: []goai.Message{goai.UserMessage("hello"), system(nil, []goai.ToolReference{{Name: "b"}}, ""), system([]goai.Tool{c}, nil, "c notes"), system(nil, []goai.ToolReference{{Name: "a"}}, ""), system([]goai.Tool{c2}, []goai.ToolReference{{Name: "c"}}, ""), system([]goai.Tool{a}, []goai.ToolReference{{Name: "c"}}, ""), goai.UserMessage("end")}}
					before, _ := json.Marshal(conv)
					capture := make(chan map[string]any, 1)
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
							capture <- body
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
						capture <- body
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprintf(w, "data: %s\n\n", frame)
					}))
					defer server.Close()
					model := codexGrammarModel()
					model.BaseURL = server.URL
					model.Reasoning = reasoning
					model.ContextWindow = 5000
					model.MaxTokens = 4000
					enabled := true
					model.ResponsesCompat.SupportsMidConvoSystemMessages = &enabled
					model.ResponsesCompat.SupportsAdditionalTools = &enabled
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
					case body = <-capture:
					case <-ctx.Done():
						t.Fatal("no request")
					}
					if _, exists := body["tools"]; exists {
						t.Fatal("anchored declarations duplicated at request level", body["tools"])
					}
					input := body["input"].([]any)
					role := "system"
					if reasoning {
						role = "developer"
					}
					var events []string
					cDeclarations := 0
					for _, raw := range input {
						item := raw.(map[string]any)
						switch item["type"] {
						case "additional_tools":
							if item["role"] != "developer" {
								t.Error("additional-tools role differs", item)
							}
							names := []string{}
							for _, rawTool := range item["tools"].([]any) {
								definition := rawTool.(map[string]any)
								name := definition["name"].(string)
								names = append(names, name)
								if name == "c" {
									cDeclarations++
									if cDeclarations == 2 && definition["description"] != "replacement" {
										t.Error("replacement description lost", definition)
									}
									wantType := "function"
									if custom {
										wantType = "custom"
									}
									if definition["type"] != wantType {
										t.Error("grammar declaration type lost", definition)
									}
								}
							}
							events = append(events, fmt.Sprint("tools:", names))
						default:
							if item["role"] == "user" {
								events = append(events, "user")
							} else {
								if item["role"] != role {
									t.Error("notice role differs", item)
								}
								events = append(events, item["content"].(string))
							}
						}
					}
					base := "This is an incremental tools update. Previously declared tools remain available unless explicitly marked unavailable. A tool declared again here is available again, and its latest definition replaces the earlier one."
					remove := func(name string) string {
						return "The following tools are no longer available. Do not call them:\n- " + name
					}
					want := []string{"tools:[a b]", "user", remove("b"), base + "\nStill unavailable: b.", "tools:[c]", "c notes", remove("a"), base + "\nStill unavailable: b, a.", "tools:[c]", base + "\nStill unavailable: b.", "tools:[a]", remove("c"), "user"}
					if !reflect.DeepEqual(events, want) {
						t.Fatal("anchored notices/order differ", events, want)
					}
					if body["instructions"] != "initial" {
						t.Fatal("initial text duplicated/lost", body)
					}
					wire, _ := json.Marshal(input)
					budget := &goai.Context{SystemPrompt: "initial", Messages: []goai.Message{goai.UserMessage(string(wire))}}
					expected := goai.ClampStreamMaxTokensPtr(model, budget, &goai.StreamOptions{MaxTokens: &maximum})
					if expected == nil || body["max_output_tokens"] != float64(*expected) {
						t.Fatal("anchored declarations/notices escaped context guard", body["max_output_tokens"], expected)
					}
					after, _ := json.Marshal(conv)
					if string(after) != string(before) {
						t.Fatal("anchoring mutated caller context")
					}
				})
			}
		}
	}
}

func TestCodexAdditionalToolsRequireBothCapabilitiesMain(t *testing.T) {
	tool := goai.Tool{Name: "a", Parameters: json.RawMessage(`{"type":"object"}`)}
	for _, mid := range []bool{false, true} {
		for _, additional := range []bool{false, true} {
			model := codexGrammarModel()
			model.ResponsesCompat.SupportsMidConvoSystemMessages = &mid
			model.ResponsesCompat.SupportsAdditionalTools = &additional
			conv := &goai.Context{Tools: []goai.Tool{tool}, Messages: []goai.Message{goai.UserMessage("hello")}}
			request := buildCodexRequest(model, conv, nil)
			var input []map[string]any
			if err := json.Unmarshal(request.Input, &input); err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, item := range input {
				if item["type"] == "additional_tools" {
					count++
				}
			}
			if mid && additional {
				if len(request.Tools) != 0 || count != 1 {
					t.Fatal("missing anchor", request)
				}
			} else if len(request.Tools) != 1 || count != 0 {
				t.Fatal("capability guard ignored", mid, additional, request)
			}
		}
	}
}
