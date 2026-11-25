package openaicodex

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	goai "github.com/rcarmo/go-ai"
)

type codexTerminalTransportV160 func(*http.Request) (*http.Response, error)

func (f codexTerminalTransportV160) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// An open fixture never supplies EOF. The parser is free to read ahead, but
// that read can only finish on body close or on the virtual test deadline.
type codexTerminalBodyV160 struct {
	reader   *strings.Reader
	open     bool
	deadline context.Context
	closed   chan struct{}
	closes   atomic.Int32
	eof      atomic.Bool
}

func (b *codexTerminalBodyV160) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	if n != 0 || err != io.EOF {
		return n, err
	}
	if !b.open {
		b.eof.Store(true)
		return 0, io.EOF
	}
	select {
	case <-b.closed:
		return 0, io.ErrClosedPipe
	case <-b.deadline.Done():
		return 0, b.deadline.Err()
	}
}

func (b *codexTerminalBodyV160) Close() error {
	if b.closes.Add(1) == 1 {
		close(b.closed)
	}
	return nil
}

// Source: pi v1.6.0 openai-codex-stream.test.ts:287–315,317–376,433–491.
func TestSourceV160CodexSSETerminalFraming(t *testing.T) {
	noEndTurn := false
	for _, tc := range []struct {
		name, status string
		open         bool
		includeDone  bool
		reason       goai.StopReason
		endTurn      *bool
	}{
		{"trimmed-eof", "completed", false, false, goai.StopReasonStop, nil},
		{"completed-open", "completed", true, true, goai.StopReasonStop, &noEndTurn},
		{"incomplete-open", "incomplete", true, false, goai.StopReasonLength, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const guard = time.Second
				ctx, cancel := context.WithTimeout(t.Context(), guard)
				defer cancel()
				terminalType := "response." + tc.status
				incomplete := "null"
				if tc.status == "incomplete" {
					incomplete = `{"reason":"max_output_tokens"}`
				}
				end := ""
				if tc.endTurn != nil {
					end = `,"end_turn":false`
				}
				frames := []string{
					`data: {"type":"response.output_item.added","item":{"type":"message","id":"msg_1","role":"assistant","status":"in_progress","content":[]}}`,
					`data: {"type":"response.content_part.added","part":{"type":"output_text","text":""}}`,
					`data: {"type":"response.output_text.delta","delta":"Hello"}`,
					`data: {"type":"response.output_item.done","item":{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Hello"}]}}`,
					fmt.Sprintf(`data: {"type":%q,"response":{"status":%q%s,"incomplete_details":%s,"usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8,"input_tokens_details":{"cached_tokens":0}}}}`, terminalType, tc.status, end, incomplete),
				}
				if tc.includeDone {
					frames = append(frames, "data: [DONE]")
				}
				wire := strings.Join(frames, "\n\n")
				if tc.open {
					wire += "\n\n"
				}
				body := &codexTerminalBodyV160{reader: strings.NewReader(wire), open: tc.open, deadline: ctx, closed: make(chan struct{})}
				defer func() {
					if body.closes.Load() == 0 {
						// On failure, let a leaked HTTP client timer and any
						// blocked read exit before leaving the virtual bubble.
						time.Sleep(guard)
					}
				}()
				token := accountJWT("acc_test")
				requests := 0
				transport := codexTerminalTransportV160(func(r *http.Request) (*http.Response, error) {
					defer r.Body.Close()
					requests++
					if requests != 1 || r.Method != http.MethodPost || r.URL.String() != "https://chatgpt.com/backend-api/codex/responses" ||
						!slices.Equal(r.Header.Values("Authorization"), []string{"Bearer " + token}) ||
						!slices.Equal(r.Header.Values("ChatGPT-Account-Id"), []string{"acc_test"}) {
						return nil, fmt.Errorf("unexpected Codex scripted request %d: %s %s or synthetic authorization", requests, r.Method, r.URL)
					}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}, nil
				})
				model := &goai.Model{ID: "gpt-5.1-codex", Name: "GPT-5.1 Codex", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses,
					BaseURL: "https://chatgpt.com/backend-api", Reasoning: true, Input: []string{"text"}, ContextWindow: 400000, MaxTokens: 128000}
				transcript := &goai.Context{SystemPrompt: "You are a helpful assistant.", Messages: []goai.Message{goai.UserMessage("Say hello")}}
				var rawTypes, events []string
				var done *goai.DoneEvent
				for event := range goai.Stream(ctx, model, transcript, &goai.StreamOptions{APIKey: token, Transport: goai.TransportSSE,
					RetryConfig: &goai.RetryConfig{RequestTimeout: guard}, HTTPTransport: transport,
					OnProviderStreamEvent: func(value any, _ *goai.Model) error {
						raw, _ := value.(map[string]any)
						kind, _ := raw["type"].(string)
						rawTypes = append(rawTypes, kind)
						return nil
					},
				}) {
					switch e := event.(type) {
					case *goai.StartEvent:
						events = append(events, "start")
					case *goai.TextStartEvent:
						events = append(events, fmt.Sprintf("text_start:%d", e.ContentIndex))
					case *goai.TextDeltaEvent:
						events = append(events, fmt.Sprintf("text_delta:%d:%s", e.ContentIndex, e.Delta))
					case *goai.TextEndEvent:
						events = append(events, fmt.Sprintf("text_end:%d:%s", e.ContentIndex, e.Content))
					case *goai.DoneEvent:
						events = append(events, "done")
						done = e
					case *goai.ErrorEvent:
						t.Errorf("terminal framing emitted error: %v", e.Err)
					default:
						t.Errorf("unexpected public SSE event %T", event)
					}
				}
				if !slices.Equal(events, []string{"start", "text_start:0", "text_delta:0:Hello", "text_end:0:Hello", "done"}) {
					t.Errorf("public event sequence = %v", events)
				}
				wantRaw := []string{"response.output_item.added", "response.content_part.added", "response.output_text.delta", "response.output_item.done", terminalType}
				if !slices.Equal(rawTypes, wantRaw) {
					t.Errorf("raw event sequence = %v; want %v", rawTypes, wantRaw)
				}
				select {
				case <-body.closed:
				default:
					t.Error("response body not closed at stream completion")
				}
				if requests != 1 || body.closes.Load() != 1 || body.eof.Load() != !tc.open || ctx.Err() != nil {
					t.Errorf("requests=%d closes=%d eof=%v deadline=%v; want one request and close, eof=%v, no deadline", requests, body.closes.Load(), body.eof.Load(), ctx.Err(), !tc.open)
				}
				if done == nil || done.Message == nil || done.Reason != tc.reason || done.Message.StopReason != tc.reason {
					t.Fatalf("terminal event = %+v; want %q", done, tc.reason)
				}
				m := done.Message
				if len(m.Content) != 1 || m.Content[0].Type != "text" || m.Content[0].Text != "Hello" || m.ErrorMessage != "" || !reflect.DeepEqual(m.EndTurn, tc.endTurn) {
					t.Errorf("terminal content/endTurn = %+v/%v; err=%q", m.Content, m.EndTurn, m.ErrorMessage)
				}
				if m.Usage == nil || m.Usage.Input != 5 || m.Usage.Output != 3 || m.Usage.TotalTokens != 8 || m.Usage.CacheRead != 0 {
					t.Errorf("terminal usage = %+v; want 5 input, 3 output, 8 total, 0 cached", m.Usage)
				}
			})
		})
	}
}
