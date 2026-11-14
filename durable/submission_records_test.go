package durable

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Consolidated from submission_records_test.go.

func submissionCommit(t *testing.T, s Storage, records ...SubmissionRecord) {
	t.Helper()
	writes := make([]StorageWrite, len(records))
	for i, r := range records {
		writes[i] = StorageWrite{Type: "submission", Value: r}
	}
	result, err := s.Commit(bg, [][]StorageWrite{writes}, nil)
	if err != nil || result.Refused != nil || result.Stored != 1 {
		t.Fatalf("submission commit: %+v %v", result, err)
	}
}
func requireSubmissionEqual(t *testing.T, actual, want SubmissionRecord) {
	t.Helper()
	a, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("record got=%s want=%s", a, b)
	}
}

// Literal storage-conformance.ts:778-852, pinned f9a26365.
func TestStorageSubmissionRequestIndexCompleteReplacementAndPages(t *testing.T) {
	s := taskConformanceStoreV150(t)
	other := mint(t, s)
	result, err := s.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: other}}}}, nil)
	if err != nil || result.Refused != nil {
		t.Fatal(result, err)
	}
	firstID, secondID, otherID := mint(t, s), mint(t, s), mint(t, s)
	first := SubmissionRecord{ID: firstID, Conversation: 1, RequestID: "same", Type: "input", Status: "queued"}
	second := SubmissionRecord{ID: secondID, Conversation: 1, RequestID: "other", Type: "input", Status: "queued"}
	foreign := SubmissionRecord{ID: otherID, Conversation: other, RequestID: "same", Type: "input", Status: "queued"}
	submissionCommit(t, s, first, second, foreign)
	reader := s
	for _, want := range []SubmissionRecord{first, foreign} {
		actual, found, err := reader.SubmissionByRequest(bg, want.Conversation, want.RequestID)
		if err != nil || !found {
			t.Fatal(found, err)
		}
		requireSubmissionEqual(t, actual, want)
	}
	placed := second
	placed.Status, placed.Entry = "placed", mint(t, s)
	submissionCommit(t, s, placed)
	actual, found, err := reader.Submission(bg, secondID)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	requireSubmissionEqual(t, actual, placed)
	actual, found, err = reader.SubmissionByRequest(bg, 1, "other")
	if err != nil || !found {
		t.Fatal(found, err)
	}
	requireSubmissionEqual(t, actual, placed)
	for _, check := range []struct {
		q    SubmissionQuery
		want []ID
	}{
		{SubmissionQuery{}, []ID{firstID, secondID, otherID}}, {SubmissionQuery{Conversation: 1}, []ID{firstID, secondID}},
		{SubmissionQuery{Status: "queued"}, []ID{firstID, otherID}}, {SubmissionQuery{Status: "placed"}, []ID{secondID}},
		{SubmissionQuery{Conversation: other, Status: "queued"}, []ID{otherID}}, {SubmissionQuery{Conversation: other, Status: "placed"}, []ID{}},
	} {
		got := []ID{}
		var cursor Cursor
		for {
			page, err := reader.ScanSubmissions(bg, check.q, 1, cursor)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range page.Items {
				got = append(got, r.ID)
			}
			cursor = page.Next
			if cursor == nil {
				break
			}
		}
		if !reflect.DeepEqual(got, check.want) {
			t.Fatal(check.q, got, check.want)
		}
	}
	page, err := reader.ScanSubmissions(bg, SubmissionQuery{Status: "placed"}, 10, nil)
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	requireSubmissionEqual(t, page.Items[0], placed)
}

// Literal storage-conformance.ts:854-902; referenced entry need not be stored.
func TestStorageSubmissionPassiveWriteLifecycleRecords(t *testing.T) {
	s := taskConformanceStoreV150(t)
	doneID, failedID := mint(t, s), mint(t, s)
	queued := SubmissionRecord{ID: doneID, Conversation: 1, RequestID: "passive-done", Type: "write", Status: "queued"}
	failed := queued
	failed.ID, failed.RequestID = failedID, "passive-failed"
	submissionCommit(t, s, queued, failed)
	done := queued
	done.Status, done.Entry = "done", mint(t, s)
	unanswered := failed
	unanswered.Status, unanswered.Reason, unanswered.Detail = "unanswered", "closed", &TaskValue{Present: true, Value: JSON{"retryable": false}}
	submissionCommit(t, s, done, unanswered)
	reader := s
	for _, want := range []SubmissionRecord{done, unanswered} {
		actual, found, err := reader.Submission(bg, want.ID)
		if err != nil || !found {
			t.Fatal(found, err)
		}
		requireSubmissionEqual(t, actual, want)
		actual, found, err = reader.SubmissionByRequest(bg, 1, want.RequestID)
		if err != nil || !found {
			t.Fatal(found, err)
		}
		requireSubmissionEqual(t, actual, want)
	}
}

// Literal storage-conformance.ts:904-957; continue is entryless and never queued.
func TestStorageSubmissionContinueLifecycleRecords(t *testing.T) {
	s := taskConformanceStoreV150(t)
	doneID, failedID := mint(t, s), mint(t, s)
	placed := SubmissionRecord{ID: doneID, Conversation: 1, RequestID: "continue-done", Type: "continue", Status: "placed"}
	failed := placed
	failed.ID, failed.RequestID = failedID, "continue-failed"
	submissionCommit(t, s, placed, failed)
	reader := s
	actual, found, err := reader.Submission(bg, doneID)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	requireSubmissionEqual(t, actual, placed)
	page, err := reader.ScanSubmissions(bg, SubmissionQuery{Status: "placed"}, 10, nil)
	if err != nil || len(page.Items) != 2 {
		t.Fatal(page, err)
	}
	requireSubmissionEqual(t, page.Items[0], placed)
	requireSubmissionEqual(t, page.Items[1], failed)
	done := placed
	done.Status, done.Answer = "done", mint(t, s)
	unanswered := failed
	unanswered.Status, unanswered.Reason = "unanswered", "nothing_to_answer"
	submissionCommit(t, s, done, unanswered)
	for _, want := range []SubmissionRecord{done, unanswered} {
		actual, found, err := reader.Submission(bg, want.ID)
		if err != nil || !found {
			t.Fatal(found, err)
		}
		requireSubmissionEqual(t, actual, want)
		actual, found, err = reader.SubmissionByRequest(bg, 1, want.RequestID)
		if err != nil || !found {
			t.Fatal(found, err)
		}
		requireSubmissionEqual(t, actual, want)
	}
}

// Narrow source-derived raw replacement probe: identity is not frozen by Storage.
func TestStorageSubmissionRawReplacementChangesIdentityAndRequestIndex(t *testing.T) {
	s := taskConformanceStoreV150(t)
	other, id := mint(t, s), mint(t, s)
	result, err := s.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: other}}}}, nil)
	if err != nil || result.Refused != nil {
		t.Fatal(result, err)
	}
	submissionCommit(t, s, SubmissionRecord{ID: id, Conversation: 1, RequestID: "old", Type: "input", Status: "queued"})
	replacement := SubmissionRecord{ID: id, Conversation: other, RequestIDPresent: true, Type: "write", Status: "unanswered", Reason: "", Detail: &TaskValue{Present: true}}
	submissionCommit(t, s, replacement)
	reader := s
	actual, found, err := reader.Submission(bg, id)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	requireSubmissionEqual(t, actual, replacement)
	if _, found, err := reader.SubmissionByRequest(bg, 1, "old"); err != nil || found {
		t.Fatal("old request key retained", found, err)
	}
	actual, found, err = reader.SubmissionByRequest(bg, other, "")
	if err != nil || !found {
		t.Fatal(found, err)
	}
	requireSubmissionEqual(t, actual, replacement)
}

// The pinned SQLite request index is non-unique. Raw duplicates are records,
// not admission: lookup uses the first indexed row, and reindexing removes it.
func TestStorageSubmissionDuplicateKeysMatchPinnedSQLite(t *testing.T) {
	s := taskConformanceStoreV150(t)
	firstID, secondID := mint(t, s), mint(t, s)
	first := SubmissionRecord{ID: firstID, Conversation: 1, RequestIDPresent: true, Type: "write", Status: "queued"}
	second := first
	second.ID = secondID
	submissionCommit(t, s, second, first)
	check := func(want SubmissionRecord) {
		t.Helper()
		for range 20 {
			actual, found, err := s.SubmissionByRequest(bg, 1, "")
			if err != nil || !found {
				t.Fatal(found, err)
			}
			requireSubmissionEqual(t, actual, want)
		}
	}
	check(first)
	moved := first
	moved.RequestIDPresent, moved.RequestID = false, "moved"
	submissionCommit(t, s, moved)
	check(second)
	actual, found, err := s.SubmissionByRequest(bg, 1, "moved")
	if err != nil || !found {
		t.Fatal(found, err)
	}
	requireSubmissionEqual(t, actual, moved)
}
func TestTxRawSubmissionCreationDoesNotDeduplicate(t *testing.T) {
	s := taskConformanceStoreV150(t)
	session, err := OpenSession(s)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	var first, second ID
	_, err = session.Commit(bg, func(tx *Tx) (any, error) {
		var err error
		createdFirst, err := tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "write", Status: "queued", RequestID: "same"})
		first = createdFirst.ID
		if err != nil {
			return nil, err
		}
		createdSecond, err := tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "write", Status: "queued", RequestID: "same"})
		second = createdSecond.ID
		return nil, err
	})
	if err != nil {
		t.Fatal("raw creation applied Harness deduplication", err)
	}
	if first == second {
		t.Fatal("raw creation reused identity")
	}
	page, err := session.ScanSubmissions(bg, SubmissionQuery{}, 10, nil)
	if err != nil || len(page.Items) != 2 {
		t.Fatal(page, err)
	}
}

func TestStorageSubmissionPrimaryReadsOwnDetailAndTraceThroughReopen(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		id := mint(t, b.store)
		detail := JSON{"nested": []any{"owned"}}
		trace := goai.TelemetryCarrier{"traceparent": "owned"}
		expected := SubmissionRecord{ID: id, Conversation: 1, RequestIDPresent: true, Type: "input", Status: "unanswered", Reason: "", Detail: &TaskValue{Present: true, Value: JSON{"nested": []any{"owned"}}}, Trace: &goai.TelemetryCarrier{"traceparent": "owned"}}
		input := expected
		input.Detail = &TaskValue{Present: true, Value: detail}
		input.Trace = &trace
		submissionCommit(t, b.store, input)
		detail["nested"].([]any)[0] = "input mutation"
		trace["traceparent"] = "input mutation"
		check := func(s Storage) {
			t.Helper()
			for _, missing := range []ID{0, ID(MaxID)} {
				if _, found, err := s.Submission(bg, missing); err != nil || found {
					t.Fatal(found, err)
				}
			}
			first, found, err := s.Submission(bg, id)
			if err != nil || !found {
				t.Fatal(found, err)
			}
			requireSubmissionEqual(t, first, expected)
			first.Detail.Value.(map[string]any)["nested"].([]any)[0] = "lookup mutation"
			(*first.Trace)["traceparent"] = "lookup mutation"
			second, found, err := s.SubmissionByRequest(bg, 1, "")
			if err != nil || !found {
				t.Fatal(found, err)
			}
			requireSubmissionEqual(t, second, expected)
			second.Detail.Value.(map[string]any)["nested"].([]any)[0] = "request mutation"
			(*second.Trace)["traceparent"] = "request mutation"
			page, err := s.ScanSubmissions(bg, SubmissionQuery{Status: "unanswered"}, 1, nil)
			if err != nil || len(page.Items) != 1 || page.Next != nil {
				t.Fatal(page, err)
			}
			requireSubmissionEqual(t, page.Items[0], expected)
			page.Items[0].Detail.Value.(map[string]any)["nested"].([]any)[0] = "scan mutation"
			(*page.Items[0].Trace)["traceparent"] = "scan mutation"
			again, found, err := s.Submission(bg, id)
			if err != nil || !found {
				t.Fatal(found, err)
			}
			requireSubmissionEqual(t, again, expected)
		}
		check(b.store)
		reopened := b.reopen()
		check(reopened)
		if err := reopened.(*SQLiteStorage).Reclaim(bg); err != nil {
			t.Fatal(err)
		}
		check(b.reopen())
	})
}

type submissionReadFaultStorage struct {
	Storage
	session   *Session
	calls     int
	err       error
	lineOwned bool
}

func (a *submissionReadFaultStorage) Submission(_ context.Context, _ ID) (SubmissionRecord, bool, error) {
	a.calls++
	a.lineOwned = a.session.line.held()
	if a.err != nil {
		return SubmissionRecord{}, false, a.err
	}
	return SubmissionRecord{ID: 44, Conversation: 1, Type: "write", Status: "queued"}, true, nil
}
func (a *submissionReadFaultStorage) SubmissionByRequest(ctx context.Context, _ ID, _ string) (SubmissionRecord, bool, error) {
	return a.Submission(ctx, 0)
}
func TestSessionSubmissionReadersPreserveWrapperAndFailureAuthority(t *testing.T) {
	for _, method := range []string{"submission", "request"} {
		t.Run(method, func(t *testing.T) {
			s := gatedSession(t)
			adapter := &submissionReadFaultStorage{Storage: s.store, session: s}
			s.taskBookkeeping(func() { s.store = adapter })
			read := func(ctx context.Context) (SubmissionRecord, bool, error) {
				if method == "submission" {
					return s.Submission(ctx, 2)
				}
				return s.SubmissionByRequest(ctx, 1, "")
			}
			actual, found, err := read(bg)
			if err != nil || !found || actual.ID != 44 || !adapter.lineOwned || adapter.calls != 1 {
				t.Fatal(actual, found, err, adapter.calls)
			}
			adapter.err = storageRequest("submission read request")
			if _, found, err := read(bg); err != adapter.err || found {
				t.Fatal("request classification", found, err)
			}
			adapter.err = nil
			actual, found, err = read(bg)
			if err != nil || !found || actual.ID != 44 {
				t.Fatal("request poisoned session", actual, found, err)
			}
			canceled, cancel := context.WithCancelCause(bg)
			cause := errors.New("caller cancellation cause")
			cancel(cause)
			before := adapter.calls
			if _, found, err := read(canceled); !errors.Is(err, cause) || found || adapter.calls != before {
				t.Fatal("canceled admission reached wrapper", found, err)
			}
			adapter.err = errors.New("submission backend failure")
			if _, found, err := read(bg); err != adapter.err || found {
				t.Fatal("backend cause identity", found, err)
			}
			before = adapter.calls
			_, found, err = read(canceled)
			var failed *SessionFailed
			if !errors.As(err, &failed) || !errors.Is(err, adapter.err) || found || adapter.calls != before {
				t.Fatal("first failure precedence", found, err)
			}
		})
	}
}

// Source harness/submissions.ts:233 delegates deduplication to the Storage
// request index, including when raw records contain duplicate keys.
func TestHarnessSubmissionDedupUsesPrimaryRequestLookup(t *testing.T) {
	s := taskConformanceStoreV150(t)
	firstID, secondID := mint(t, s), mint(t, s)
	first := SubmissionRecord{ID: firstID, Conversation: 1, RequestID: "same", Type: "write", Status: "unanswered", Reason: "closed"}
	second := first
	second.ID, second.Type = secondID, "input"
	submissionCommit(t, s, second, first)
	h := taskTestHarness(t, s)
	conversation, err := h.Conversation(bg, 1)
	if err != nil {
		t.Fatal(err)
	}
	before, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	for range 50 {
		handle, err := conversation.Submit(bg, Input{Type: "write", Entry: &Entry{Kind: "note"}, RequestID: "same"})
		if err != nil || handle.ID() != firstID {
			t.Fatal("dedup bypassed SQLite request index", handle, err)
		}
	}
	after, err := h.Snapshot(bg)
	if err != nil || after.Seq != before.Seq {
		t.Fatal("dedup wrote a replacement", after.Seq, before.Seq, err)
	}
}

// Consolidated from submission_presence_v150_test.go.

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
	_, err = h.Commit(bg, func(tx *Tx) (any, error) {
		var err error
		conv, err := tx.MintID()
		if err != nil {
			return nil, err
		}
		if err := tx.createConversationRecord(Conversation{ID: conv}); err != nil {
			return nil, err
		}
		createdSubmission, err := tx.CreateSubmission(SubmissionCreate{Conversation: conv, Type: "continue", Status: "placed", RequestIDPresent: true})
		id = createdSubmission.ID
		return nil, err
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.Commit(bg, func(tx *Tx) (any, error) {
		record, ok, err := tx.Submission(id)
		if err != nil {
			return nil, err
		}
		if !ok || !record.RequestIDPresent {
			t.Fatalf("tx lost presence %+v", record)
		}
		return nil, tx.SettleSubmission(id, SubmissionSettlement{Status: "unanswered", Reason: ""})
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

// Source Submissions.submit admits inside one commit. Queue a raw creation from
// the storage-read witness: it must run after the admission that owns that read,
// not slip between its request lookup and fresh write.
type submissionAdmissionInterleaveStorage struct {
	Storage
	session      *Session
	once         sync.Once
	queuedResult chan struct {
		record SubmissionRecord
		err    error
	}
}

func (a *submissionAdmissionInterleaveStorage) Snapshot(ctx context.Context) (Snapshot, error) {
	state, err := a.Storage.Snapshot(ctx)
	if err != nil {
		return state, err
	}
	a.once.Do(func() {
		go func() {
			var record SubmissionRecord
			_, err := a.session.Commit(bg, func(tx *Tx) (any, error) {
				var err error
				record, err = tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "write", Status: "unanswered", Reason: "manual", RequestID: "atomic"})
				return nil, err
			})
			a.queuedResult <- struct {
				record SubmissionRecord
				err    error
			}{record, err}
		}()
		a.session.jobs.waitPending(1)
	})
	return state, nil
}
func TestHarnessSubmissionLookupAndWriteUseOneAdmission(t *testing.T) {
	store := taskConformanceStoreV150(t)
	h := taskTestHarness(t, store)
	conversation, err := h.Conversation(bg, 1)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &submissionAdmissionInterleaveStorage{Storage: store, session: h.session, queuedResult: make(chan struct {
		record SubmissionRecord
		err    error
	}, 1)}
	h.session.taskBookkeeping(func() { h.session.store = adapter })
	submitted, err := conversation.Submit(bg, Input{Type: "write", Entry: &Entry{Kind: "note", HasData: true, Data: JSON{"text": "first"}}, RequestID: "atomic"})
	if err != nil {
		t.Fatal(err)
	}
	raw := <-adapter.queuedResult
	if raw.err != nil {
		t.Fatal(raw.err)
	}
	if submitted.ID() >= raw.record.ID {
		t.Fatalf("raw creation overtook the submission owning its read: submitted=%d raw=%d", submitted.ID(), raw.record.ID)
	}
}
