package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

const opaqueBaseV170 = "https://api.individual.githubcopilot.com"
const opaqueDetailV170 = `{"type":"reasoning.encrypted","id":"call_1","data":"encrypted"}`

type opaqueFixtureV170 struct {
	sets     [][]string
	requests [][]map[string]any
	onEvent  func(any, *goai.Model) error
}

func opaqueModelV170() *goai.Model {
	return &goai.Model{ID: "gemini-test", Name: "Gemini Test", Api: goai.ApiOpenAICompletions,
		Provider: goai.ProviderGitHubCopilot, BaseURL: opaqueBaseV170, Reasoning: true,
		Input: []string{"text"}, ContextWindow: 100_000, MaxTokens: 4096}
}

func opaqueChunkV170(delta, finish string) string {
	if finish == "" {
		finish = "null"
	} else {
		finish = fmt.Sprintf("%q", finish)
	}
	return fmt.Sprintf("data: {\"id\":\"chatcmpl-test\",\"model\":\"gemini-test\",\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":%s}]}\n\n", delta, finish)
}

func opaqueToolV170(index int, id string) string {
	return opaqueChunkV170(fmt.Sprintf(`{"tool_calls":[{"index":%d,"id":%q,"type":"function","function":{"name":"read","arguments":"{\"path\":\"README.md\"}"}}]}`, index, id), "")
}

func (f *opaqueFixtureV170) run(t *testing.T, messages ...goai.Message) (*goai.Message, []string, error) {
	t.Helper()
	zero := 0
	opts := &goai.StreamOptions{APIKey: "test", MaxRetries: &zero, OnProviderStreamEvent: f.onEvent, HTTPTransport: promptCacheTransportV160(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		n := len(f.requests)
		if n >= len(f.sets) || r.Method != http.MethodPost || r.URL.String() != opaqueBaseV170+"/chat/completions" {
			return nil, fmt.Errorf("unexpected opaque fixture request %d: %s %s", n, r.Method, r.URL)
		}
		var wire struct {
			Messages []map[string]any `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			return nil, err
		}
		f.requests = append(f.requests, wire.Messages)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(strings.Join(f.sets[n], "")))}, nil
	})}
	input := &goai.Context{Messages: messages, Tools: []goai.Tool{{Name: "read", Description: "Read a file", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)}}}
	var final *goai.Message
	var thinkingDeltas []string
	var failure error
	for event := range goai.Stream(t.Context(), opaqueModelV170(), input, opts) {
		switch e := event.(type) {
		case *goai.ThinkingDeltaEvent:
			thinkingDeltas = append(thinkingDeltas, e.Delta)
		case *goai.DoneEvent:
			final = e.Message
		case *goai.ErrorEvent:
			final, failure = e.Error, e.Err
		}
	}
	if final == nil {
		t.Fatalf("fixture had no assistant result (requests=%d): %v", len(f.requests), failure)
	}
	return final, thinkingDeltas, failure
}

func (f *opaqueFixtureV170) assistant(t *testing.T, request int) map[string]any {
	t.Helper()
	if len(f.requests) != request+1 {
		t.Fatalf("requests = %d, want %d", len(f.requests), request+1)
	}
	for _, msg := range f.requests[request] {
		if msg["role"] == "assistant" {
			return msg
		}
	}
	t.Fatal("captured request has no assistant message")
	return nil
}

func opaqueRequiredFieldsV170(t *testing.T, got, want map[string]any) {
	t.Helper()
	for key, wantValue := range want {
		value, present := got[key]
		if !present || !reflect.DeepEqual(value, wantValue) {
			t.Errorf("payload %s = %#v (present=%t), want %#v", key, value, present, wantValue)
		}
	}
}

func opaqueAbsentFieldsV170(t *testing.T, got map[string]any, fields ...string) {
	t.Helper()
	for _, key := range fields {
		if value, present := got[key]; present {
			t.Errorf("unexpected payload %s = %#v", key, value)
		}
	}
}

func opaqueThinkingV170(t *testing.T, message *goai.Message, thinking, signature string) {
	t.Helper()
	if len(message.Content) == 0 {
		t.Fatal("missing first thinking block")
	}
	encoded, err := json.Marshal(message.Content[0])
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"type": "thinking", "thinking": thinking, "thinkingSignature": signature}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("first block = %#v, want %#v", got, want)
	}
}

func TestSourceV170CompletionsOpaqueReasoning(t *testing.T) {
	toolDone := opaqueChunkV170(`{}`, "tool_calls")
	ok := []string{opaqueChunkV170(`{"content":"ok"}`, "stop")}
	t.Run("after-text", func(t *testing.T) {
		f := opaqueFixtureV170{sets: [][]string{{
			opaqueChunkV170(`{"role":"assistant","reasoning_text":"Read the readme"}`, ""),
			opaqueChunkV170(`{"reasoning_text":" first."}`, ""),
			opaqueChunkV170(`{"role":"assistant","reasoning_opaque":"opaque-1"}`, ""),
			opaqueToolV170(0, "call_1"), toolDone,
		}, ok}}
		first, _, _ := f.run(t)
		opaqueThinkingV170(t, first, "Read the readme first.", `{"field":"reasoning_text","reasoning_opaque":"opaque-1"}`)
		f.run(t, *first)
		opaqueRequiredFieldsV170(t, f.assistant(t, 1), map[string]any{"content": nil, "reasoning_text": "Read the readme first.", "reasoning_opaque": "opaque-1"})
	})
	t.Run("latest", func(t *testing.T) {
		f := opaqueFixtureV170{sets: [][]string{{
			opaqueChunkV170(`{"reasoning_text":"Read the readme."}`, ""),
			opaqueChunkV170(`{"reasoning_opaque":"opaque-first"}`, ""),
			opaqueToolV170(0, "call_1"),
			opaqueChunkV170(`{"reasoning_text":" Then the manifest."}`, ""),
			opaqueChunkV170(`{"reasoning_opaque":"opaque-second"}`, ""),
			opaqueToolV170(1, "call_2"), toolDone,
		}, ok}}
		first, _, _ := f.run(t)
		f.run(t, *first)
		opaqueRequiredFieldsV170(t, f.assistant(t, 1), map[string]any{"reasoning_text": "Read the readme. Then the manifest.", "reasoning_opaque": "opaque-second"})
	})
	t.Run("opaque-only", func(t *testing.T) {
		f := opaqueFixtureV170{sets: [][]string{{
			opaqueChunkV170(`{"content":"Checking.","reasoning_opaque":"opaque-only"}`, ""),
			opaqueToolV170(0, "call_1"), toolDone,
		}, ok}}
		first, deltas, _ := f.run(t)
		var blocks []string
		for _, c := range first.Content {
			blocks = append(blocks, c.Type)
		}
		if !slices.Equal(blocks, []string{"thinking", "text", "toolCall"}) {
			t.Errorf("block order = %v, want [thinking text toolCall]", blocks)
		}
		if len(deltas) != 0 {
			t.Errorf("opaque-only reasoning produced text deltas: %v", deltas)
		}
		f.run(t, *first)
		payload := f.assistant(t, 1)
		opaqueRequiredFieldsV170(t, payload, map[string]any{"content": "Checking.", "reasoning_opaque": "opaque-only"})
		opaqueAbsentFieldsV170(t, payload, "reasoning_text")
	})
	t.Run("same-chunk", func(t *testing.T) {
		f := opaqueFixtureV170{sets: [][]string{{
			opaqueChunkV170(`{"reasoning_text":"Plan.","reasoning_opaque":"opaque-1"}`, ""),
			opaqueToolV170(0, "call_1"), toolDone,
		}, ok}}
		first, _, _ := f.run(t)
		f.run(t, *first)
		opaqueRequiredFieldsV170(t, f.assistant(t, 1), map[string]any{"reasoning_text": "Plan.", "reasoning_opaque": "opaque-1"})
	})
	t.Run("details-win", func(t *testing.T) {
		f := opaqueFixtureV170{sets: [][]string{{
			opaqueChunkV170(`{"reasoning_text":"Plan.","reasoning_details":[`+opaqueDetailV170+`]}`, ""),
			opaqueChunkV170(`{"reasoning_opaque":"opaque-1"}`, ""),
			opaqueToolV170(0, "call_1"), toolDone,
		}, ok}}
		first, _, _ := f.run(t)
		f.run(t, *first)
		payload := f.assistant(t, 1)
		var details []any
		if err := json.Unmarshal([]byte("["+opaqueDetailV170+"]"), &details); err != nil {
			t.Fatal(err)
		}
		opaqueRequiredFieldsV170(t, payload, map[string]any{"reasoning_details": details})
		opaqueAbsentFieldsV170(t, payload, "reasoning_opaque", "reasoning_text")
	})
	t.Run("unfinished-stream", func(t *testing.T) {
		f := opaqueFixtureV170{sets: [][]string{{
			opaqueChunkV170(`{"reasoning_text":"Plan."}`, ""), opaqueChunkV170(`{"reasoning_opaque":"opaque-1"}`, ""),
		}}}
		first, _, err := f.run(t)
		if first.StopReason != goai.StopReasonError {
			t.Errorf("unfinished stop reason = %q, want error", first.StopReason)
		}
		if !strings.Contains(first.ErrorMessage, "Stream ended without finish_reason") {
			t.Errorf("unfinished stream diagnostic = %q (%v)", first.ErrorMessage, err)
		}
		opaqueThinkingV170(t, first, "Plan.", `{"field":"reasoning_text","reasoning_opaque":"opaque-1"}`)
	})
	t.Run("other-model", func(t *testing.T) {
		f := opaqueFixtureV170{sets: [][]string{{
			opaqueChunkV170(`{"reasoning_text":"Plan."}`, ""), opaqueChunkV170(`{"reasoning_opaque":"opaque-1"}`, ""),
			opaqueChunkV170(`{"content":"Done."}`, "stop"),
		}, ok}}
		first, _, _ := f.run(t)
		other := *first
		other.Model = "other-model"
		f.run(t, other)
		payload := f.assistant(t, 1)
		opaqueRequiredFieldsV170(t, payload, map[string]any{"content": "Done."})
		opaqueAbsentFieldsV170(t, payload, "reasoning_opaque", "reasoning_text")
	})
}

func TestCompletionsStoredOpaqueReasoningV170(t *testing.T) {
	for _, row := range []struct {
		name, signature, thinking string
		want                      map[string]any
		absent                    []string
	}{
		{"with-field", `{"field":"reasoning_text","reasoning_opaque":"o"}`, "Plan.", map[string]any{"reasoning_opaque": "o", "reasoning_text": "Plan."}, nil},
		{"without-field", `{"reasoning_opaque":"o"}`, "", map[string]any{"reasoning_opaque": "o"}, []string{"reasoning_text"}},
		{"invalid-field", `{"field":"content","reasoning_opaque":"o"}`, "Plan.", nil, []string{"reasoning_opaque", "reasoning_text"}},
		{"explicit-null-field", `{"field":null,"reasoning_opaque":"o"}`, "Plan.", nil, []string{"reasoning_opaque", "reasoning_text"}},
		{"empty-opaque", `{"reasoning_opaque":""}`, "Plan.", nil, []string{"reasoning_opaque", "reasoning_text"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			f := opaqueFixtureV170{sets: [][]string{{opaqueChunkV170(`{"content":"ok"}`, "stop")}}}
			model := opaqueModelV170()
			f.run(t, goai.Message{Role: goai.RoleAssistant, Provider: model.Provider, Api: model.Api, Model: model.ID, StopReason: goai.StopReasonStop,
				Content: []goai.ContentBlock{{Type: "thinking", Thinking: row.thinking, ThinkingSignature: row.signature}, {Type: "text", Text: "Done."}}})
			payload := f.assistant(t, 0)
			opaqueRequiredFieldsV170(t, payload, row.want)
			opaqueAbsentFieldsV170(t, payload, row.absent...)
		})
	}
}

func TestCompletionsOpaqueReasoningBoundariesV170(t *testing.T) {
	t.Run("provider-event-refusal-keeps-metadata", func(t *testing.T) {
		f := opaqueFixtureV170{sets: [][]string{{
			opaqueChunkV170(`{"reasoning_text":"Plan.","reasoning_opaque":"opaque-1"}`, ""),
			opaqueChunkV170(`{"content":"unobserved"}`, "stop"),
		}}}
		refusal := errors.New("fixture refused the second provider event")
		seen := 0
		f.onEvent = func(any, *goai.Model) error {
			seen++
			if seen == 2 {
				return refusal
			}
			return nil
		}
		first, _, err := f.run(t)
		if !errors.Is(err, refusal) || seen != 2 {
			t.Fatalf("provider event error = %v, events = %d", err, seen)
		}
		opaqueThinkingV170(t, first, "Plan.", `{"field":"reasoning_text","reasoning_opaque":"opaque-1"}`)
	})
	t.Run("wrong-wire-type-keeps-visible-text", func(t *testing.T) {
		f := opaqueFixtureV170{sets: [][]string{{opaqueChunkV170(`{"content":"Done.","reasoning_opaque":7}`, "stop")}}}
		first, deltas, err := f.run(t)
		if err != nil || len(first.Content) != 1 || first.Content[0].Text != "Done." || len(deltas) != 0 {
			t.Fatalf("malformed opaque metadata lost visible text or produced thinking: %+v (%v, %v)", first, err, deltas)
		}
	})
}
