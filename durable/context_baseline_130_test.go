package durable

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestContext130BaselineLeadsImportedAssistantHistory(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		h := openHarness(t, b.store, Options{})
		conv, err := h.Root(bg, AgentChange{})
		if err != nil {
			t.Fatal(err)
		}
		var assistant ID
		append130 := func(entry Entry) {
			t.Helper()
			if _, err := h.session.Commit(bg, func(tx *Tx) error {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				entry.ID = id
				if entry.Value == nil {
					entry.Value = JSON{}
				}
				entry.Conversation = conv.ID()
				if entry.Kind == "imported" {
					assistant = id
				}
				return tx.AppendEntry(entry)
			}); err != nil {
				t.Fatal(err)
			}
		}
		append130(Entry{Kind: "imported", Model: []MessageReceipt{{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "imported"}}}}})
		append130(Entry{Kind: "head", Head: assistant})
		append130(Entry{Kind: "pi.system", Model: []MessageReceipt{{Role: goai.RoleSystem, Content: []goai.ContentBlock{{Type: "text", Text: "baseline"}}}}})
		check := func() {
			t.Helper()
			view, err := conv.Context(bg, 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(view.Messages) < 2 || view.Messages[0].Role != goai.RoleSystem || view.Messages[0].Content[0].Text != "baseline" || view.Messages[1].Role != goai.RoleAssistant {
				t.Fatal(view.Messages)
			}
		}
		check()
		append130(Entry{Kind: "input", Model: []MessageReceipt{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "later"}}}}})
		check()
	})
}

func TestContext130LaterSystemDeltaDoesNotLeadAssistant(t *testing.T) {
	messages := []MessageReceipt{{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "earlier"}}}, {Role: goai.RoleSystem, Content: []goai.ContentBlock{{Type: "text", Text: "delta"}}}}
	got := leadContextWithBaseline(messages, false)
	if got[0].Role != goai.RoleAssistant {
		t.Fatal(got)
	}
}
