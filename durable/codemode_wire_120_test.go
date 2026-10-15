package durable

import (
	"context"
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/inference/provider/openairesponses"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodemode120RawGrammarHTTPAndReopen(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiOpenAICompletions, goai.ApiOpenAIResponses} {
		t.Run(string(api), func(t *testing.T) { testCodemodeRawGrammarHTTP(t, api) })
	}
}
func TestCodemode120InterruptedHTTPFollowupReopen(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiOpenAICompletions, goai.ApiOpenAIResponses} {
		t.Run(string(api), func(t *testing.T) { testCodemodeRawGrammarHTTP(t, api, true) })
	}
}
func testCodemodeRawGrammarHTTP(t *testing.T, api goai.Api, interrupted ...bool) {
	interrupt := len(interrupted) > 0 && interrupted[0]
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		var requests, effects atomic.Int64
		followup := make(chan struct{})
		code := `text(await tools.wire_leaf({}));`
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			n := requests.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			if n%2 == 1 && (!interrupt || n == 1) {
				found := false
				for _, raw := range body["tools"].([]any) {
					tool := raw.(map[string]any)
					if tool["name"] == "codemode" {
						found = true
						if tool["type"] != "custom" || tool["format"].(map[string]any)["definition"] != strings.TrimSpace(CodemodeSourceGrammar) {
							t.Error("grammar definition", tool)
						}
					}
				}
				if !found {
					t.Error("codemode missing")
				}
				if api == goai.ApiOpenAIResponses {
					emitResponseEvent(w, map[string]any{"type": "response.output_item.added", "item": map[string]any{"type": "custom_tool_call", "id": "ctc_raw", "call_id": "raw-call", "name": "codemode"}})
					for _, chunk := range []string{code[:12], code[12:]} {
						emitResponseEvent(w, map[string]any{"type": "response.custom_tool_call_input.delta", "delta": chunk})
					}
					emitResponseEvent(w, map[string]any{"type": "response.custom_tool_call_input.done", "input": code})
					emitResponseEvent(w, map[string]any{"type": "response.output_item.done", "item": map[string]any{"type": "custom_tool_call", "id": "ctc_raw", "call_id": "raw-call", "name": "codemode", "input": code}})
					emitResponseEvent(w, map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed"}})
					return
				}
				for i, chunk := range []string{code[:12], code[12:]} {
					custom := map[string]any{"input": chunk}
					call := map[string]any{"index": 0, "type": "custom", "custom": custom}
					if i == 0 {
						call["id"] = "raw-call"
						custom["name"] = "codemode"
					}
					delta := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{call}}}}}
					bytes, _ := json.Marshal(delta)
					fmt.Fprintf(w, "data: %s\n\n", bytes)
				}
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			} else {
				foundCall, foundResult := false, false
				key := "messages"
				if api == goai.ApiOpenAIResponses {
					key = "input"
				}
				for _, raw := range body[key].([]any) {
					message := raw.(map[string]any)
					if message["type"] == "custom_tool_call" {
						foundCall = true
						if message["input"] != code {
							t.Error("responses raw replay", message)
						}
					}
					if message["type"] == "custom_tool_call_output" && strings.HasPrefix(fmt.Sprint(message["output"]), "Script completed\nWall time ") && strings.HasSuffix(fmt.Sprint(message["output"]), "Output:\nwire-leaf") {
						foundResult = true
					}
					if calls, ok := message["tool_calls"].([]any); ok {
						for _, rawCall := range calls {
							call := rawCall.(map[string]any)
							if call["type"] == "custom" {
								foundCall = true
								if _, ok := call["function"]; ok {
									t.Error("custom call has function payload")
								}
								if call["custom"].(map[string]any)["input"] != code {
									t.Error("raw code replay", call)
								}
							}
						}
					}
					if message["role"] == "tool" && strings.HasPrefix(fmt.Sprint(message["content"]), "Script completed\nWall time ") && strings.HasSuffix(fmt.Sprint(message["content"]), "Output:\nwire-leaf") {
						foundResult = true
					}
				}
				if !foundCall || !foundResult {
					t.Error("custom transcript missing", body)
				}
				if interrupt && n == 2 {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
					close(followup)
					<-r.Context().Done()
					return
				}
				if api == goai.ApiOpenAIResponses {
					emitResponseEvent(w, map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed"}})
					return
				}
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}
		}))
		defer server.Close()
		registry := NewRegistry()
		leaf := wrapRegistration("wire_leaf")
		leaf.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "wire-leaf"}, nil
		}
		if err = registry.Register(leaf); err != nil {
			t.Fatal(err)
		}
		extension, err := NewCodemodeExtension(sandbox)
		if err != nil {
			t.Fatal(err)
		}
		if err = registry.Install(&extension); err != nil {
			t.Fatal(err)
		}
		yes := true
		model := fakeModel(api)
		model.BaseURL = server.URL + "/v1"
		model.CompletionsCompat = &goai.OpenAICompletionsCompat{SupportsOpenAIGrammarTools: &yes}
		model.ResponsesCompat = &goai.OpenAIResponsesCompat{SupportsOpenAIGrammarTools: &yes}
		options := Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }, RequestOptions: func(context.Context, ModelRef) (*goai.StreamOptions, error) {
			return &goai.StreamOptions{APIKey: "fixture"}, nil
		}}
		h := openHarness(t, b.store, options)
		conv := root(t, h, ModelRef{Provider: model.Provider, ID: model.ID})
		run := func(c *ConversationHandle) {
			t.Helper()
			sub, err := c.Submit(bg, Input{Content: "script"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(bg, 10*time.Second)
			defer cancel()
			result, err := sub.Wait(ctx)
			if err != nil || result.Submission.Status != "done" {
				t.Fatal(result, err)
			}
		}
		if interrupt {
			sub, err := conv.Submit(bg, Input{Content: "script"})
			if err != nil {
				t.Fatal(err)
			}
			awaitTaskSignal(t, followup)
			if effects.Load() != 1 {
				t.Fatal("nested effect not settled", effects.Load())
			}
			if err = h.Close(bg); err != nil {
				t.Fatal(err)
			}
			next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
			if err = next.Resume(bg); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(bg, 10*time.Second)
			defer cancel()
			reopened, err := next.Submission(ctx, sub.ID())
			if err != nil || reopened == nil {
				t.Fatal("submission missing", err)
			}
			result, err := reopened.Wait(ctx)
			if err != nil || result.Submission.Status != "done" || requests.Load() != 3 || effects.Load() != 1 {
				t.Fatal(result, err, requests.Load(), effects.Load())
			}
			conversation, err := next.Conversation(ctx, conv.ID())
			if err != nil {
				t.Fatal(err)
			}
			view, err := conversation.ContextView(ctx, 0)
			if err != nil {
				t.Fatal(err)
			}
			calls, results := 0, 0
			for _, message := range view.Messages {
				for _, block := range message.Content {
					if block.Type == "toolCall" && block.Name == "codemode" {
						calls++
					}
				}
				if message.Role == goai.RoleToolResult && message.ToolName == "codemode" {
					results++
				}
			}
			if calls != 1 || results != 1 {
				t.Fatal("duplicate script transcript", calls, results)
			}
			return
		}
		run(conv)
		view, err := conv.ContextView(bg, 0)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, message := range view.Messages {
			for _, block := range message.Content {
				if block.Type == "toolCall" && block.Name == "codemode" {
					found = true
					if block.Arguments["code"] != code {
						t.Fatal("persisted source", block)
					}
				}
			}
		}
		if !found {
			t.Fatal("source missing")
		}
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		recovered, err := next.Conversation(bg, conv.ID())
		if err != nil {
			t.Fatal(err)
		}
		run(recovered)
		if requests.Load() != 4 || effects.Load() != 2 {
			t.Fatal(requests.Load(), effects.Load())
		}
		after, err := recovered.ContextView(bg, 0)
		if err != nil || !strings.Contains(ReplayPromptSections(after.Messages)[0].Text, "wire_leaf") {
			t.Fatal("reopened prompt", err)
		}
	})
}

func emitResponseEvent(w http.ResponseWriter, event map[string]any) {
	data, _ := json.Marshal(event)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], data)
}
