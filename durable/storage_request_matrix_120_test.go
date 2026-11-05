package durable

import (
	"errors"
	"testing"
)

// Exercise the direct storage boundary, outside the Session guard. Each failed
// read must leave both snapshot and subsequent valid reads usable.
func storageRequestMatrix120(t *testing.T, store Storage) {
	t.Helper()
	history := store.(DocumentHistoryStorage)
	limits, e := store.Limits()
	if e != nil {
		t.Fatal(e)
	}
	// The pinned source accepts an empty request string as a real lookup key.
	// Missing is a successful lookup; presence/dedup/reopen controls live in
	// submission_presence_v150_test.go.
	if _, found, err := store.Request(bg, 1, ""); err != nil || found {
		t.Fatalf("empty request lookup: %v %v", found, err)
	}
	initial, e := store.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		name string
		read func() error
	}{
		{"page-zero", func() error { _, e := store.Conversations(bg, Query{Limit: 0}); return e }},
		{"page-large", func() error { _, e := store.ScanTasks(bg, TaskQuery{}, limits.MaxPage+1, nil); return e }},
		{"conversation-missing", func() error { _, e := store.Entries(bg, 99, EntryCursor{}, 1); return e }},
		{"entry-cursor-owner", func() error { _, e := store.Entries(bg, 1, EntryCursor{Conversation: 2}, 1); return e }},
		{"entry-cursor-range", func() error { _, e := store.Entries(bg, 1, EntryCursor{Seq: MaxID + 1}, 1); return e }},
		{"request-owner-range", func() error { _, _, e := store.Request(bg, ID(MaxID+1), "x"); return e }},
		{"task-owner-range", func() error { _, e := store.ScanTasks(bg, TaskQuery{Conversation: ID(MaxID + 1)}, 1, nil); return e }},
		{"query-after-range", func() error { _, e := store.Documents(bg, Query{After: ID(MaxID + 1), Limit: 1}); return e }},
		{"entry-conversation-missing", func() error { _, e := store.ScanEntries(bg, EntryQuery{Conversation: 99}, 1, nil); return e }},
		{"entry-bound-range", func() error {
			_, e := store.ScanEntries(bg, EntryQuery{Conversation: 1, MaxEntryID: ID(MaxID + 1)}, 1, nil)
			return e
		}},
		{"document-owner", func() error {
			_, _, e := history.FindDocument(bg, DocumentAddress{Scope: "session", Owner: 1, Kind: "test"}, CurrentDocumentPoint())
			return e
		}},
		{"document-kind", func() error {
			_, _, e := history.FindDocument(bg, DocumentAddress{Scope: "session", Kind: ""}, CurrentDocumentPoint())
			return e
		}},
		{"document-point", func() error { _, _, e := history.DocumentAt(bg, 1, DocumentPoint{Current: true, Seq: 1}); return e }},
		{"document-id-range", func() error { _, _, e := history.DocumentAt(bg, ID(MaxID+1), CurrentDocumentPoint()); return e }},
		{"document-scope", func() error {
			_, e := history.ScanDocuments(bg, DocumentQuery{Scope: "bad", At: CurrentDocumentPoint()}, 1, nil)
			return e
		}},
		{"document-cursor-type", func() error {
			_, e := history.ScanDocuments(bg, DocumentQuery{Scope: "session", At: CurrentDocumentPoint()}, 1, Cursor{"after": true})
			return e
		}},
		{"document-cursor-json", func() error {
			_, e := history.ScanDocuments(bg, DocumentQuery{Scope: "session", At: CurrentDocumentPoint()}, 1, Cursor{"after": "bad"})
			return e
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := tc.read()
			var request *StorageRequestError
			var rejected *StorageRejected
			if !errors.As(e, &request) || !errors.As(e, &rejected) {
				t.Fatal("invalid read missing taxonomy", e)
			}
			state, e := store.Snapshot(bg)
			if e != nil || state.Seq != initial.Seq || state.HighWater != initial.HighWater {
				t.Fatal("invalid read changed/poisoned storage", e)
			}
			if _, e = store.ScanConversations(bg, ConversationQuery{}, 1, nil); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestStorage120DirectRequestMatrix(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) { storageRequestMatrix120(t, b.store) })
}
