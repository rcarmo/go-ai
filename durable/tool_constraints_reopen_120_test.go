package durable

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestTool120ConstraintContributionReopenAndProjectionIsolation(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		session, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		token := entryToken(t, "constraint.entry")
		grammar := &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": "start: /.+/"}}
		var entry Entry
		_, err = session.Commit(bg, func(tx *Tx) error {
			var err error
			entry, err = tx.AppendTypedEntry(token, 1, EntryContent{Model: []goai.Message{{Role: goai.RoleSystem, ToolsAdded: []goai.Tool{{Name: "grammar", Parameters: json.RawMessage(`{"type":"object"}`), ConstrainedSampling: grammar}}}}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		grammar.Variants["openai_lark"] = "changed caller"
		if err = session.Close(bg); err != nil {
			t.Fatal(err)
		}
		store := reopenStoreAfterHarnessClose(t, b.store)
		next, err := OpenSession(store)
		if err != nil {
			t.Fatal(err)
		}
		defer next.Close(bg)
		state, err := next.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		stored := state.Entries[entry.ID]
		if len(stored.Model) != 1 || len(stored.Model[0].ToolsAdded) != 1 {
			t.Fatal("stored contribution missing", stored)
		}
		receipt := stored.Model[0]
		message := receiptMessage(receipt)
		if message.ToolsAdded[0].ConstrainedSampling.Variants["openai_lark"] != "start: /.+/" {
			t.Fatal(message)
		}
		message.ToolsAdded[0].ConstrainedSampling.Variants["openai_lark"] = "changed projection"
		if receipt.ToolsAdded[0].ConstrainedSampling.Variants["openai_lark"] != "start: /.+/" {
			t.Fatal("receipt aliased")
		}
	})
}
func TestTool120GrammarChangeProducesPromptDeclarationDelta(t *testing.T) {
	tool := goai.Tool{Name: "grammar", Parameters: json.RawMessage(`{"type":"object"}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": "first"}}}
	receipt, err := contributionReceipt(goai.Message{Role: goai.RoleSystem, ToolsAdded: []goai.Tool{tool}}, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	tool.ConstrainedSampling.Variants["openai_lark"] = "second"
	drafts, err := PlanSystemEntries(ContextView{Messages: []MessageReceipt{receipt}}, nil, []goai.Tool{tool}, 2, DefaultLimits())
	if err != nil || len(drafts) != 1 || len(drafts[0].Model[0].ToolsAdded) != 1 || len(drafts[0].Model[0].ToolsRemoved) != 1 {
		t.Fatal(drafts, err)
	}
	if drafts[0].Model[0].ToolsAdded[0].ConstrainedSampling.Variants["openai_lark"] != "second" {
		t.Fatal(drafts)
	}
}
