package durable

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestCompactionCutUsesEligibleBoundaryAndProtectsPendingResult(t *testing.T) {
	call := MessageReceipt{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "toolCall", ID: "call", Name: "tool", Arguments: map[string]any{}}}}
	result := MessageReceipt{Role: goai.RoleToolResult, ToolCallID: "call", Content: []goai.ContentBlock{{Type: "text", Text: "result"}}}
	view := ContextView{Entries: []Entry{{ID: 2}, {ID: 3}, {ID: 4}, {ID: 5}, {ID: 6}}, Contributions: [][]MessageReceipt{{userReceipt("old")}, {call}, {userReceipt("interleaved steer")}, {result}, {userReceipt("newest")}}}
	// Candidate index 2 is a user message, but the preceding call still has a
	// result later in the retained suffix. Index 3 is a result and also ineligible.
	keep := 0
	for _, messages := range view.Contributions[2:] {
		for _, message := range messages {
			keep += goai.EstimateMessageTokens(receiptMessage(message))
		}
	}
	if cut := compactionCut(view, keep); cut != 4 {
		t.Fatal("pending pair split", cut)
	}
	if cut := compactionCut(view, 0); cut != 4 {
		t.Fatal("zero budget selects last eligible boundary", cut)
	}
	if cut := compactionCut(view, 10000); cut != 0 {
		t.Fatal("large budget must keep entire transcript", cut)
	}
	boundary := ContextView{Entries: []Entry{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}}, Contributions: [][]MessageReceipt{{call}, {userReceipt("eligible")}, {MessageReceipt{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "answer"}}}}, {result}}}
	if !compactionCuttable(boundary, 1) {
		t.Fatal("next assistant failed to end pending-call boundary")
	}
	marker := Entry{ID: 10, Head: 11}
	withHead := ContextView{Head: &marker, Entries: []Entry{marker, {ID: 11}}, Contributions: [][]MessageReceipt{{userReceipt("summary")}, {userReceipt("new")}}}
	if cut := compactionCut(withHead, 0); cut != 0 {
		t.Fatal("head alone made removable prefix", cut)
	}
	// A raw entry with no model contribution cannot be the retained boundary.
	view.Entries = append(view.Entries, Entry{ID: 7})
	view.Contributions = append(view.Contributions, []MessageReceipt{})
	if cut := compactionCut(view, goai.EstimateMessageTokens(receiptMessage(userReceipt("newest")))); cut != 4 {
		t.Fatal("raw entry changed eligible boundary", cut)
	}
	// A prefix made entirely of invisible raw entries is not summary work.
	raw := ContextView{Entries: []Entry{{ID: 1}, {ID: 2}}, Contributions: [][]MessageReceipt{{}, {userReceipt("newest")}}}
	if cut := compactionCut(raw, 100); cut != 0 {
		t.Fatal("empty prefix compacted", cut)
	}
}
