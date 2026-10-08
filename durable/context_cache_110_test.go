package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"reflect"
	"testing"
	"time"
)

func contextEntry(id, conversation ID, role goai.Role, text string) Entry {
	return Entry{ID: id, Conversation: conversation, Kind: "contribution", Value: JSON{}, Model: []MessageReceipt{{Role: role, Content: []goai.ContentBlock{{Type: "text", Text: text}}}}}
}
func TestContext110CacheAppendEditsBoundsAndDetachment(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		root := mint(t, b.store)
		apply(t, b.store, Write{Op: "create-conversation", Conversation: &Conversation{ID: root}})
		s, e := OpenSession(b.store)
		if e != nil {
			t.Fatal(e)
		}
		defer s.Close(bg)
		appendEntry := func(entry Entry) {
			t.Helper()
			_, e := s.Commit(bg, func(tx *Tx) error { return tx.AppendEntry(entry) })
			if e != nil {
				t.Fatal(e)
			}
		}
		mintID := func() ID {
			id, e := s.MintID(bg)
			if e != nil {
				t.Fatal(e)
			}
			return id
		}
		user := mintID()
		appendEntry(contextEntry(user, root, goai.RoleUser, "input"))
		baseline := mintID()
		appendEntry(contextEntry(baseline, root, goai.RoleSystem, "baseline"))
		v, e := s.ContextView(bg, root, 0)
		if e != nil || len(v.Messages) != 2 || v.Messages[0].Role != goai.RoleSystem || v.Messages[1].Role != goai.RoleUser {
			t.Fatal(v, e)
		}
		v.Messages[0].Content[0].Text = "mutated"
		v, e = s.ContextView(bg, root, 0)
		if e != nil || v.Messages[0].Content[0].Text != "baseline" {
			t.Fatal("cache alias", v, e)
		}
		// A historical-first read must not become the current-tail witness.
		s.taskBookkeeping(func() { s.contextRanges = nil })
		older, e := s.ContextView(bg, root, user)
		if e != nil || len(older.Messages) != 1 {
			t.Fatal(older, e)
		}
		v, e = s.ContextView(bg, root, 0)
		if e != nil || len(v.Messages) != 2 {
			t.Fatal("historical cache reused as current", v, e)
		}
		var previous *contextRange
		s.taskBookkeeping(func() { previous = s.contextRanges[root] })
		if previous == nil {
			t.Fatal("cache absent")
		}
		assistant := mintID()
		entry := contextEntry(assistant, root, goai.RoleAssistant, "")
		entry.Model[0].Content = []goai.ContentBlock{{Type: "toolCall", ID: "call", Name: "tool", Arguments: map[string]any{}}}
		appendEntry(entry)
		v, e = s.ContextView(bg, root, 0)
		if e != nil || len(v.Messages) != 4 || !v.Messages[3].IsError {
			t.Fatal(v, e)
		}
		result := mintID()
		entry = contextEntry(result, root, goai.RoleToolResult, "actual")
		entry.Model[0].ToolCallID = "call"
		entry.Model[0].ToolName = "tool"
		appendEntry(entry)
		v, e = s.ContextView(bg, root, 0)
		if e != nil || len(v.Messages) != 4 || v.Messages[3].IsError || v.Messages[3].Content[0].Text != "actual" {
			t.Fatal("synthetic result retained", v, e)
		}
		old, e := s.ContextView(bg, root, baseline)
		if e != nil || len(old.Messages) != 2 {
			t.Fatal(old, e)
		}
		s.taskBookkeeping(func() {
			if s.contextRanges[root].tail != result {
				t.Fatal("historical read replaced cache")
			}
		})
		editID := mintID()
		appendEntry(Entry{ID: editID, Conversation: root, Kind: "edit", Value: JSON{}, Edits: []ContextEdit{{Target: user, Action: "replace", Messages: []MessageReceipt{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "edited"}}}}}}})
		v, e = s.ContextView(bg, root, 0)
		if e != nil || v.Messages[1].Content[0].Text != "edited" {
			t.Fatal(v, e)
		}
		old, e = s.ContextView(bg, root, baseline)
		if e != nil || old.Messages[1].Content[0].Text != "input" {
			t.Fatal("as-of edit leak", old, e)
		}
		snapshot, e := s.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		reference, e := deriveContextView(snapshot, root, 0, s.limits)
		if e != nil || !reflect.DeepEqual(v, reference) {
			t.Fatal("cache/reference drift", v, reference, e)
		}
		marker := mintID()
		appendEntry(Entry{ID: marker, Conversation: root, Kind: "head", Value: JSON{}, Head: assistant})
		v, e = s.ContextView(bg, root, 0)
		if e != nil || v.Head == nil || v.Head.ID != marker || len(v.Messages) != 2 {
			t.Fatal("head invalidation", v, e)
		}
		// Fork cutoff remains authoritative after the parent's later appends.
		child := mintID()
		_, e = s.Commit(bg, func(tx *Tx) error {
			return tx.CreateConversation(Conversation{ID: child, Parent: root, ParentAt: baseline})
		})
		if e != nil {
			t.Fatal(e)
		}
		fork, e := s.ContextView(bg, child, 0)
		if e != nil || len(fork.Messages) != 2 || fork.Messages[1].Content[0].Text != "input" {
			t.Fatal("fork cutoff leaked", fork, e)
		}
		if _, e = s.ContextView(bg, child, result); e == nil {
			t.Fatal("foreign fork cutoff accepted")
		}
		if _, e = s.ContextView(bg, root, ID(MaxID)); e == nil {
			t.Fatal("invisible cutoff accepted")
		}
	})
}
func TestContext110RetentionZeroExpiryAndSettingsIsolation(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		zero := int64(0)
		h := openHarness(t, b.store, Options{Settings: &HarnessSettings{ContextRetentionMs: &zero}})
		c, e := h.Root(bg, AgentChange{})
		if e != nil {
			t.Fatal(e)
		}
		var entryID ID
		_, e = c.Commit(bg, func(tx *Tx) error {
			var e error
			entryID, e = tx.MintID()
			if e != nil {
				return e
			}
			return tx.AppendEntry(contextEntry(entryID, c.ID(), goai.RoleUser, "one"))
		})
		if e != nil {
			t.Fatal(e)
		}
		var secondID ID
		_, e = c.Commit(bg, func(tx *Tx) error {
			var e error
			secondID, e = tx.MintID()
			if e != nil {
				return e
			}
			return tx.AppendEntry(contextEntry(secondID, c.ID(), goai.RoleUser, "two"))
		})
		if e != nil {
			t.Fatal(e)
		}
		page, e := c.ScanEntries(bg, EntryQuery{}, 1, "")
		if e != nil || len(page.Items) != 1 || page.Items[0].ID != entryID || page.Next == "" {
			t.Fatal("conversation default order", page, e)
		}
		last, e := c.ScanEntries(bg, EntryQuery{}, 1, page.Next)
		if e != nil || len(last.Items) != 1 || last.Items[0].ID != secondID {
			t.Fatal("conversation cursor order", last, e)
		}
		if _, e = c.Context(bg, entryID); e != nil {
			t.Fatal(e)
		}
		h.session.taskBookkeeping(func() {
			if len(h.session.contextRanges) != 0 {
				t.Fatal("zero retention retained idle data")
			}
		})
		short := int64(15)
		if e = h.SetSettings(bg, HarnessSettings{ContextRetentionMs: &short}); e != nil {
			t.Fatal(e)
		}
		short = 999999
		if _, e = c.ContextView(bg, 0); e != nil {
			t.Fatal(e)
		}
		h.session.taskBookkeeping(func() {
			if len(h.session.contextRanges) != 1 {
				t.Fatal("positive retention not cached")
			}
		})
		deadline, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for {
			empty := false
			h.session.taskBookkeeping(func() { empty = len(h.session.contextRanges) == 0 })
			if empty {
				break
			}
			select {
			case <-deadline.Done():
				t.Fatal("idle expiry failed")
			case <-time.After(time.Millisecond):
			}
		}
		negative := int64(-1)
		if e = h.SetSettings(bg, HarnessSettings{ContextRetentionMs: &negative}); e == nil {
			t.Fatal("negative retention accepted")
		}
	})
}
