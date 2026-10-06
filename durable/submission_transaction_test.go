package durable

import "testing"

func TestSubmissionTransactionCurrentRecordPlacementFirstSettlementAndDetached(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		h := taskTestHarness(t, b.store)
		var entry, input, write, placed ID
		_, err := h.Commit(bg, func(tx *Tx) error {
			var err error
			entry, err = tx.MintID()
			if err != nil {
				return err
			}
			if err = tx.AppendEntry(Entry{ID: entry, Conversation: 1, Kind: "note", Value: JSON{}}); err != nil {
				return err
			}
			input, err = tx.CreateSubmission(SubmissionRecord{Conversation: 1, Type: "input", Status: "queued"})
			if err != nil {
				return err
			}
			write, err = tx.CreateSubmission(SubmissionRecord{Conversation: 1, Type: "write", Status: "queued"})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []ID{input, write} {
			if _, err := h.Commit(bg, func(tx *Tx) error {
				return tx.SettleSubmission(id, SubmissionRecord{Status: "unanswered", Reason: "aborted", Answer: entry})
			}); err == nil {
				t.Fatal("unanswered settlement accepted answer", id)
			}
			_, err := h.Commit(bg, func(tx *Tx) error { return tx.SettleSubmission(id, SubmissionRecord{Status: "done", Answer: entry}) })
			if err == nil {
				t.Fatal("unplaced submission answered", id)
			}
		}
		_, err = h.Commit(bg, func(tx *Tx) error {
			return tx.SettleSubmission(ID(MaxID), SubmissionRecord{Status: "unanswered", Reason: "missing"})
		})
		if err == nil {
			t.Fatal("missing submission settled")
		}
		_, err = h.Commit(bg, func(tx *Tx) error {
			var err error
			placed, err = tx.CreateSubmission(SubmissionRecord{Conversation: 1, Type: "input", Status: "placed", Entry: entry})
			if err != nil {
				return err
			}
			if err := tx.SettleSubmission(placed, SubmissionRecord{Status: "done", Answer: entry}); err != nil {
				return err
			}
			if err := tx.SettleSubmission(placed, SubmissionRecord{Status: "unanswered", Reason: "late"}); err != nil {
				return err
			}
			record, found, err := tx.Submission(placed)
			if err != nil || !found || record.Status != "done" || record.Entry != entry || record.Answer != entry {
				t.Fatal(record, found, err)
			}
			record.Value["placedEntry"] = ID(MaxID)
			if err := tx.PlaceSubmission(input, entry); err != nil {
				return err
			}
			return tx.PlaceSubmission(write, entry)
		})
		if err != nil {
			t.Fatal(err)
		}
		var unanswered ID
		_, err = h.Commit(bg, func(tx *Tx) error {
			var err error
			unanswered, err = tx.CreateSubmission(SubmissionRecord{Conversation: 1, Type: "input", Status: "queued"})
			if err != nil {
				return err
			}
			if err := tx.SettleSubmission(unanswered, SubmissionRecord{Status: "unanswered", Reason: "host", Detail: &TaskValue{Present: true, Value: nil}}); err != nil {
				return err
			}
			record, found, err := tx.Submission(unanswered)
			if err != nil || !found || record.Detail == nil || !record.Detail.Present || record.Detail.Value != nil {
				t.Fatal("null detail lost", record, err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		nullHandle, err := h.Submission(bg, unanswered)
		if err != nil {
			t.Fatal(err)
		}
		nullRecord, err := nullHandle.Status(bg)
		if err != nil || nullRecord.Detail == nil || !nullRecord.Detail.Present || nullRecord.Detail.Value != nil {
			t.Fatal("committed null detail lost", nullRecord, err)
		}
		nullRecord.Detail.Value = JSON{"mutated": true}
		again, err := nullHandle.Status(bg)
		if err != nil || again.Detail == nil || again.Detail.Value != nil {
			t.Fatal("detail retrieval aliases storage", again, err)
		}
		candidate := JSON{"nested": []any{"kept"}}
		var detailed ID
		_, err = h.Commit(bg, func(tx *Tx) error {
			var err error
			detailed, err = tx.CreateSubmission(SubmissionRecord{Conversation: 1, Type: "input", Status: "unanswered", Reason: "host", Detail: &TaskValue{Present: true, Value: candidate}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		candidate["nested"].([]any)[0] = "caller mutation"
		detailHandle, err := h.Submission(bg, detailed)
		if err != nil {
			t.Fatal(err)
		}
		detailRecord, err := detailHandle.Status(bg)
		if err != nil || detailRecord.Detail == nil || !equalJSONValue(detailRecord.Detail.Value, JSON{"nested": []any{"kept"}}) {
			t.Fatal("detail candidate aliased", detailRecord, err)
		}
		detailRecord.Detail.Value.(map[string]any)["nested"].([]any)[0] = "read mutation"
		detailAgain, err := detailHandle.Status(bg)
		if err != nil || !equalJSONValue(detailAgain.Detail.Value, JSON{"nested": []any{"kept"}}) {
			t.Fatal("detail read aliased", detailAgain, err)
		}
		for _, id := range []ID{placed, input, write} {
			sub, err := h.Submission(bg, id)
			if err != nil {
				t.Fatal(err)
			}
			record, err := sub.Status(bg)
			if err != nil || record.Entry != entry {
				t.Fatal(record, err)
			}
			if id == input && record.Status != "placed" || id != input && record.Status != "done" {
				t.Fatal(record)
			}
		}
	})
}
