package openai

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func storedMessageJSONV170(t *testing.T, message goai.Message) goai.Message {
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

func storedAssistantV170(block goai.ContentBlock) goai.Message {
	model := opaqueModelV170()
	return goai.Message{Role: goai.RoleAssistant, Provider: model.Provider, Api: model.Api, Model: model.ID,
		StopReason: goai.StopReasonStop, Content: []goai.ContentBlock{block, {Type: "text", Text: "Done."}}}
}

func storedReplayFieldsV170(t *testing.T, payload, want map[string]any) {
	t.Helper()
	opaqueRequiredFieldsV170(t, payload, want)
	for _, field := range []string{"reasoning_content", "reasoning", "reasoning_text", "reasoning_details", "reasoning_opaque"} {
		if _, expected := want[field]; !expected {
			opaqueAbsentFieldsV170(t, payload, field)
		}
	}
}

func TestSourceV170CompletionsStoredReasoning(t *testing.T) {
	ok := []string{opaqueChunkV170(`{"content":"ok"}`, "stop")}
	details := []any{map[string]any{"type": "reasoning.encrypted", "id": "call_1", "data": "encrypted"}}

	t.Run("round-trip", func(t *testing.T) {
		for _, row := range []struct {
			name, delta, signature string
			want                   map[string]any
		}{
			{"field", `{"reasoning_content":"Plan.","content":"Done."}`, "reasoning_content", map[string]any{"reasoning_content": "Plan."}},
			{"details", `{"reasoning_details":[` + opaqueDetailV170 + `],"content":"Done."}`, `[` + opaqueDetailV170 + `]`, map[string]any{"reasoning_details": details}},
			{"opaque-with-field", `{"reasoning_text":"Plan.","reasoning_opaque":"opaque-1","content":"Done."}`, `{"field":"reasoning_text","reasoning_opaque":"opaque-1"}`,
				map[string]any{"reasoning_text": "Plan.", "reasoning_opaque": "opaque-1"}},
			{"opaque-without-field", `{"reasoning_opaque":"opaque-1","content":"Done."}`, `{"reasoning_opaque":"opaque-1"}`, map[string]any{"reasoning_opaque": "opaque-1"}},
		} {
			t.Run(row.name, func(t *testing.T) {
				f := opaqueFixtureV170{sets: [][]string{{opaqueChunkV170(row.delta, "stop")}, ok}}
				first, _, err := f.run(t)
				if err != nil || first.StopReason != goai.StopReasonStop || len(first.Content) == 0 || first.Content[0].Type != "thinking" {
					t.Fatalf("cannot store streamed reasoning: %+v (%v)", first, err)
				}
				saved := storedMessageJSONV170(t, *first)
				if saved.Content[0].ThinkingSignature != row.signature {
					t.Errorf("stored signature = %q, want %q", saved.Content[0].ThinkingSignature, row.signature)
				}
				f.run(t, saved)
				storedReplayFieldsV170(t, f.assistant(t, 1), row.want)
			})
		}
	})

	t.Run("reads-formats", func(t *testing.T) {
		for _, row := range []struct {
			name, signature string
			want            map[string]any
		}{
			{"field", "reasoning", map[string]any{"reasoning": "Plan."}},
			{"details", `[` + opaqueDetailV170 + `]`, map[string]any{"reasoning_details": details}},
			{"opaque", `{"field":"reasoning_text","reasoning_opaque":"o"}`, map[string]any{"reasoning_text": "Plan.", "reasoning_opaque": "o"}},
		} {
			t.Run(row.name, func(t *testing.T) {
				f := opaqueFixtureV170{sets: [][]string{ok}}
				seed := storedAssistantV170(goai.ContentBlock{Type: "thinking", Thinking: "Plan.", ThinkingSignature: row.signature})
				f.run(t, storedMessageJSONV170(t, seed))
				storedReplayFieldsV170(t, f.assistant(t, 0), row.want)
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
				f := opaqueFixtureV170{sets: [][]string{ok}}
				seed := storedAssistantV170(goai.ContentBlock{Type: "thinking", Thinking: "Plan.", ThinkingSignature: row.signature, ThinkingSignaturePresent: row.present})
				f.run(t, storedMessageJSONV170(t, seed))
				storedReplayFieldsV170(t, f.assistant(t, 0), map[string]any{"content": "Done."})
			})
		}
	})

	// This checks the wire outcome of the three raw values. Go does not
	// expose the original's global JSON.parse spy as part of this path.
	t.Run("non-json-outcomes", func(t *testing.T) {
		f := opaqueFixtureV170{sets: [][]string{ok}}
		seed := storedAssistantV170(goai.ContentBlock{Type: "thinking", Thinking: "Plan.", ThinkingSignature: "reasoning_content"})
		seed.Content = append(seed.Content,
			goai.ContentBlock{Type: "thinking", ThinkingSignature: "anthropic-signature"},
			goai.ContentBlock{Type: "toolCall", ID: "call_1", Name: "read", Arguments: map[string]any{"path": "README.md"}, ThoughtSignature: "google-thought-signature"})
		f.run(t, storedMessageJSONV170(t, seed))
		storedReplayFieldsV170(t, f.assistant(t, 0), map[string]any{"reasoning_content": "Plan."})
	})

	t.Run("legacy-tool-call", func(t *testing.T) {
		for _, row := range []struct {
			name, signature string
			want            map[string]any
		}{
			{"valid", opaqueDetailV170, map[string]any{"reasoning_details": details}},
			{"empty-id", `{"type":"reasoning.encrypted","id":"","data":"encrypted"}`, nil},
		} {
			t.Run(row.name, func(t *testing.T) {
				f := opaqueFixtureV170{sets: [][]string{ok}}
				seed := storedAssistantV170(goai.ContentBlock{Type: "toolCall", ID: "call_1", Name: "read", Arguments: map[string]any{"path": "README.md"}, ThoughtSignature: row.signature})
				f.run(t, storedMessageJSONV170(t, seed))
				storedReplayFieldsV170(t, f.assistant(t, 0), row.want)
			})
		}
	})
}
