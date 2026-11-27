package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

const opaqueBase = "https://api.individual.githubcopilot.com"
const opaqueDetail = `{"type":"reasoning.encrypted","id":"call_1","data":"encrypted"}`

type opaqueFixture struct {
	sets     [][]string
	requests [][]map[string]any
	onEvent  func(any, *goai.Model) error
}

func opaqueModel() *goai.Model {
	return &goai.Model{ID: "gemini-test", Name: "Gemini Test", Api: goai.ApiOpenAICompletions,
		Provider: goai.ProviderGitHubCopilot, BaseURL: opaqueBase, Reasoning: true,
		Input: []string{"text"}, ContextWindow: 100_000, MaxTokens: 4096}
}

func opaqueChunk(delta, finish string) string {
	if finish == "" {
		finish = "null"
	} else {
		finish = fmt.Sprintf("%q", finish)
	}
	return fmt.Sprintf("data: {\"id\":\"chatcmpl-test\",\"model\":\"gemini-test\",\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":%s}]}\n\n", delta, finish)
}

func opaqueTool(index int, id string) string {
	return opaqueChunk(fmt.Sprintf(`{"tool_calls":[{"index":%d,"id":%q,"type":"function","function":{"name":"read","arguments":"{\"path\":\"README.md\"}"}}]}`, index, id), "")
}

func (f *opaqueFixture) run(t *testing.T, messages ...goai.Message) (*goai.Message, []string, error) {
	t.Helper()
	zero := 0
	opts := &goai.StreamOptions{APIKey: "test", MaxRetries: &zero, OnProviderStreamEvent: f.onEvent, HTTPTransport: openaiTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		n := len(f.requests)
		if n >= len(f.sets) || r.Method != http.MethodPost || r.URL.String() != opaqueBase+"/chat/completions" {
			return nil, fmt.Errorf("unexpected opaque fixture request %d: %s %s", n, r.Method, r.URL)
		}
		var wire struct {
			Messages []map[string]any `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			return nil, err
		}
		f.requests = append(f.requests, wire.Messages)
		return successfulSSETestResponse(nil, strings.Join(f.sets[n], "")), nil
	})}
	input := &goai.Context{Messages: messages, Tools: []goai.Tool{{Name: "read", Description: "Read a file", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)}}}
	var final *goai.Message
	var thinkingDeltas []string
	var failure error
	for event := range goai.Stream(t.Context(), opaqueModel(), input, opts) {
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

func (f *opaqueFixture) assistant(t *testing.T, request int) map[string]any {
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

func opaqueRequiredFields(t *testing.T, got, want map[string]any) {
	t.Helper()
	for key, wantValue := range want {
		value, present := got[key]
		if !present || !reflect.DeepEqual(value, wantValue) {
			t.Errorf("payload %s = %#v (present=%t), want %#v", key, value, present, wantValue)
		}
	}
}

func opaqueAbsentFields(t *testing.T, got map[string]any, fields ...string) {
	t.Helper()
	for _, key := range fields {
		if value, present := got[key]; present {
			t.Errorf("unexpected payload %s = %#v", key, value)
		}
	}
}

func opaqueThinking(t *testing.T, message *goai.Message, thinking, signature string) {
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

func TestCompletionsOpaqueReasoning(t *testing.T) {
	toolDone := opaqueChunk(`{}`, "tool_calls")
	ok := []string{opaqueChunk(`{"content":"ok"}`, "stop")}
	t.Run("after-text", func(t *testing.T) {
		f := opaqueFixture{sets: [][]string{{
			opaqueChunk(`{"role":"assistant","reasoning_text":"Read the readme"}`, ""),
			opaqueChunk(`{"reasoning_text":" first."}`, ""),
			opaqueChunk(`{"role":"assistant","reasoning_opaque":"opaque-1"}`, ""),
			opaqueTool(0, "call_1"), toolDone,
		}, ok}}
		first, _, _ := f.run(t)
		opaqueThinking(t, first, "Read the readme first.", `{"field":"reasoning_text","reasoning_opaque":"opaque-1"}`)
		f.run(t, *first)
		opaqueRequiredFields(t, f.assistant(t, 1), map[string]any{"content": nil, "reasoning_text": "Read the readme first.", "reasoning_opaque": "opaque-1"})
	})
	t.Run("latest", func(t *testing.T) {
		f := opaqueFixture{sets: [][]string{{
			opaqueChunk(`{"reasoning_text":"Read the readme."}`, ""),
			opaqueChunk(`{"reasoning_opaque":"opaque-first"}`, ""),
			opaqueTool(0, "call_1"),
			opaqueChunk(`{"reasoning_text":" Then the manifest."}`, ""),
			opaqueChunk(`{"reasoning_opaque":"opaque-second"}`, ""),
			opaqueTool(1, "call_2"), toolDone,
		}, ok}}
		first, _, _ := f.run(t)
		f.run(t, *first)
		opaqueRequiredFields(t, f.assistant(t, 1), map[string]any{"reasoning_text": "Read the readme. Then the manifest.", "reasoning_opaque": "opaque-second"})
	})
	t.Run("opaque-only", func(t *testing.T) {
		f := opaqueFixture{sets: [][]string{{
			opaqueChunk(`{"content":"Checking.","reasoning_opaque":"opaque-only"}`, ""),
			opaqueTool(0, "call_1"), toolDone,
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
		opaqueRequiredFields(t, payload, map[string]any{"content": "Checking.", "reasoning_opaque": "opaque-only"})
		opaqueAbsentFields(t, payload, "reasoning_text")
	})
	t.Run("same-chunk", func(t *testing.T) {
		f := opaqueFixture{sets: [][]string{{
			opaqueChunk(`{"reasoning_text":"Plan.","reasoning_opaque":"opaque-1"}`, ""),
			opaqueTool(0, "call_1"), toolDone,
		}, ok}}
		first, _, _ := f.run(t)
		f.run(t, *first)
		opaqueRequiredFields(t, f.assistant(t, 1), map[string]any{"reasoning_text": "Plan.", "reasoning_opaque": "opaque-1"})
	})
	t.Run("details-win", func(t *testing.T) {
		f := opaqueFixture{sets: [][]string{{
			opaqueChunk(`{"reasoning_text":"Plan.","reasoning_details":[`+opaqueDetail+`]}`, ""),
			opaqueChunk(`{"reasoning_opaque":"opaque-1"}`, ""),
			opaqueTool(0, "call_1"), toolDone,
		}, ok}}
		first, _, _ := f.run(t)
		f.run(t, *first)
		payload := f.assistant(t, 1)
		var details []any
		if err := json.Unmarshal([]byte("["+opaqueDetail+"]"), &details); err != nil {
			t.Fatal(err)
		}
		opaqueRequiredFields(t, payload, map[string]any{"reasoning_details": details})
		opaqueAbsentFields(t, payload, "reasoning_opaque", "reasoning_text")
	})
	t.Run("unfinished-stream", func(t *testing.T) {
		f := opaqueFixture{sets: [][]string{{
			opaqueChunk(`{"reasoning_text":"Plan."}`, ""), opaqueChunk(`{"reasoning_opaque":"opaque-1"}`, ""),
		}}}
		first, _, err := f.run(t)
		if first.StopReason != goai.StopReasonError {
			t.Errorf("unfinished stop reason = %q, want error", first.StopReason)
		}
		if !strings.Contains(first.ErrorMessage, "Stream ended without finish_reason") {
			t.Errorf("unfinished stream diagnostic = %q (%v)", first.ErrorMessage, err)
		}
		opaqueThinking(t, first, "Plan.", `{"field":"reasoning_text","reasoning_opaque":"opaque-1"}`)
	})
	t.Run("other-model", func(t *testing.T) {
		f := opaqueFixture{sets: [][]string{{
			opaqueChunk(`{"reasoning_text":"Plan."}`, ""), opaqueChunk(`{"reasoning_opaque":"opaque-1"}`, ""),
			opaqueChunk(`{"content":"Done."}`, "stop"),
		}, ok}}
		first, _, _ := f.run(t)
		other := *first
		other.Model = "other-model"
		f.run(t, other)
		payload := f.assistant(t, 1)
		opaqueRequiredFields(t, payload, map[string]any{"content": "Done."})
		opaqueAbsentFields(t, payload, "reasoning_opaque", "reasoning_text")
	})
}

func TestCompletionsStoredOpaqueReasoning(t *testing.T) {
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
			f := opaqueFixture{sets: [][]string{{opaqueChunk(`{"content":"ok"}`, "stop")}}}
			model := opaqueModel()
			f.run(t, goai.Message{Role: goai.RoleAssistant, Provider: model.Provider, Api: model.Api, Model: model.ID, StopReason: goai.StopReasonStop,
				Content: []goai.ContentBlock{{Type: "thinking", Thinking: row.thinking, ThinkingSignature: row.signature}, {Type: "text", Text: "Done."}}})
			payload := f.assistant(t, 0)
			opaqueRequiredFields(t, payload, row.want)
			opaqueAbsentFields(t, payload, row.absent...)
		})
	}
}

func TestCompletionsOpaqueReasoningBoundaries(t *testing.T) {
	t.Run("provider-event-refusal-keeps-metadata", func(t *testing.T) {
		f := opaqueFixture{sets: [][]string{{
			opaqueChunk(`{"reasoning_text":"Plan.","reasoning_opaque":"opaque-1"}`, ""),
			opaqueChunk(`{"content":"unobserved"}`, "stop"),
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
		opaqueThinking(t, first, "Plan.", `{"field":"reasoning_text","reasoning_opaque":"opaque-1"}`)
	})
	t.Run("wrong-wire-type-keeps-visible-text", func(t *testing.T) {
		f := opaqueFixture{sets: [][]string{{opaqueChunk(`{"content":"Done.","reasoning_opaque":7}`, "stop")}}}
		first, deltas, err := f.run(t)
		if err != nil || len(first.Content) != 1 || first.Content[0].Text != "Done." || len(deltas) != 0 {
			t.Fatalf("malformed opaque metadata lost visible text or produced thinking: %+v (%v, %v)", first, err, deltas)
		}
	})
}

func storedMessageJSON(t *testing.T, message goai.Message) goai.Message {
	t.Helper()
	wire, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var decoded goai.Message
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func storedAssistant(block goai.ContentBlock) goai.Message {
	model := opaqueModel()
	return goai.Message{Role: goai.RoleAssistant, Provider: model.Provider, Api: model.Api, Model: model.ID,
		StopReason: goai.StopReasonStop, Content: []goai.ContentBlock{block, {Type: "text", Text: "Done."}}}
}

func storedReplayFields(t *testing.T, payload, want map[string]any) {
	t.Helper()
	opaqueRequiredFields(t, payload, want)
	for _, field := range []string{"reasoning_content", "reasoning", "reasoning_text", "reasoning_details", "reasoning_opaque"} {
		if _, expected := want[field]; !expected {
			opaqueAbsentFields(t, payload, field)
		}
	}
}

func TestCompletionsStoredReasoning(t *testing.T) {
	ok := []string{opaqueChunk(`{"content":"ok"}`, "stop")}
	details := []any{map[string]any{"type": "reasoning.encrypted", "id": "call_1", "data": "encrypted"}}

	t.Run("round-trip", func(t *testing.T) {
		for _, row := range []struct {
			name, delta, signature string
			want                   map[string]any
		}{
			{"field", `{"reasoning_content":"Plan.","content":"Done."}`, "reasoning_content", map[string]any{"reasoning_content": "Plan."}},
			{"details", `{"reasoning_details":[` + opaqueDetail + `],"content":"Done."}`, `[` + opaqueDetail + `]`, map[string]any{"reasoning_details": details}},
			{"opaque-with-field", `{"reasoning_text":"Plan.","reasoning_opaque":"opaque-1","content":"Done."}`, `{"field":"reasoning_text","reasoning_opaque":"opaque-1"}`,
				map[string]any{"reasoning_text": "Plan.", "reasoning_opaque": "opaque-1"}},
			{"opaque-without-field", `{"reasoning_opaque":"opaque-1","content":"Done."}`, `{"reasoning_opaque":"opaque-1"}`, map[string]any{"reasoning_opaque": "opaque-1"}},
		} {
			t.Run(row.name, func(t *testing.T) {
				f := opaqueFixture{sets: [][]string{{opaqueChunk(row.delta, "stop")}, ok}}
				first, _, err := f.run(t)
				if err != nil || first.StopReason != goai.StopReasonStop || len(first.Content) == 0 || first.Content[0].Type != "thinking" {
					t.Fatalf("cannot store streamed reasoning: %+v (%v)", first, err)
				}
				saved := storedMessageJSON(t, *first)
				if saved.Content[0].ThinkingSignature != row.signature {
					t.Errorf("stored signature = %q, want %q", saved.Content[0].ThinkingSignature, row.signature)
				}
				f.run(t, saved)
				storedReplayFields(t, f.assistant(t, 1), row.want)
			})
		}
	})

	t.Run("reads-formats", func(t *testing.T) {
		for _, row := range []struct {
			name, signature string
			want            map[string]any
		}{
			{"field", "reasoning", map[string]any{"reasoning": "Plan."}},
			{"details", `[` + opaqueDetail + `]`, map[string]any{"reasoning_details": details}},
			{"opaque", `{"field":"reasoning_text","reasoning_opaque":"o"}`, map[string]any{"reasoning_text": "Plan.", "reasoning_opaque": "o"}},
		} {
			t.Run(row.name, func(t *testing.T) {
				f := opaqueFixture{sets: [][]string{ok}}
				seed := storedAssistant(goai.ContentBlock{Type: "thinking", Thinking: "Plan.", ThinkingSignature: row.signature})
				f.run(t, storedMessageJSON(t, seed))
				storedReplayFields(t, f.assistant(t, 0), row.want)
			})
		}
	})

	t.Run("rejects-invalid", func(t *testing.T) {
		for _, row := range []struct {
			name, signature string
			present         bool
		}{
			{"undefined", "", false},
			{"empty", "", true},
			{"foreign", "anthropic-signature", true},
			{"empty-array", "[]", true},
			{"malformed-array", "[not json", true},
			{"unknown-detail", `[{"type":"unknown"}]`, true},
			{"empty-opaque", `{"reasoning_opaque":""}`, true},
			{"invalid-field", `{"field":"content","reasoning_opaque":"o"}`, true},
			{"malformed-object", "{not json", true},
		} {
			t.Run(row.name, func(t *testing.T) {
				f := opaqueFixture{sets: [][]string{ok}}
				seed := storedAssistant(goai.ContentBlock{Type: "thinking", Thinking: "Plan.", ThinkingSignature: row.signature, ThinkingSignaturePresent: row.present})
				f.run(t, storedMessageJSON(t, seed))
				storedReplayFields(t, f.assistant(t, 0), map[string]any{"content": "Done."})
			})
		}
	})

	// This checks the wire outcome of the three raw values. Go does not
	// expose the original's global JSON.parse spy as part of this path.
	t.Run("non-json-outcomes", func(t *testing.T) {
		f := opaqueFixture{sets: [][]string{ok}}
		seed := storedAssistant(goai.ContentBlock{Type: "thinking", Thinking: "Plan.", ThinkingSignature: "reasoning_content"})
		seed.Content = append(seed.Content,
			goai.ContentBlock{Type: "thinking", ThinkingSignature: "anthropic-signature"},
			goai.ContentBlock{Type: "toolCall", ID: "call_1", Name: "read", Arguments: map[string]any{"path": "README.md"}, ThoughtSignature: "google-thought-signature"})
		f.run(t, storedMessageJSON(t, seed))
		storedReplayFields(t, f.assistant(t, 0), map[string]any{"reasoning_content": "Plan."})
	})

	t.Run("legacy-tool-call", func(t *testing.T) {
		for _, row := range []struct {
			name, signature string
			want            map[string]any
		}{
			{"valid", opaqueDetail, map[string]any{"reasoning_details": details}},
			{"empty-id", `{"type":"reasoning.encrypted","id":"","data":"encrypted"}`, nil},
		} {
			t.Run(row.name, func(t *testing.T) {
				f := opaqueFixture{sets: [][]string{ok}}
				seed := storedAssistant(goai.ContentBlock{Type: "toolCall", ID: "call_1", Name: "read", Arguments: map[string]any{"path": "README.md"}, ThoughtSignature: row.signature})
				f.run(t, storedMessageJSON(t, seed))
				storedReplayFields(t, f.assistant(t, 0), row.want)
			})
		}
	})
}
