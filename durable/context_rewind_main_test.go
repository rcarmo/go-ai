package durable

import (
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// A backwards head must recover entries discarded from the kept range. Expected
// IDs and messages are explicit: deriving a second view would share the cache's
// head-selection implementation and could conceal the same defect.
func TestContextMainHeadRewindRestoresDiscardedHistory(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		root := mint(t, b.store)
		apply(t, b.store, Write{Op: "create-conversation", Conversation: &Conversation{ID: root}})
		s, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		appendEntry := func(entry Entry) ID {
			t.Helper()
			id, err := s.MintID(bg)
			if err != nil {
				t.Fatal(err)
			}
			entry.ID, entry.Conversation = id, root
			if entry.Value == nil {
				entry.Value = JSON{}
			}
			if _, err = s.Commit(bg, func(tx *Tx) error { return tx.AppendEntry(entry) }); err != nil {
				t.Fatal(err)
			}
			return id
		}
		first := appendEntry(contextEntry(0, root, goai.RoleUser, "first"))
		second := appendEntry(contextEntry(0, root, goai.RoleUser, "second"))
		reset := appendEntry(Entry{Kind: "head", Head: second})
		check := func(at, head ID, ids []ID, texts []string) ContextView {
			t.Helper()
			view, err := s.ContextView(bg, root, at)
			if err != nil {
				t.Fatal(err)
			}
			gotHead := ID(0)
			if view.Head != nil {
				gotHead = view.Head.ID
			}
			var gotIDs []ID
			for _, entry := range view.Entries {
				gotIDs = append(gotIDs, entry.ID)
			}
			var gotTexts []string
			for _, message := range view.Messages {
				for _, block := range message.Content {
					if block.Type == "text" {
						gotTexts = append(gotTexts, block.Text)
					}
				}
			}
			if gotHead != head || !reflect.DeepEqual(gotIDs, ids) || !reflect.DeepEqual(gotTexts, texts) {
				t.Fatalf("at %d: head=%d IDs=%v texts=%v; want head=%d IDs=%v texts=%v", at, gotHead, gotIDs, gotTexts, head, ids, texts)
			}
			return view
		}
		check(0, reset, []ID{reset, second}, []string{"second"})
		s.taskBookkeeping(func() {
			for _, entry := range s.contextRanges[root].history {
				if entry.ID == first {
					t.Fatal("fixture did not discard first entry")
				}
			}
		})
		rewind := appendEntry(Entry{Kind: "head", Head: first})
		restored := check(0, rewind, []ID{rewind, first, second}, []string{"first", "second"})
		edit := appendEntry(Entry{Kind: "edit", Edits: []ContextEdit{{Target: first, Action: "replace", Messages: []MessageReceipt{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "edited"}}}}}}})
		check(0, rewind, []ID{rewind, first, second, edit}, []string{"edited", "second"})
		check(reset, reset, []ID{reset, second}, []string{"second"})
		check(first, 0, []ID{first}, []string{"first"})
		check(rewind, rewind, []ID{rewind, first, second}, []string{"first", "second"})
		s.taskBookkeeping(func() {
			kept := s.contextRanges[root]
			if kept.tail != edit || !kept.current {
				t.Fatal("historical read replaced current range")
			}
		})
		restored.Messages[0].Content[0].Text = "caller mutation"
		restored.Entries[1].Model[0].Content[0].Text = "caller entry mutation"
		check(0, rewind, []ID{rewind, first, second, edit}, []string{"edited", "second"})
		last := appendEntry(contextEntry(0, root, goai.RoleUser, "last"))
		check(0, rewind, []ID{rewind, first, second, edit, last}, []string{"edited", "second", "last"})
		// Pinned harness-context #10512 compares historical reads with forks at
		// the same cutoffs. Include both sides of the forward/backward heads.
		for _, at := range []ID{first, second, reset, rewind, edit, last} {
			child, err := s.MintID(bg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Commit(bg, func(tx *Tx) error {
				return tx.CreateConversation(Conversation{ID: child, Parent: root, ParentAt: at})
			}); err != nil {
				t.Fatal(err)
			}
			parentView, err := s.ContextView(bg, root, at)
			if err != nil {
				t.Fatal(err)
			}
			childView, err := s.ContextView(bg, child, 0)
			if err != nil || !reflect.DeepEqual(parentView, childView) {
				t.Fatalf("fork at %d: parent=%+v child=%+v err=%v", at, parentView, childView, err)
			}
			if at != last {
				if _, err := s.ContextView(bg, child, last); err == nil {
					t.Fatalf("fork at %d admitted invisible cutoff %d", at, last)
				}
			}
		}
	})
}
