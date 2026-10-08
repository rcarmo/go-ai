//go:build cgo

package durable

import (
	"path/filepath"
	"testing"
)

func TestSQLite110OrderedScanCursorAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "scan.sqlite")
	store, e := OpenSQLite(path, SQLiteOptions{})
	if e != nil {
		t.Fatal(e)
	}
	first, second := mint(t, store), mint(t, store)
	apply(t, store, Write{Op: "create-conversation", Conversation: &Conversation{ID: first}}, Write{Op: "create-conversation", Conversation: &Conversation{ID: second}})
	page, e := store.ScanConversations(bg, ConversationQuery{Order: Descending}, 1, "")
	if e != nil || len(page.Items) != 1 || page.Items[0].ID != second || page.Next == "" {
		t.Fatal(page, e)
	}
	if e = store.Close(bg); e != nil {
		t.Fatal(e)
	}
	store, e = OpenSQLite(path, SQLiteOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close(bg)
	last, e := store.ScanConversations(bg, ConversationQuery{}, 2, page.Next)
	if e != nil || len(last.Items) != 2 || last.Items[0].ID != first || last.Items[1].ID != 1 || last.Next != "" {
		t.Fatal(last, e)
	}
	if _, e = store.ScanConversations(bg, ConversationQuery{Order: Ascending}, 1, page.Next); e == nil {
		t.Fatal("cursor direction changed after reopen")
	}
}
