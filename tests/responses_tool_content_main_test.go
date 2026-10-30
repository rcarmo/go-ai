package goai_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/klauspost/compress/zstd"
	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/inference/provider/openaicodex"
	_ "github.com/rcarmo/go-ai/inference/provider/openairesponses"
	"github.com/rcarmo/go-ai/transports/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

// Capture the registered production functions before registry-isolation tests run.
var contentWireProviders = []*goai.ApiProvider{goai.GetApiProvider(goai.ApiOpenAIResponses), goai.GetApiProvider(goai.ApiOpenAICodexResponses)}

func TestResponsesToolContentWireMain(t *testing.T) {
	for _, provider := range contentWireProviders {
		if provider == nil {
			t.Fatal("missing registered provider")
		}
		transports := []goai.Transport{goai.TransportSSE}
		if provider.Api == goai.ApiOpenAICodexResponses {
			transports = append(transports, goai.TransportWebSocket)
		}
		for _, transport := range transports {
			for _, images := range []bool{false, true} {
				for _, custom := range []bool{false, true} {
					for _, mode := range []string{"mixed", "images", "empty", "lines", "bom", "placeholder", "replacement"} {
						t.Run(fmt.Sprintf("%s/%s/images_%t/custom_%t/%s", provider.Api, transport, images, custom, mode), func(t *testing.T) {
							captured := make(chan map[string]any, 1)
							server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
								frame := `{"type":"response.completed","response":{"id":"done","status":"completed","output":[]}}`
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
							enabled := true
							model := &goai.Model{ID: "content-model", Api: provider.Api, Provider: "openai", BaseURL: server.URL, Input: []string{"text"}, ResponsesCompat: &goai.OpenAIResponsesCompat{SupportsOpenAIGrammarTools: &enabled}}
							if images {
								model.Input = append(model.Input, "image")
							}
							tool := goai.Tool{Name: "script", Parameters: json.RawMessage(`{"type":"object","properties":{"program":{"type":"string"}},"required":["program"]}`)}
							wantType := "function_call_output"
							if custom {
								tool.ConstrainedSampling = &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": "start: /.+/"}}
								wantType = "custom_tool_call_output"
							}
							image := goai.ContentBlock{Type: "image", MimeType: "image/png", Data: "AA=="}
							placeholder := "(tool image omitted: model does not support images)"
							text := func(value string) goai.ContentBlock { return goai.ContentBlock{Type: "text", Text: value} }
							wireImage := map[string]any{"type": "input_image", "detail": "auto", "image_url": "data:image/png;base64,AA=="}
							var content []goai.ContentBlock
							var want any
							switch mode {
							case "mixed":
								content = []goai.ContentBlock{text("a"), image, text("b"), image}
								want = "a\n" + placeholder + "\nb\n" + placeholder
								if images {
									want = []any{map[string]any{"type": "input_text", "text": "a\nb"}, wireImage, wireImage}
								}
							case "images":
								content = []goai.ContentBlock{image, image}
								want = placeholder
								if images {
									want = []any{wireImage, wireImage}
								}
							case "empty":
								want = "(no tool output)"
							case "lines":
								content = []goai.ContentBlock{text("a"), text(""), text("b")}
								want = "a\n\nb"
							case "bom":
								content = []goai.ContentBlock{text("\ufeff")}
								want = "(no tool output)"
							case "replacement":
								content = []goai.ContentBlock{text("\ufffd")}
								want = "\ufffd"
							case "placeholder":
								content = []goai.ContentBlock{image, text(placeholder), image}
								want = placeholder
								if images {
									want = []any{map[string]any{"type": "input_text", "text": placeholder}, wireImage, wireImage}
								}
							}
							conv := &goai.Context{Tools: []goai.Tool{tool}, Messages: []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{text("prompt"), image}}, {Role: goai.RoleToolResult, ToolName: tool.Name, ToolCallID: "call|item", Content: content}}}
							before, _ := json.Marshal(conv)
							token := "test"
							if provider.Api == goai.ApiOpenAICodexResponses {
								token = "x." + base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct"}}`)) + ".x"
							}
							ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer cancel()
							done := 0
							for event := range provider.Stream(ctx, model, conv, &goai.StreamOptions{APIKey: token, Transport: transport}) {
								switch e := event.(type) {
								case *goai.ErrorEvent:
									t.Error(e.Err)
								case *goai.DoneEvent:
									done++
								}
							}
							if done != 1 {
								t.Fatal("missing completion", done)
							}
							var body map[string]any
							select {
							case body = <-captured:
							case <-ctx.Done():
								t.Fatal("missing wire request")
							}
							input := body["input"].([]any)
							result := input[len(input)-1].(map[string]any)
							if result["type"] != wantType || result["call_id"] != "call" || !reflect.DeepEqual(result["output"], want) {
								t.Fatalf("tool content %#v; want %#v", result, want)
							}
							user := input[0].(map[string]any)["content"].([]any)
							if len(user) != 2 {
								t.Fatal("user image was lost", user)
							}
							second := user[1].(map[string]any)
							if images {
								if !reflect.DeepEqual(second, wireImage) {
									t.Fatal("supported user image differs", second)
								}
							} else if second["type"] != "input_text" || second["text"] != "(image omitted: model does not support images)" {
								t.Fatal("unsupported user image bypassed downgrade", second)
							}
							after, _ := json.Marshal(conv)
							if string(before) != string(after) {
								t.Fatal("wire conversion mutated caller context")
							}
						})
					}
				}
			}
		}
	}
}

func TestSanitizeSurrogatesPreservesReplacementMain(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{"a\ufffdb🙂", "a\ufffdb🙂"}, {"a\xed\xa0\x80b\xed\xbf\xbfc", "abc"}, {"a\xffb", "ab"}, {"\ufffd\xed\xbf\xbf\ufffd", "\ufffd\ufffd"}} {
		if got := goai.SanitizeSurrogates(tc.input); got != tc.want {
			t.Fatalf("sanitize %q = %q; want %q", tc.input, got, tc.want)
		}
	}
}

func TestImagePlaceholderDeduplicationMain(t *testing.T) {
	for _, role := range []goai.Role{goai.RoleUser, goai.RoleToolResult} {
		placeholder := "(image omitted: model does not support images)"
		if role == goai.RoleToolResult {
			placeholder = "(tool image omitted: model does not support images)"
		}
		text := func(s string) goai.ContentBlock { return goai.ContentBlock{Type: "text", Text: s} }
		image := goai.ContentBlock{Type: "image", Data: "AA==", MimeType: "image/png"}
		original := []goai.Message{{Role: role, Content: []goai.ContentBlock{image, text(placeholder), image, text("break"), text(placeholder), image}}}
		before, _ := json.Marshal(original)
		model := &goai.Model{Input: []string{"text"}}
		got := goai.TransformMessages(original, model)
		want := []goai.ContentBlock{text(placeholder), text("break"), text(placeholder)}
		if len(got) != 1 || !reflect.DeepEqual(got[0].Content, want) {
			t.Fatal("image/placeholder groups differ", role, got)
		}
		if again := goai.TransformMessages(got, model); !reflect.DeepEqual(again, got) {
			t.Fatal("placeholder transform is not idempotent", again)
		}
		after, _ := json.Marshal(original)
		if string(before) != string(after) {
			t.Fatal("image transform mutated caller content")
		}
		model.Input = append(model.Input, "image")
		if supported := goai.TransformMessages(original, model); !reflect.DeepEqual(supported[0].Content, original[0].Content) {
			t.Fatal("supported images/placeholders changed", supported)
		}
	}
}
