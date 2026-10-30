package openaicodex

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/transports/websocket"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodexStartedMissingContinuationRecoveryMain(t *testing.T) {
	for _, mode := range []string{"created", "text", "complete_tool", "partial_tool", "twice", "response_failed", "retry_transport", "cancel_retry"} {
		t.Run(mode, func(t *testing.T) {
			var attempts, sse atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
					sse.Add(1)
					http.Error(w, "unexpected SSE", 400)
					return
				}
				attempt := attempts.Add(1)
				if mode == "retry_transport" && attempt == 2 {
					http.Error(w, "retry dial rejected", 400)
					return
				}
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.CloseNow()
				ctx, cancel := context.WithTimeout(r.Context(), 7*time.Second)
				defer cancel()
				if _, data, err := conn.Read(ctx); err != nil {
					t.Error(err)
					return
				} else {
					var body map[string]interface{}
					_ = json.Unmarshal(data, &body)
					if _, exists := body["previous_response_id"]; exists {
						t.Error("retry did not use full prepared context", body)
					}
				}
				write := func(frame string) bool { return conn.Write(ctx, websocket.MessageText, []byte(frame)) == nil }
				if !write(`{"type":"response.created","response":{"id":"attempt"}}`) {
					return
				}
				if attempt == 1 {
					switch mode {
					case "complete_tool", "partial_tool":
						if !write(`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"i","call_id":"c","name":"leaf"}}`) {
							return
						}
						if !write(`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"x\":\"\udfff\"}"}`) {
							return
						}
						if mode == "complete_tool" {
							if !write(`{"type":"response.function_call_arguments.done","output_index":0,"arguments":"{\"x\":\"\udfff\"}"}`) {
								return
							}
							if !write(`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call"}}`) {
								return
							}
						}
					default:
						if mode != "created" {
							if !write(`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"old"}}`) {
								return
							}
							if !write(`{"type":"response.output_text.delta","output_index":0,"delta":"prior"}`) {
								return
							}
						}
					}
					frame := `{"type":"error","code":"previous_response_not_found","message":"missing"}`
					if mode == "response_failed" {
						frame = `{"type":"response.failed","response":{"status":"failed","error":{"code":"previous_response_not_found","message":"missing"}}}`
					}
					if !write(frame) {
						return
					}
				} else {
					if mode == "twice" {
						if !write(`{"type":"error","code":"previous_response_not_found","message":"again"}`) {
							return
						}
					} else {
						if mode == "text" {
							if !write(`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"new"}}`) {
								return
							}
							if !write(`{"type":"response.output_text.delta","output_index":0,"delta":"next"}`) {
								return
							}
							if !write(`{"type":"response.output_item.done","output_index":0,"item":{"type":"message"}}`) {
								return
							}
						}
						if !write(`{"type":"response.completed","response":{"id":"final","status":"completed","output":[]}}`) {
							return
						}
					}
				}
				_, _, _ = conn.Read(ctx)
			}))
			defer server.Close()
			session := t.Name()
			defer ResetOpenAICodexWebSocketDebugStats(session)
			defer CloseOpenAICodexWebSocketSessions(session)
			model := codexGrammarModel()
			model.BaseURL = server.URL
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			zero := 0
			payloads, created := 0, 0
			opts := &goai.StreamOptions{APIKey: payloadJWT, Transport: goai.TransportAuto, SessionID: session, MaxRetries: &zero, OnPayload: func(value interface{}, _ *goai.Model) (interface{}, error) { payloads++; return value, nil }, OnProviderStreamEvent: func(value interface{}, _ *goai.Model) error {
				if value.(map[string]interface{})["type"] == "response.created" {
					created++
					if mode == "cancel_retry" && created == 2 {
						cancel()
					}
				}
				return nil
			}}
			starts, done, failures, toolEnds := 0, 0, 0, 0
			var final *goai.Message
			var failure *goai.ErrorEvent
			var progress *goai.Message
			indexes := []int{}
			for event := range streamCodex(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, opts) {
				switch e := event.(type) {
				case *goai.StartEvent:
					starts++
				case *goai.DoneEvent:
					done++
					final = e.Message
				case *goai.ErrorEvent:
					failures++
					failure = e
				case *goai.TextStartEvent:
					indexes = append(indexes, e.ContentIndex)
				case *goai.TextDeltaEvent:
					if progress == nil {
						progress = e.Partial
					}
				case *goai.ToolCallEndEvent:
					toolEnds++
				}
			}
			wantDone, wantErrors := 1, 0
			if mode == "partial_tool" || mode == "twice" || mode == "retry_transport" || mode == "cancel_retry" {
				wantDone, wantErrors = 0, 1
			}
			if starts != 1 || done != wantDone || failures != wantErrors || attempts.Load() != 2 || payloads != 1 || sse.Load() != 0 {
				t.Fatal("started recovery replay/start boundary", starts, done, failures, attempts.Load(), payloads, sse.Load())
			}
			if final == nil {
				if failure == nil || failure.Error == nil {
					t.Fatal("retry failure lost existing output", failure)
				}
				final = failure.Error
			}
			if mode == "text" {
				if !reflect.DeepEqual(indexes, []int{0, 1}) || len(final.Content) != 2 || final.Content[0].Text != "prior" || final.Content[1].Text != "next" {
					t.Fatal("retry discarded/reindexed prior output", indexes, final)
				}
			}
			if mode == "complete_tool" || mode == "partial_tool" {
				if len(final.Content) != 1 || !final.Content[0].HasStreamingArguments() {
					t.Fatal("typed tool ownership lost", final)
				}
				value, ok := final.Content[0].Arguments["x"].(goai.UTF16String)
				if !ok || !reflect.DeepEqual(value.CodeUnits(), []uint16{0xdfff}) {
					t.Fatal("tool argument units lost", final.Content[0])
				}
				raw, err := json.Marshal(final)
				if err != nil {
					t.Fatal(err)
				}
				var restored goai.Message
				if err = json.Unmarshal(raw, &restored); err != nil {
					t.Fatal(err)
				}
				if err = goai.RestoreStreamingArgumentsJSON(raw, &restored, false); err != nil {
					t.Fatal(err)
				}
				if !restored.Content[0].HasStreamingArguments() || !reflect.DeepEqual(restored.Content[0].Arguments, final.Content[0].Arguments) {
					t.Fatal("recovered tool persistence changed")
				}
				if mode == "complete_tool" && toolEnds != 1 {
					t.Fatal("completed tool duplicated", toolEnds)
				}
				if mode == "partial_tool" && !strings.Contains(failure.Err.Error(), "did not complete") {
					t.Fatal("unfinished old tool escaped completion guard", failure)
				}
			}
			if mode == "cancel_retry" && failure.Reason != goai.StopReasonAborted {
				t.Fatal(failure)
			}
			if (mode == "twice" || mode == "retry_transport" || mode == "cancel_retry" || mode == "response_failed") && (len(final.Content) != 1 || final.Content[0].Text != "prior") {
				t.Fatal("exhausted retry lost output", final)
			}
			if progress != nil && (progress.ErrorMessage != "" || progress.StopReason == goai.StopReasonError) {
				t.Fatal("retry mutated detached prior snapshot", progress)
			}
			stats := GetOpenAICodexWebSocketDebugStats(session)
			wantNetwork := 0
			if mode == "retry_transport" {
				wantNetwork = 1
				if len(final.Diagnostics) != 1 || final.Diagnostics[0].Details["eventsEmitted"] != true {
					t.Fatal("late retry transport diagnostics lost", final)
				}
			}
			if stats == nil || stats.WebSocketFailures != wantNetwork || stats.SSEFallbacks != 0 {
				t.Fatal("semantic retry counted as fallback/network", stats)
			}
		})
	}
}
