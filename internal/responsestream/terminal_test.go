package responsestream

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
)

func TestTerminalStatusMain(t *testing.T) {
	for _, tc := range []struct {
		status, reason string
		want           goai.StopReason
		message        string
		err            bool
	}{{"", "", goai.StopReasonStop, "", false}, {"completed", "", goai.StopReasonStop, "", false}, {"in_progress", "", goai.StopReasonStop, "", false}, {"queued", "", goai.StopReasonStop, "", false}, {"incomplete", "max_output_tokens", goai.StopReasonLength, "", false}, {"incomplete", "", goai.StopReasonError, "Response incomplete without a provider reason", false}, {"incomplete", "content_filter", goai.StopReasonError, "Response incomplete: content_filter", false}, {"failed", "", goai.StopReasonError, "", false}, {"cancelled", "", goai.StopReasonError, "", false}, {"new_status", "", goai.StopReasonError, "", true}} {
		stop, message, err := TerminalStatus(tc.status, tc.reason)
		if stop != tc.want || message != tc.message || (err != nil) != tc.err {
			t.Fatal(tc, stop, message, err)
		}
	}
}
func TestReasoningBackfillMain(t *testing.T) {
	for _, field := range []string{"", `,"encrypted_content":""`, `,"encrypted_content":null`, `,"encrypted_content":"original"`, `,"encrypted_content":42`} {
		t.Run(field, func(t *testing.T) {
			signature := `{"type":"reasoning","id":"r","summary":[{"type":"summary_text","text":"mentions encrypted_content"}],"opaque":9007199254740993` + field + `}`
			blocks := []goai.ContentBlock{{Type: "thinking", Thinking: "unchanged", ThinkingSignature: signature}, {Type: "thinking", ThinkingSignature: `{invalid`}}
			output := []json.RawMessage{json.RawMessage(`{"type":"reasoning","id":"other","encrypted_content":"ignored"}`), json.RawMessage(`{"type":"reasoning","id":"r","encrypted_content":"backfill","summary":[{"text":"do not replace"}]}`), json.RawMessage(`{"type":"reasoning","id":"r","encrypted_content":"later"}`)}
			BackfillReasoning(blocks, output)
			var stored map[string]json.RawMessage
			if err := json.Unmarshal([]byte(blocks[0].ThinkingSignature), &stored); err != nil {
				t.Fatal(err)
			}
			if blocks[0].Thinking != "unchanged" || !strings.Contains(string(stored["summary"]), "mentions encrypted_content") || string(stored["opaque"]) != "9007199254740993" || blocks[1].ThinkingSignature != `{invalid` {
				t.Fatal("stored reasoning changed", blocks)
			}
			want := `"backfill"`
			if strings.Contains(field, "original") {
				want = `"original"`
			}
			if strings.Contains(field, "42") {
				want = "42"
			}
			if string(stored["encrypted_content"]) != want {
				t.Fatal("backfill mismatch", stored)
			}
		})
	}
}
