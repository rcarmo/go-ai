package durable

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

// Source: types.ts optional requestId and storage/memory.ts's !== undefined
// index rule. Private storage keeps the same optional string field; no extra
// presence marker is introduced into SQLite frames or checkpoints.
func TestV150SubmissionRequestPresenceLegacyWire(t *testing.T) {
	for _, field := range []string{"", `,"requestId":"key"`, `,"requestId":""`} {
		raw := []byte(`{"id":2,"conversation":1,"type":"write","status":"pending","value":{}` + field + `}`)
		var sub Submission
		if err := decodeStrict(raw, DefaultLimits(), DefaultLimits().MaxRecordBytes, &sub); err != nil {
			t.Fatal(err)
		}
		if sub.hasRequestID() != (field != "") {
			t.Fatalf("presence from %s: %+v", raw, sub)
		}
		encoded, err := encodeBounded(sub, DefaultLimits(), DefaultLimits().MaxRecordBytes)
		if err != nil {
			t.Fatal(err)
		}
		var want, got any
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(encoded, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(canonicalJSON(t, sub), want) {
			t.Fatalf("private envelope changed: %s vs %s", encoded, raw)
		}
		// Public optional-field presence follows the exact same absence distinction.
		public := canonicalJSON(t, publicSubmission(sub)).(map[string]any)
		_, present := public["requestId"]
		if present != (field != "") {
			t.Fatalf("public presence: %v", public)
		}
	}
	for _, raw := range []string{
		`{"id":2,"conversation":1,"type":"write","status":"pending","value":{},"requestId":null}`,
		`{"id":2,"conversation":1,"type":"write","status":"pending","value":{},"requestId":false}`,
		`{"id":2,"conversation":1,"type":"write","status":"pending","value":{},"extra":true}`,
	} {
		original := Submission{ID: 99, RequestID: "unchanged"}
		got := original
		if err := decodeStrict([]byte(raw), DefaultLimits(), DefaultLimits().MaxRecordBytes, &got); err == nil || !reflect.DeepEqual(got, original) {
			t.Fatalf("invalid wire accepted or destination modified: %s %+v", raw, got)
		}
	}
}

func TestV150SubmissionRequestPresenceReopenAndCheckpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close(bg) }()
	absent, empty := mint(t, store), mint(t, store)
	for _, sub := range []Submission{
		{ID: absent, Conversation: 1, Type: "write", Status: "pending", Value: JSON{}},
		{ID: empty, Conversation: 1, Type: "write", Status: "pending", Value: JSON{}, RequestIDPresent: true},
	} {
		apply(t, store, Write{Op: "put-submission", Submission: &sub})
	}
	check := func() {
		state := snap(t, store)
		if state.Submissions[absent].hasRequestID() || !state.Submissions[empty].hasRequestID() {
			t.Fatal("presence lost", state.Submissions)
		}
		found, ok, err := store.SubmissionByRequest(bg, 1, "")
		if err != nil || !ok || found.ID != empty {
			t.Fatalf("empty request lookup: %+v %v %v", found, ok, err)
		}
		// Snapshot mutation cannot change storage. An explicit raw replacement
		// may remove the key; source storage does not freeze this identity.
		copy := state.Submissions[empty]
		copy.RequestIDPresent = false
		retained, exists, err := store.Submission(bg, empty)
		if err != nil || !exists || !retained.RequestIDPresent {
			t.Fatal(retained, exists, err)
		}
		if _, err = store.Apply(bg, Batch{Writes: []Write{{Op: "put-submission", Submission: &copy}}}); err != nil {
			t.Fatal(err)
		}
		removed, exists, err := store.Submission(bg, empty)
		if err != nil || !exists || removed.RequestIDPresent {
			t.Fatal(removed, exists, err)
		}
		original := state.Submissions[empty]
		apply(t, store, Write{Op: "put-submission", Submission: &original})
	}
	check()
	// Both absent keys and duplicate present-empty raw keys are independent records.
	another := mint(t, store)
	apply(t, store, Write{Op: "put-submission", Submission: &Submission{ID: another, Conversation: 1, Type: "write", Status: "pending", Value: JSON{}}})
	duplicate := Submission{ID: mint(t, store), Conversation: 1, Type: "write", Status: "pending", Value: JSON{}, RequestIDPresent: true}
	if _, err := store.Apply(bg, Batch{Writes: []Write{{Op: "put-submission", Submission: &duplicate}}}); err != nil {
		t.Fatal("raw duplicate", err)
	}
	// Updating lifecycle preserves the key.
	updated := snap(t, store).Submissions[empty]
	updated.Status = "failed"
	updated.Value = JSON{"errorCode": ""}
	apply(t, store, Write{Op: "put-submission", Submission: &updated})
	for _, checkpoint := range []bool{false, true} {
		if checkpoint {
			if err := store.Reclaim(bg); err != nil {
				t.Fatal(err)
			}
		}
		if err := store.Close(bg); err != nil {
			t.Fatal(err)
		}
		store, err = OpenSQLite(path, SQLiteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		check()
	}
}

func TestV150SubmissionRequestPresenceSessionAndHarness(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := taskTestHarness(t, store)
	c, err := h.Root(bg, AgentChange{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.Submit(bg, Input{Type: "write", Content: "one", RequestIDPresent: true})
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := c.Submit(bg, Input{Type: "write", Content: "ignored", RequestIDPresent: true})
	if err != nil || duplicate.ID() != first.ID() {
		t.Fatalf("empty key dedup: %v %v", duplicate, err)
	}
	absent, err := c.Submit(bg, Input{Type: "write", Content: "two"})
	if err != nil || absent.ID() == first.ID() {
		t.Fatalf("absent key deduped: %v %v", absent, err)
	}
	if _, err := c.Submit(bg, Input{Type: "input", Content: "cross type", RequestIDPresent: true}); err == nil {
		t.Fatal("cross-type empty-key reuse accepted")
	}
	status, err := first.Status(bg)
	if err != nil || !status.RequestIDPresent {
		t.Fatalf("status lost presence: %+v %v", status, err)
	}
	// The public transaction API also expresses explicit empty presence without
	// requiring callers to manufacture a JSON decoder side effect.
	var id ID
	_, err = h.Commit(bg, func(tx *Tx) error {
		var err error
		conv, err := tx.MintID()
		if err != nil {
			return err
		}
		if err := tx.CreateConversation(Conversation{ID: conv}); err != nil {
			return err
		}
		id, err = tx.CreateSubmission(SubmissionRecord{Conversation: conv, Type: "continue", Status: "placed", RequestIDPresent: true})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.Commit(bg, func(tx *Tx) error {
		record, ok, err := tx.Submission(id)
		if err != nil {
			return err
		}
		if !ok || !record.RequestIDPresent {
			t.Fatalf("tx lost presence %+v", record)
		}
		return tx.SettleSubmission(id, SubmissionRecord{Status: "unanswered", Reason: ""})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestV150SubmissionNativeMarshalDeterministic(t *testing.T) {
	sub := Submission{ID: 2, Conversation: 1, Type: "write", Status: "pending", Value: JSON{"a": 1, "b": 2, "c": 3, "d": 4, "e": 5, "f": 6, "nested": JSON{"x": 1, "y": 2, "z": 3}}}
	want := marshalOrFail(t, sub)
	for i := 0; i < 100; i++ {
		if got := marshalOrFail(t, sub); string(got) != string(want) {
			t.Fatalf("nondeterministic native marshal:\n%s\n%s", want, got)
		}
	}
}
