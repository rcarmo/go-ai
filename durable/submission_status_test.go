package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestSubmissionReferenceStatusAndAbortDispositionScopeQueuedWrites(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		entered, release := make(chan struct{}), make(chan struct{})
		calls := 0
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			calls++
			ch := make(chan goai.Event, 1)
			if calls == 1 {
				close(entered)
				go func() { <-release; ch <- terminal("answer"); close(ch) }()
			} else {
				ch <- terminal("extra")
				close(ch)
			}
			return ch
		})
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		root := root(t, h, ref)
		first, err := root.Submit(bg, Input{Content: "first"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		queued, err := root.Submit(bg, Input{Content: "queued"})
		if err != nil {
			t.Fatal(err)
		}
		write, err := root.Submit(bg, Input{Type: "write", Content: "never placed"})
		if err != nil {
			t.Fatal(err)
		}
		status, err := queued.Status(bg)
		if err != nil || status.Status != "queued" {
			t.Fatal(status, err)
		}
		status.Entry = ID(MaxID) // a returned record cannot mutate committed state
		// Session applications may create a queued submission without
		// Harness admission/inbox placement; abort must still settle it.
		var raw ID
		_, err = h.commitTasks(bg, root.ID(), func(tx *Tx) error {
			var err error
			raw, err = tx.mintID()
			if err != nil {
				return err
			}
			return tx.putSubmissionRecord(Submission{ID: raw, Conversation: root.ID(), Type: "write", Status: "pending", Value: JSON{}})
		})
		if err != nil {
			t.Fatal(err)
		}
		if result, err := h.AbortSubmission(bg, raw); err != nil || result != "aborted" {
			t.Fatal("raw queued abort", result, err)
		}
		if absent, err := h.Submission(bg, ID(MaxID)); err != nil || absent != nil {
			t.Fatal("unknown submission lookup", absent, err)
		}
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if result, err := h.AbortSubmission(bg, queued.ID(), ID(MaxID)); err != nil || result != "not_found" {
			t.Fatal(result, err)
		}
		if result, err := h.AbortSubmission(bg, ID(MaxID)); err != nil || result != "not_found" {
			t.Fatal(result, err)
		}
		if result, err := first.Abort(bg); err != nil || result != "already_placed" {
			t.Fatal(result, err)
		}
		after, err := h.Snapshot(bg)
		if err != nil || after.Seq != before.Seq {
			t.Fatal("lookup disposition wrote", err)
		}
		for _, sub := range []*SubmissionHandle{queued, write} {
			if result, err := sub.Abort(bg); err != nil || result != "aborted" {
				t.Fatal(result, err)
			}
			if result, err := sub.Abort(bg); err != nil || result != "settled" {
				t.Fatal(result, err)
			}
			status, err := sub.Status(bg)
			if err != nil || status.Status != "unanswered" || status.Reason != "aborted" {
				t.Fatal(status, err)
			}
		}
		releaseTaskGate(release)
		if final := waitSubmission(t, first); final.Submission.Status != "done" {
			t.Fatal(final)
		}
		if result, err := first.Abort(bg); err != nil || result != "settled" {
			t.Fatal(result, err)
		}
		if calls != 1 {
			t.Fatal("aborted queued input dispatched", calls)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range state.Entries {
			if legacyEntryValue(entry)["role"] == string(goai.RoleUser) {
				var message MessageReceipt
				if err := decodeEntryMessage(entry, h.session.limits, &message); err != nil {
					t.Fatal(err)
				}
				if message.ContentText != nil && (*message.ContentText == "never placed" || *message.ContentText == "queued") {
					t.Fatal("withdrawn submission placed", entry)
				}
				for _, block := range message.Content {
					if block.Text == "never placed" || block.Text == "queued" {
						t.Fatal("withdrawn submission placed", entry)
					}
				}
			}
		}
	})
}

// Source submissions.ts abort settles the queued record and removes only its
// inbox item. Unrelated canonical tasks remain untouched and are not dispatched.
func TestSubmissionAbortOwnsOnlyItsCanonicalRecordAndInboxItem(t *testing.T) {
	for _, kind := range []string{"input", "write"} {
		t.Run(kind, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				h := openHarness(t, b.store, Options{})
				c := root(t, h, ModelRef{})
				var target, other SubmissionRecord
				var generation ID
				trace := goai.TelemetryCarrier{"probe": "kept"}
				_, err := h.Commit(bg, func(tx *Tx) (any, error) {
					var err error
					target, err = tx.CreateSubmission(SubmissionCreate{Conversation: c.ID(), Type: kind, Status: "queued", RequestIDPresent: true, Trace: &trace})
					if err != nil {
						return nil, err
					}
					other, err = tx.CreateSubmission(SubmissionCreate{Conversation: c.ID(), Type: "input", Status: "queued"})
					if err != nil {
						return nil, err
					}
					generation, err = tx.CreateTask(GenerationTask(), JSON{}, TaskOptions{Conversation: c.ID(), Ownership: TaskOwnership{Kind: "conversation"}})
					if err != nil {
						return nil, err
					}
					inbox, err := builtin(tx, c.ID(), "pi.inbox")
					if err != nil {
						return nil, err
					}
					item := JSON{"id": target.ID, "mode": "followUp", "content": "target"}
					if kind == "write" {
						item = JSON{"id": target.ID, "mode": "write", "entry": JSON{"kind": "note"}}
					}
					return nil, inbox.Set(JSON{"items": []any{item, JSON{"id": other.ID, "mode": "followUp", "content": "other"}}})
				})
				if err != nil {
					t.Fatal(err)
				}
				before, found, err := h.Task(bg, generation)
				if err != nil || !found || h.scheduler.enabled.Load() {
					t.Fatal("canonical task must remain undispatched", before, err)
				}
				decision := mainAtomicSourceDecision(t, h, target.ID)
				if result, err := h.AbortSubmission(bg, target.ID, c.ID()); err != nil || result != "aborted" {
					t.Fatal(result, err)
				}
				publication := decision()
				submissions, documents := 0, 0
				for i := range publication.Changes.Len() {
					change, _ := publication.Changes.Index(i)
					value := immutableViewMember(t, change, "value")
					switch immutableViewMember(t, change, "type").String() {
					case "submission":
						submissions++
						want := JSON{"id": float64(target.ID), "conversationId": float64(c.ID()), "type": kind, "status": "unanswered", "reason": "aborted", "requestId": "", "trace": JSON{"probe": "kept"}}
						if !equalJSONValue(value.Copy(), want) {
							t.Fatal("exact canonical aborted record", value.Copy(), want)
						}
					case "document":
						documents++
						metadata := immutableViewMember(t, change, "record")
						if immutableViewMember(t, metadata, "kind").String() != "pi.inbox" || !equalJSONValue(value.Copy(), JSON{"items": []any{JSON{"id": float64(other.ID), "mode": "followUp", "content": "other"}}}) {
							t.Fatal("abort must remove only its inbox item", change.Copy())
						}
					default:
						t.Fatal("abort wrote unrelated state", change.Copy())
					}
				}
				if submissions != 1 || documents != 1 {
					t.Fatal("abort publication cardinality", submissions, documents)
				}
				after, found, err := h.Task(bg, generation)
				if err != nil || !found || !equalJSONValue(before, after) || h.scheduler.enabled.Load() {
					t.Fatal("abort changed or dispatched unrelated canonical task", after, err)
				}
				untouched, found, err := h.session.Submission(bg, other.ID)
				if err != nil || !found || !equalJSONValue(untouched, other) {
					t.Fatal("abort changed another queued record", untouched, err)
				}
				if result, err := h.AbortSubmission(bg, target.ID); err != nil || result != "settled" {
					t.Fatal("repeated abort disposition", result, err)
				}
			})
		})
	}
}

func TestSubmissionAbortRemovesOnlyFirstMatchingInboxItem(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h := openHarness(t, b.store, Options{})
		c := root(t, h, ModelRef{})
		var target, other SubmissionRecord
		_, err := h.Commit(bg, func(tx *Tx) (any, error) {
			var err error
			target, err = tx.CreateSubmission(SubmissionCreate{Conversation: c.ID(), Type: "input", Status: "queued"})
			if err != nil {
				return nil, err
			}
			other, err = tx.CreateSubmission(SubmissionCreate{Conversation: c.ID(), Type: "input", Status: "queued"})
			if err != nil {
				return nil, err
			}
			inbox, err := builtin(tx, c.ID(), "pi.inbox")
			if err != nil {
				return nil, err
			}
			return nil, inbox.Set(JSON{"items": []any{JSON{"id": target.ID, "mode": "followUp", "content": "first"}, JSON{"id": other.ID, "mode": "followUp", "content": "other"}, JSON{"id": target.ID, "mode": "followUp", "content": "duplicate"}}})
		})
		if err != nil {
			t.Fatal(err)
		}
		decision := mainAtomicSourceDecision(t, h, target.ID)
		if disposition, err := h.AbortSubmission(bg, target.ID); err != nil || disposition != "aborted" {
			t.Fatal(disposition, err)
		}
		publication := decision()
		found := false
		for i := range publication.Changes.Len() {
			change, _ := publication.Changes.Index(i)
			if immutableViewMember(t, change, "type").String() != "document" {
				continue
			}
			metadata := immutableViewMember(t, change, "record")
			if immutableViewMember(t, metadata, "kind").String() != "pi.inbox" {
				continue
			}
			found = true
			value := immutableViewMember(t, change, "value")
			want := JSON{"items": []any{JSON{"id": float64(other.ID), "mode": "followUp", "content": "other"}, JSON{"id": float64(target.ID), "mode": "followUp", "content": "duplicate"}}}
			if !equalJSONValue(value.Copy(), want) {
				t.Fatal("source findIndex/splice removes one position only", value.Copy(), want)
			}
			if !equalJSONValue(immutableViewMember(t, change, "ops").Copy(), []any{[]any{"p", []any{"items"}, 0, 1, []any{}}}) {
				t.Fatal("source positional inbox splice operation", change.Copy())
			}
		}
		if !found {
			t.Fatal("missing canonical inbox publication")
		}
	})
}
