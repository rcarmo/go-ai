package durable

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

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
	if actual.Value != nil {
		t.Fatal("private submission payload leaked", actual.Value)
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
	_, err = session.Commit(bg, func(tx *Tx) error {
		var err error
		first, err = tx.CreateSubmission(SubmissionRecord{Conversation: 1, Type: "write", Status: "queued", RequestID: "same"})
		if err != nil {
			return err
		}
		second, err = tx.CreateSubmission(SubmissionRecord{Conversation: 1, Type: "write", Status: "queued", RequestID: "same"})
		return err
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
