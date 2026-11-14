package durable

import (
	"context"
	"math"
	"strings"
	"testing"
)

// f9a26365 harness-deferred-progress.test.ts:430-475: an actual third-party
// native task, not Harness.Commit, reads progress through public Runtime.Commit.
// The Go document-ID and object-valued Entry.Value adapters are explicit below.
func TestMainHarnessThirdPartyCommitStoresDerivedPartialAtomically(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		ref, options, streams, clock := mainRecoverySetup(t)
		audit := taskDefinition(t, "test.audit", func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			input, ok := task.Input.Value.(map[string]any)
			if !ok {
				return reject("invalid audit input")
			}
			number, ok := exactNumber(input["documentId"])
			if !ok || !number.IsInt() || !number.Num().IsUint64() {
				return reject("invalid audit document")
			}
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				// Third-party callers can address a known builtin document by public ID.
				live, err := tx.Document(ID(number.Num().Uint64()))
				if err != nil {
					return nil, err
				}
				value, err := live.Get()
				if err != nil {
					return nil, err
				}
				text, _ := mainRecoveryText(value)
				id, err := tx.MintID()
				if err != nil {
					return nil, err
				}
				// Native Entry.Value is an object rather than source Entry.data:string.
				if err := tx.appendEntryRecord(Entry{ID: id, Conversation: r.ConversationID(), Kind: "test.audit", Value: JSON{"text": text}}); err != nil {
					return nil, err
				}
				return taskDone(nil), nil
			})
		})
		options.Registry = NewRegistry()
		if _, err := options.Registry.RegisterTask(audit); err != nil {
			t.Fatal(err)
		}
		f := mainOpenSlowRecovery(t, b.store, ref, options, clock, math.Inf(1))
		sub, err := f.c.Submit(bg, Input{Content: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		stream := watchReceive(t, streams)
		held := strings.Repeat("streamed ", 8) // a >40-character prefix of source LONG
		f.partial(stream, held)
		before := f.stored()
		if text, ok := mainRecoveryText(f.live(before)); ok {
			t.Fatal("partial already stored before third-party read", text)
		}
		var liveID ID
		for id, doc := range before.Documents {
			if doc.Owner == f.c.ID() && doc.Kind == "pi.live" {
				liveID = id
			}
		}
		if liveID == 0 {
			t.Fatal("live document missing")
		}
		batches := make(chan []Write, 1)
		f.h.session.taskBookkeeping(func() {
			core := f.h.session.core
			original := core.appendFrame
			core.appendFrame = func(kind byte, seq, highWater uint64, data []byte) error {
				if kind == 2 {
					var record commitRecord
					if err := decodeStrict(data, core.limits, core.limits.MaxRecordBytes, &record); err != nil {
						return err
					}
					for _, write := range record.Writes {
						if write.Entry != nil && write.Entry.Kind == "test.audit" {
							select {
							case batches <- record.Writes:
							default:
								return reject("unexpected repeated audit transaction")
							}
							break
						}
					}
				}
				if original != nil {
					return original(kind, seq, highWater, data)
				}
				return nil
			}
		})
		var taskID ID
		if _, err := f.h.commitTasks(bg, f.c.ID(), func(tx *Tx) error {
			var err error
			taskID, err = tx.CreateTask(audit, JSON{"conversationId": f.c.ID(), "documentId": liveID}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		result := waitPublicTask(t, f.h, taskID)
		if result.State.Outcome == nil || result.State.Outcome.Status != "completed" {
			t.Fatal(result)
		}
		writes := watchReceive(t, batches)
		changed := false
		derived := ""
		for _, write := range writes {
			if write.Op == "delta-document" && write.Delta != nil && write.Delta.ID == liveID {
				changed = true
			}
			if write.Entry != nil && write.Entry.Kind == "test.audit" {
				derived, _ = write.Entry.Value["text"].(string)
			}
		}
		if !changed {
			t.Error("audit storage transaction lacks live document.change")
		}
		if len(derived) <= 40 || derived != held {
			t.Fatal("wrong derived partial", derived)
		}
		store := f.crash()
		stored, err := store.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if text, ok := mainRecoveryText(f.live(stored)); !ok || text != derived {
			t.Error("crash storage differs from derived text", text, derived)
		}
		f = mainOpenSlowRecovery(t, store, ref, options, clock, math.Inf(1))
		mainFinishRecovery(t, f, streams, sub.ID(), []JSON{{"text": derived, "stopReason": "aborted"}, {"text": "answer", "stopReason": "stop"}})
	})
}
