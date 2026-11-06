package durable

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Source-derived from the actual original output in storage-boundaries.json.
// This packet isolates empty batches and empty continue reasons; explicit IDs
// are covered in storage_explicit_id_v150_test.go. Root bootstrap remains a D28
// gap. No fixture payload is rewritten.
func TestV150StorageEmptyBatch(t *testing.T) {
	raw, err := os.ReadFile("testdata/pi-durable-v150/storage-boundaries.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		EmptyBatch struct{ Stored int }
		Receipts   []struct {
			Index int
			Seq   uint64
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "store.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close(bg) }()
	before := snap(t, store)
	var receipts [][2]uint64
	result, err := store.Commit(bg, [][]StorageWrite{{}}, func(i int, seq uint64) { receipts = append(receipts, [2]uint64{uint64(i), seq}) })
	if err != nil || result.Refused != nil || result.Stored != fixture.EmptyBatch.Stored {
		t.Fatalf("empty batch: %+v %v", result, err)
	}
	want := [][2]uint64{{uint64(fixture.Receipts[0].Index), fixture.Receipts[0].Seq}}
	if !reflect.DeepEqual(receipts, want) {
		t.Fatalf("receipts %v want %v", receipts, want)
	}
	after := snap(t, store)
	if after.Seq != before.Seq+1 || after.HighWater != before.HighWater || !reflect.DeepEqual(after.Conversations, before.Conversations) || len(after.Entries) != 0 {
		t.Fatal("empty commit changed records/allocator", after)
	}
	// Empty outer calls consume no sequence; each inner empty batch consumes one.
	result, err = store.Commit(bg, nil, nil)
	if err != nil || result.Stored != 0 || result.Refused != nil || snap(t, store).Seq != after.Seq {
		t.Fatalf("empty call: %+v %v", result, err)
	}
	result, err = store.Commit(bg, [][]StorageWrite{nil, {}}, nil)
	if err != nil || result.Stored != 2 || result.Refused != nil {
		t.Fatalf("two empty batches: %+v %v", result, err)
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	store, err = OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if snap(t, store).Seq != 3 {
		t.Fatal("empty commits lost on replay")
	}
	if err := store.Reclaim(bg); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	store, err = OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if snap(t, store).Seq != 3 {
		t.Fatal("empty commits lost on checkpoint replay")
	}
}

func TestV150SubmissionEmptyContinueReason(t *testing.T) {
	raw, err := os.ReadFile("testdata/pi-durable-v150/storage-boundaries.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Conversation     Conversation
		Submission       SubmissionRecord
		StoredSubmission json.RawMessage
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		for mint(t, b.store) < fixture.Submission.ID {
		}
		result, err := b.store.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: fixture.Conversation}}, {{Type: "submission", Value: fixture.Submission}}}, nil)
		if err != nil || result.Refused != nil || result.Stored != 2 {
			t.Fatalf("continue empty reason: %+v %v", result, err)
		}
		check := func(store Storage) {
			state := snap(t, store)
			got := publicSubmission(state.Submissions[fixture.Submission.ID])
			var expected any
			if err := json.Unmarshal(fixture.StoredSubmission, &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(canonicalJSON(t, got), expected) {
				t.Fatalf("stored %s want %s", marshalOrFail(t, got), fixture.StoredSubmission)
			}
		}
		check(b.store)
		check(b.reopen())
	})
}

func TestV150SubmissionEmptyReasonSettlement(t *testing.T) {
	for _, kind := range []string{"input", "write", "continue"} {
		t.Run(kind, func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			session, err := OpenSession(store)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close(bg)
			var id ID
			_, err = session.Commit(bg, func(tx *Tx) error {
				var err error
				status := "queued"
				if kind == "continue" {
					status = "placed"
				}
				id, err = tx.CreateSubmission(SubmissionRecord{Conversation: 1, Type: kind, Status: status})
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = session.Commit(bg, func(tx *Tx) error { return tx.SettleSubmission(id, SubmissionRecord{Status: "unanswered", Reason: ""}) })
			if err != nil {
				t.Fatal("empty reason settlement", err)
			}
			_, err = session.Commit(bg, func(tx *Tx) error {
				record, ok, err := tx.Submission(id)
				if err != nil {
					return err
				}
				if !ok || record.Status != "unanswered" || record.Reason != "" {
					t.Fatalf("settled %+v", record)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestV150StorageEmptyBatchRollback(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	failure := reject("empty group backend failure")
	store.appendFrames = func([]storageFrame) error { return failure }
	receipts := 0
	result, err := store.Commit(bg, [][]StorageWrite{{}, {}}, func(int, uint64) { receipts++ })
	if !errors.Is(err, failure) || result.Stored != 0 || receipts != 0 || snap(t, store).Seq != 0 {
		t.Fatalf("failed empty group: %+v %v receipts=%d", result, err, receipts)
	}
}

func TestV150StorageEmptyBatchRealRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close(bg) }()
	if err := store.exec("CREATE TRIGGER reject_second_empty BEFORE INSERT ON frames WHEN NEW.kind=2 AND NEW.ordinal=2 BEGIN SELECT RAISE(ABORT,'second empty frame rejected'); END;"); err != nil {
		t.Fatal(err)
	}
	receipts := 0
	result, err := store.Commit(bg, [][]StorageWrite{{}, {}}, func(int, uint64) { receipts++ })
	if err == nil || result.Stored != 0 || receipts != 0 || snap(t, store).Seq != 0 {
		t.Fatalf("SQL rollback: %+v %v receipts=%d", result, err, receipts)
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	store, err = OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if snap(t, store).Seq != 0 {
		t.Fatal("first empty frame escaped rollback")
	}
	if err := store.exec("DROP TRIGGER reject_second_empty"); err != nil {
		t.Fatal(err)
	}
	result, err = store.Commit(bg, [][]StorageWrite{{}, {}}, nil)
	if err != nil || result.Refused != nil || result.Stored != 2 {
		t.Fatalf("after rollback: %+v %v", result, err)
	}
}

func TestV150StorageEmptyBatchRefusalKeepsPrefix(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var receipts []uint64
		result, err := b.store.Commit(bg, [][]StorageWrite{{}, {{Type: "unknown"}}, {}}, func(_ int, seq uint64) { receipts = append(receipts, seq) })
		if err != nil || result.Stored != 1 || result.Refused == nil || !reflect.DeepEqual(receipts, []uint64{1}) {
			t.Fatalf("refusal prefix: %+v %v %v", result, err, receipts)
		}
		if state := snap(t, b.reopen()); state.Seq != 1 || len(state.Entries) != 0 {
			t.Fatal("refusal prefix lost", state)
		}
	})
}

func TestV150ContinueReasonStillRequired(t *testing.T) {
	var record SubmissionRecord
	if err := json.Unmarshal([]byte(`{"id":2,"conversationId":1,"type":"continue","status":"unanswered"}`), &record); err == nil {
		t.Fatal("missing reason accepted on the public wire")
	}
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	id := mint(t, store)
	for _, value := range []JSON{{}, {"errorCode": nil}, {"errorCode": 42}, {"errorCode": "", "answerEntry": ID(99)}} {
		_, err := store.Apply(bg, Batch{Writes: []Write{{Op: "put-submission", Submission: &Submission{ID: id, Conversation: 1, Type: "continue", Status: "failed", Value: value}}}})
		if err == nil || snap(t, store).Seq != 0 {
			t.Fatalf("invalid reason/union accepted: %v %v", value, err)
		}
	}
}
