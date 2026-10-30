package openaicodex

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
)

func TestCodexTerminalStatusBothTransportsMain(t *testing.T) {
	for _, transport := range []string{"sse", "websocket"} {
		for _, reason := range []string{"max_output_tokens", "content_filter", ""} {
			t.Run(transport+"/"+reason, func(t *testing.T) {
				response := map[string]any{"type": "response.incomplete", "response": map[string]any{"id": "r", "status": "incomplete", "incomplete_details": map[string]any{"reason": reason}}}
				raw, _ := json.Marshal(response)
				var final *goai.Message
				done, failures := 0, 0
				session := t.Name()
				defer CloseOpenAICodexWebSocketSessions(session)
				send := func(event goai.Event) {
					switch e := event.(type) {
					case *goai.DoneEvent:
						done++
						final = e.Message
					case *goai.ErrorEvent:
						failures++
						final = e.Error
						if e.Err == nil {
							t.Error("missing terminal error")
						}
					}
				}
				if err := streamCodexArgumentFrames(t, transport, []string{string(raw)}, send, session); err != nil {
					t.Fatal(err)
				}
				if final == nil {
					t.Fatal("missing terminal message")
				}
				want := goai.StopReasonError
				message := "Response incomplete without a provider reason"
				rawReason := "incomplete"
				if reason != "" {
					rawReason += "." + reason
					message = "Response incomplete: " + reason
				}
				if reason == "max_output_tokens" {
					want = goai.StopReasonLength
					message = ""
				}
				if want == goai.StopReasonError {
					if failures != 1 || done != 0 {
						t.Fatal("failed output delivered as done", done, failures)
					}
					if transport == "websocket" {
						codexWebSocketSessionsMu.Lock()
						cached := cachedCodexSessionForTest(session) != nil
						codexWebSocketSessionsMu.Unlock()
						if cached {
							t.Fatal("failed output retained cached session")
						}
					}
				} else if done != 1 || failures != 0 {
					t.Fatal(done, failures)
				}
				if final.StopReason != want || final.RawStopReason != rawReason || final.ErrorMessage != message {
					t.Fatal("terminal reason lost", final)
				}
			})
		}
		t.Run(transport+"/unknown", func(t *testing.T) {
			failures, done := 0, 0
			err := streamCodexArgumentFrames(t, transport, []string{`{"type":"response.done","response":{"status":"unknown"}}`}, func(event goai.Event) {
				switch e := event.(type) {
				case *goai.ErrorEvent:
					failures++
				case *goai.DoneEvent:
					done++
					if e.Message.StopReason != goai.StopReasonStop || e.Message.RawStopReason != "" {
						t.Error("Codex adapter did not drop unknown status", e.Message)
					}
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if failures != 0 || done != 1 {
				t.Fatal("Codex status normalization changed", failures, done)
			}
		})
	}
}

func TestCodexTerminalReasoningAndUsageMain(t *testing.T) {
	for _, transport := range []string{"sse", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			frames := []string{`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"r1"}}`, `{"type":"response.reasoning_summary_text.delta","output_index":0,"delta":"trace encrypted_content"}`, `{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"r1","summary":[{"type":"summary_text","text":"trace encrypted_content"}],"encrypted_content":"","opaque":9007199254740993}}`, `{"type":"response.completed","response":{"id":"response_id","status":"completed","end_turn":false,"output":[{"type":"reasoning","id":"r1","encrypted_content":"terminal_cipher","summary":[{"type":"summary_text","text":"untrusted replacement"}]}],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12,"input_tokens_details":{"cached_tokens":8,"cache_write_tokens":4},"output_tokens_details":{"reasoning_tokens":0}}}}`}
			var final *goai.Message
			session := t.Name()
			defer CloseOpenAICodexWebSocketSessions(session)
			if err := streamCodexArgumentFrames(t, transport, frames, func(event goai.Event) {
				switch e := event.(type) {
				case *goai.DoneEvent:
					final = e.Message
				case *goai.ErrorEvent:
					t.Error(e.Err)
				}
			}, session); err != nil {
				t.Fatal(err)
			}
			if final == nil || final.EndTurn == nil || *final.EndTurn || final.ResponseID != "response_id" {
				t.Fatal("terminal metadata missing", final)
			}
			usage := final.Usage
			if usage.Input != 0 || usage.CacheRead != 8 || usage.CacheWrite != 4 || usage.Output != 2 || usage.TotalTokens != 12 || !usage.ReasoningSet || usage.Reasoning != 0 {
				t.Fatal("usage transport mismatch", usage)
			}
			block := final.Content[0]
			if block.Thinking != "trace encrypted_content" || !strings.Contains(block.ThinkingSignature, `"encrypted_content":"terminal_cipher"`) || !strings.Contains(block.ThinkingSignature, "9007199254740993") || strings.Contains(block.ThinkingSignature, "untrusted replacement") {
				t.Fatal("reasoning finalisation changed content", block)
			}
			items := buildCodexAssistantItems(*final, codexGrammarModel())
			if len(items) != 1 || items[0].(map[string]interface{})["encrypted_content"] != "terminal_cipher" {
				t.Fatal("reasoning replay missing cipher", items)
			}
			if transport == "websocket" {
				codexWebSocketSessionsMu.Lock()
				entry := cachedCodexSessionForTest(session)
				codexWebSocketSessionsMu.Unlock()
				if entry == nil {
					t.Fatal("missing cached continuation")
				}
				entry.mu.Lock()
				defer entry.mu.Unlock()
				if entry.continuation == nil || len(entry.continuation.lastResponseItems) != 1 || entry.continuation.lastResponseItems[0].(map[string]interface{})["encrypted_content"] != "terminal_cipher" {
					t.Fatal("cached reasoning missing terminal cipher")
				}
			}
		})
	}
}

func TestCodexAdapterDropsNonStringStatusMain(t *testing.T) {
	for _, status := range []string{`null`, `123`, `true`, `{}`, `[]`} {
		t.Run(status, func(t *testing.T) {
			partial := &goai.Message{}
			err := finalizeCodexResponse([]byte(`{"status":`+status+`}`), partial, codexGrammarModel(), nil)
			if err != nil || partial.StopReason != goai.StopReasonStop || partial.RawStopReason != "" {
				t.Fatal("Codex adapter leaked unrecognised status", partial, err)
			}
		})
	}
}
