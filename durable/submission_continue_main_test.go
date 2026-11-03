package durable

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// Source-derived lifecycle controls for official f9a26365. These exercise raw
// Session/Harness transactions only, not continue admission or generation.
func TestMainSubmissionContinueRawLifecycle(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h := taskTestHarness(t, b.store)
		var id, answer ID
		_, err := h.Commit(bg, func(tx *Tx) error {
			var err error
			id, err = tx.CreateSubmission(SubmissionRecord{Conversation: 1, RequestID: "continue", Type: "continue", Status: "placed"})
			if err != nil {
				return err
			}
			record, found, err := tx.Submission(id)
			if err != nil || !found || record.Type != "continue" || record.Status != "placed" || record.Entry != 0 || record.Answer != 0 {
				t.Fatalf("placed: %+v %v %v", record, found, err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		native := state.Submissions[id]
		if native.Type != "continue" || native.Status != "pending" || len(state.Entries) != 0 {
			t.Fatalf("native placement: %+v", state)
		}
		if _, found := native.Value["placedEntry"]; found {
			t.Fatal("continue acquired an entry")
		}
		handle, err := h.Submission(bg, id)
		if err != nil {
			t.Fatal(err)
		}
		placed, err := handle.Status(bg)
		if err != nil || placed.Type != "continue" || placed.Status != "placed" || placed.Entry != 0 {
			t.Fatalf("status: %+v %v", placed, err)
		}
		_, err = h.Commit(bg, func(tx *Tx) error {
			var err error
			answer, err = tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.AppendEntry(Entry{ID: answer, Conversation: 1, Kind: "note", Value: JSON{"text": "answer"}}); err != nil {
				return err
			}
			if err := tx.SettleSubmission(id, SubmissionRecord{Status: "done", Answer: answer}); err != nil {
				return err
			}
			// First settlement wins over a later, conflicting receipt.
			if err := tx.SettleSubmission(id, SubmissionRecord{Status: "unanswered", Reason: "late"}); err != nil {
				return err
			}
			got, found, err := tx.Submission(id)
			if err != nil || !found || got.Type != "continue" || got.Status != "done" || got.Entry != 0 || got.Answer != answer {
				t.Fatalf("done: %+v %v %v", got, found, err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		h = taskTestHarness(t, b.reopen())
		handle, err = h.Submission(bg, id)
		if err != nil {
			t.Fatal(err)
		}
		got, err := handle.Status(bg)
		if err != nil || got.Type != "continue" || got.Status != "done" || got.Entry != 0 || got.Answer != answer {
			t.Fatalf("reopen: %+v %v", got, err)
		}
	})
}

func TestMainSubmissionContinueCreateValidation(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h := taskTestHarness(t, b.store)
		var entryID, foreign ID
		_, err := h.Commit(bg, func(tx *Tx) error {
			var err error
			entryID, err = tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.AppendEntry(Entry{ID: entryID, Conversation: 1, Kind: "note", Value: JSON{}}); err != nil {
				return err
			}
			conversation, err := tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.CreateConversation(Conversation{ID: conversation}); err != nil {
				return err
			}
			foreign, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: foreign, Conversation: conversation, Kind: "note", Value: JSON{}})
		})
		if err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			name   string
			record SubmissionRecord
			valid  bool
		}{
			{"placed", SubmissionRecord{Type: "continue", Status: "placed"}, true},
			{"done", SubmissionRecord{Type: "continue", Status: "done", Answer: entryID}, true},
			{"unanswered", SubmissionRecord{Type: "continue", Status: "unanswered", Reason: "nothing_to_answer"}, true},
			{"null-detail", SubmissionRecord{Type: "continue", Status: "unanswered", Reason: "host", Detail: &TaskValue{Present: true}}, true},
			{"queued", SubmissionRecord{Type: "continue", Status: "queued"}, false},
			{"assigned-id", SubmissionRecord{ID: entryID, Type: "continue", Status: "placed"}, false},
			{"invalid-status", SubmissionRecord{Type: "continue", Status: "pending"}, false},
			{"invalid-type", SubmissionRecord{Type: "continuation", Status: "placed"}, false},
			{"invalid-value", SubmissionRecord{Type: "continue", Status: "placed", Value: JSON{"bad": complex(1, 2)}}, false},
			{"invalid-detail", SubmissionRecord{Type: "continue", Status: "unanswered", Reason: "host", Detail: &TaskValue{Value: "absent"}}, false},
			{"payload-detail-placed", SubmissionRecord{Type: "continue", Status: "placed", Value: JSON{"detail": nil}}, false},
			{"payload-detail-done", SubmissionRecord{Type: "continue", Status: "done", Answer: entryID, Value: JSON{"detail": nil}}, false},
			{"placed-entry", SubmissionRecord{Type: "continue", Status: "placed", Entry: entryID}, false},
			{"placed-answer", SubmissionRecord{Type: "continue", Status: "placed", Answer: entryID}, false},
			{"placed-reason", SubmissionRecord{Type: "continue", Status: "placed", Reason: "host"}, false},
			{"placed-detail", SubmissionRecord{Type: "continue", Status: "placed", Detail: &TaskValue{Present: true}}, false},
			{"done-no-answer", SubmissionRecord{Type: "continue", Status: "done"}, false},
			{"done-entry", SubmissionRecord{Type: "continue", Status: "done", Entry: entryID, Answer: entryID}, false},
			{"done-foreign-answer", SubmissionRecord{Type: "continue", Status: "done", Answer: foreign}, false},
			{"done-missing-answer", SubmissionRecord{Type: "continue", Status: "done", Answer: ID(MaxID)}, false},
			{"done-reason", SubmissionRecord{Type: "continue", Status: "done", Answer: entryID, Reason: "host"}, false},
			{"done-detail", SubmissionRecord{Type: "continue", Status: "done", Answer: entryID, Detail: &TaskValue{Present: true}}, false},
			{"unanswered-no-reason", SubmissionRecord{Type: "continue", Status: "unanswered"}, false},
			{"unanswered-entry", SubmissionRecord{Type: "continue", Status: "unanswered", Reason: "host", Entry: entryID}, false},
			{"unanswered-answer", SubmissionRecord{Type: "continue", Status: "unanswered", Reason: "host", Answer: entryID}, false},
			{"input-entryless-placed", SubmissionRecord{Type: "input", Status: "placed"}, false},
			{"input-entryless-done", SubmissionRecord{Type: "input", Status: "done", Answer: entryID}, false},
			{"input-answerless-done", SubmissionRecord{Type: "input", Status: "done", Entry: entryID}, false},
			{"write-placed", SubmissionRecord{Type: "write", Status: "placed", Entry: entryID}, false},
			{"write-entryless-done", SubmissionRecord{Type: "write", Status: "done"}, false},
			{"write-answer", SubmissionRecord{Type: "write", Status: "done", Entry: entryID, Answer: entryID}, false},
			{"input-done", SubmissionRecord{Type: "input", Status: "done", Entry: entryID, Answer: entryID}, true},
			{"write-done", SubmissionRecord{Type: "write", Status: "done", Entry: entryID}, true},
		}
		for _, field := range []string{"placedEntry", "entry", "answerEntry", "answer"} {
			cases = append(cases, struct {
				name   string
				record SubmissionRecord
				valid  bool
			}{"payload-" + field, SubmissionRecord{Type: "continue", Status: "placed", Value: JSON{field: nil}}, false})
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				before, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				record := tc.record
				record.Conversation = 1
				_, err = h.Commit(bg, func(tx *Tx) error { _, err := tx.CreateSubmission(record); return err })
				if tc.valid {
					if err != nil {
						t.Fatal(err)
					}
					return
				}
				rejected(t, err)
				after, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				if after.Seq != before.Seq || !reflect.DeepEqual(after.Submissions, before.Submissions) {
					t.Fatal("rejected creation changed records")
				}
			})
		}
	})
}

func TestMainSubmissionContinueStorageUnion(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s := b.store
		answer := mint(t, s)
		apply(t, s, entry(answer, JSON{}))
		conversation := mint(t, s)
		apply(t, s, Write{Op: "create-conversation", Conversation: &Conversation{ID: conversation}})
		foreign := mint(t, s)
		apply(t, s, Write{Op: "append-entry", Entry: &Entry{ID: foreign, Conversation: conversation, Kind: "note", Value: JSON{}}})
		cases := []struct {
			name, status string
			value        JSON
			valid        bool
		}{
			{"placed", "pending", JSON{}, true},
			{"running", "running", JSON{}, true},
			{"done", "done", JSON{"answerEntry": answer}, true},
			{"failed", "failed", JSON{"errorCode": "model_error", "detail": nil}, true},
			{"aborted", "aborted", JSON{"errorCode": "aborted"}, true},
			{"queued", "queued", JSON{}, false},
			{"nil-value", "pending", nil, false},
			{"placed-entry", "pending", JSON{"placedEntry": nil}, false},
			{"legacy-entry", "pending", JSON{"entry": map[string]any{}}, false},
			{"legacy-answer", "pending", JSON{"answer": answer}, false},
			{"placed-answer", "pending", JSON{"answerEntry": answer}, false},
			{"placed-null-answer", "pending", JSON{"answerEntry": nil}, false},
			{"placed-reason", "pending", JSON{"errorCode": "host"}, false},
			{"placed-detail", "pending", JSON{"detail": nil}, false},
			{"done-no-answer", "done", JSON{}, false},
			{"done-zero-answer", "done", JSON{"answerEntry": 0}, false},
			{"done-string-answer", "done", JSON{"answerEntry": "2"}, false},
			{"done-fractional-answer", "done", JSON{"answerEntry": 2.5}, false},
			{"done-out-of-range-answer", "done", JSON{"answerEntry": uint64(MaxID) + 1}, false},
			{"done-missing-answer", "done", JSON{"answerEntry": ID(MaxID)}, false},
			{"done-foreign-answer", "done", JSON{"answerEntry": foreign}, false},
			{"done-placed-entry", "done", JSON{"answerEntry": answer, "placedEntry": answer}, false},
			{"done-reason", "done", JSON{"answerEntry": answer, "errorCode": "host"}, false},
			{"done-detail", "done", JSON{"answerEntry": answer, "detail": nil}, false},
			{"failed-no-reason", "failed", JSON{}, false},
			{"failed-empty-reason", "failed", JSON{"errorCode": ""}, false},
			{"failed-nonstring-reason", "failed", JSON{"errorCode": 3}, false},
			{"failed-answer", "failed", JSON{"errorCode": "host", "answerEntry": answer}, false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				id := mint(t, s)
				before := snap(t, s)
				sub := Submission{ID: id, Conversation: 1, Type: "continue", Status: tc.status, Value: tc.value}
				_, err := s.Apply(bg, Batch{Writes: []Write{{Op: "put-submission", Submission: &sub}}})
				if tc.valid {
					if err != nil {
						t.Fatal(err)
					}
					got := publicSubmission(snap(t, s).Submissions[id])
					if got.Type != "continue" || got.Entry != 0 || !terminalStatus(tc.status) && got.Status != "placed" {
						t.Fatalf("projection: %+v", got)
					}
					return
				}
				rejected(t, err)
				if after := snap(t, s); !reflect.DeepEqual(before, after) {
					t.Fatal("rejected batch changed state")
				}
			})
		}
		// Final reference validation also resolves a forward answer in one batch.
		forward, receipt := mint(t, s), mint(t, s)
		apply(t, s,
			Write{Op: "put-submission", Submission: &Submission{ID: receipt, Conversation: 1, Type: "continue", Status: "done", Value: JSON{"answerEntry": forward}}},
			entry(forward, JSON{}),
		)
		id := mint(t, s)
		apply(t, s, Write{Op: "put-submission", Submission: &Submission{ID: id, Conversation: 1, RequestID: "same", Type: "continue", Status: "pending", Value: JSON{}}})
		duplicate := mint(t, s)
		for _, sub := range []Submission{
			{ID: duplicate, Conversation: 1, RequestID: "same", Type: "continue", Status: "pending", Value: JSON{}},
			{ID: id, Conversation: conversation, RequestID: "same", Type: "continue", Status: "pending", Value: JSON{}},
			{ID: id, Conversation: 1, RequestID: "other", Type: "continue", Status: "pending", Value: JSON{}},
			{ID: id, Conversation: 1, RequestID: "same", Type: "follow-up", Status: "pending", Value: JSON{}},
		} {
			_, err := s.Apply(bg, Batch{Writes: []Write{{Op: "put-submission", Submission: &sub}}})
			rejected(t, err)
		}
		reopened := b.reopen()
		if got := publicSubmission(snap(t, reopened).Submissions[id]); got.Type != "continue" || got.Status != "placed" || got.Entry != 0 {
			t.Fatalf("reopen: %+v", got)
		}
	})
}

func TestMainSubmissionContinuePlacementSettlementAndAbort(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h := taskTestHarness(t, b.store)
		var id, answer, foreign ID
		_, err := h.Commit(bg, func(tx *Tx) error {
			var err error
			id, err = tx.CreateSubmission(SubmissionRecord{Conversation: 1, Type: "continue", Status: "placed"})
			if err != nil {
				return err
			}
			answer, err = tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.AppendEntry(Entry{ID: answer, Conversation: 1, Kind: "note", Value: JSON{}}); err != nil {
				return err
			}
			conversation, err := tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.CreateConversation(Conversation{ID: conversation}); err != nil {
				return err
			}
			foreign, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: foreign, Conversation: conversation, Kind: "note", Value: JSON{}})
		})
		if err != nil {
			t.Fatal(err)
		}
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range []ID{0, answer, foreign, ID(MaxID)} {
			_, err := h.Commit(bg, func(tx *Tx) error { return tx.PlaceSubmission(id, entry) })
			rejected(t, err)
		}
		for _, settlement := range []SubmissionRecord{
			{Status: "done"}, {Status: "done", Answer: foreign}, {Status: "done", Answer: ID(MaxID)},
			{Status: "done", Entry: answer, Answer: answer}, {Status: "done", Answer: answer, Reason: "host"},
			{Status: "done", Answer: answer, Detail: &TaskValue{Present: true}},
			{Status: "unanswered"}, {Status: "unanswered", Reason: "host", Answer: answer},
			{Status: "unanswered", Reason: "host", Entry: answer}, {Status: "queued"},
		} {
			_, err := h.Commit(bg, func(tx *Tx) error { return tx.SettleSubmission(id, settlement) })
			rejected(t, err)
		}
		for _, scope := range [][]ID{nil, {1}, {ID(MaxID)}} {
			result, err := h.AbortSubmission(bg, id, scope...)
			want := "already_placed"
			if len(scope) > 0 && scope[0] != 1 {
				want = "not_found"
			}
			if err != nil || result != want {
				t.Fatal(result, err)
			}
		}
		handle, err := h.Submission(bg, id)
		if err != nil {
			t.Fatal(err)
		}
		if result, err := handle.Abort(bg); err != nil || result != "already_placed" {
			t.Fatal(result, err)
		}
		if err := handle.Withdraw(bg); err == nil {
			t.Fatal("continue withdrawn")
		}
		after, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatal("rejected placement/settlement or abort disposition mutated state")
		}
		_, err = h.Commit(bg, func(tx *Tx) error {
			return tx.SettleSubmission(id, SubmissionRecord{Status: "unanswered", Reason: "nothing_to_answer"})
		})
		if err != nil {
			t.Fatal(err)
		}
		if result, err := handle.Abort(bg); err != nil || result != "settled" {
			t.Fatal(result, err)
		}
		got, err := handle.Status(bg)
		if err != nil || got.Type != "continue" || got.Status != "unanswered" || got.Entry != 0 || got.Answer != 0 || got.Reason != "nothing_to_answer" || got.Detail != nil {
			t.Fatalf("unanswered: %+v %v", got, err)
		}
	})
}

func TestMainSubmissionContinueOwnershipAndCopies(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h := taskTestHarness(t, b.store)
		value := JSON{"nested": []any{"kept"}}
		detail := JSON{"nested": []any{"detail"}}
		var id ID
		var escaped *Tx
		_, err := h.Commit(bg, func(tx *Tx) error {
			escaped = tx
			tx.busy.Store(true)
			if _, err := tx.CreateSubmission(SubmissionRecord{}); !errors.Is(err, ErrConcurrent) {
				t.Fatal(err)
			}
			if _, _, err := tx.Submission(1); !errors.Is(err, ErrConcurrent) {
				t.Fatal(err)
			}
			if err := tx.PlaceSubmission(1, 0); !errors.Is(err, ErrConcurrent) {
				t.Fatal(err)
			}
			if err := tx.SettleSubmission(1, SubmissionRecord{}); !errors.Is(err, ErrConcurrent) {
				t.Fatal(err)
			}
			tx.busy.Store(false)
			var err error
			id, err = tx.CreateSubmission(SubmissionRecord{Conversation: 1, Type: "continue", Status: "placed", Value: value})
			if err != nil {
				return err
			}
			value["nested"].([]any)[0] = "caller mutation"
			if err := tx.SettleSubmission(id, SubmissionRecord{Status: "unanswered", Reason: "host", Detail: &TaskValue{Present: true, Value: detail}}); err != nil {
				return err
			}
			detail["nested"].([]any)[0] = "caller mutation"
			record, _, err := tx.Submission(id)
			if err != nil {
				return err
			}
			record.Value["nested"].([]any)[0] = "read mutation"
			record.Detail.Value.(map[string]any)["nested"].([]any)[0] = "read mutation"
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := escaped.CreateSubmission(SubmissionRecord{}); !errors.Is(err, ErrSealed) {
			t.Fatal(err)
		}
		if _, _, err := escaped.Submission(id); !errors.Is(err, ErrSealed) {
			t.Fatal(err)
		}
		if err := escaped.PlaceSubmission(id, 0); !errors.Is(err, ErrSealed) {
			t.Fatal(err)
		}
		if err := escaped.SettleSubmission(id, SubmissionRecord{}); !errors.Is(err, ErrSealed) {
			t.Fatal(err)
		}
		if _, err := b.store.MintID(bg); !errors.Is(err, ErrOwned) {
			t.Fatal(err)
		}
		if _, err := b.store.Apply(bg, Batch{Writes: []Write{{Op: "put-submission", Submission: &Submission{ID: id, Conversation: 1, Type: "continue", Status: "pending", Value: JSON{}}}}}); !errors.Is(err, ErrOwned) {
			t.Fatal(err)
		}
		handle, err := h.Submission(bg, id)
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			got, err := handle.Status(bg)
			if err != nil || got.Type != "continue" || got.Entry != 0 || got.Answer != 0 || got.Reason != "host" || got.Detail == nil || !got.Detail.Present {
				t.Fatalf("status: %+v %v", got, err)
			}
			if !equalJSONValue(got.Value["nested"], []any{"kept"}) || !equalJSONValue(got.Detail.Value, JSON{"nested": []any{"detail"}}) {
				t.Fatalf("aliased: %+v", got)
			}
			got.Value["nested"].([]any)[0] = "handle mutation"
			got.Detail.Value.(map[string]any)["nested"].([]any)[0] = "handle mutation"
		}
		ctx, cancel := context.WithCancel(bg)
		cancel()
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.Commit(ctx, func(tx *Tx) error { t.Fatal("canceled commit entered"); return nil })
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		after, err := h.Snapshot(bg)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("canceled commit mutated", err)
		}
	})
}

// Regression controls for the ordinary paths whose predicates gained continue.
func TestMainSubmissionContinueOrdinaryInputWriteControls(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h := taskTestHarness(t, b.store)
		var input, write, answer ID
		_, err := h.Commit(bg, func(tx *Tx) error {
			var err error
			input, err = tx.CreateSubmission(SubmissionRecord{Conversation: 1, Type: "input", Status: "queued"})
			if err != nil {
				return err
			}
			write, err = tx.CreateSubmission(SubmissionRecord{Conversation: 1, Type: "write", Status: "queued"})
			if err != nil {
				return err
			}
			answer, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: answer, Conversation: 1, Kind: "note", Value: JSON{}})
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []ID{input, write} {
			_, err := h.Commit(bg, func(tx *Tx) error { return tx.SettleSubmission(id, SubmissionRecord{Status: "done", Answer: answer}) })
			rejected(t, err)
			_, err = h.Commit(bg, func(tx *Tx) error { return tx.PlaceSubmission(id, 0) })
			rejected(t, err)
		}
		_, err = h.Commit(bg, func(tx *Tx) error {
			if err := tx.PlaceSubmission(input, answer); err != nil {
				return err
			}
			if err := tx.SettleSubmission(input, SubmissionRecord{Status: "done", Answer: answer}); err != nil {
				return err
			}
			return tx.PlaceSubmission(write, answer)
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.Commit(bg, func(tx *Tx) error {
			for _, id := range []ID{input, write} {
				got, found, err := tx.Submission(id)
				if err != nil || !found || got.Status != "done" || got.Entry != answer {
					t.Fatalf("ordinary receipt: %+v %v %v", got, found, err)
				}
				if id == input && (got.Type != "input" || got.Answer != answer) || id == write && (got.Type != "write" || got.Answer != 0) {
					t.Fatalf("ordinary answer: %+v", got)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}
