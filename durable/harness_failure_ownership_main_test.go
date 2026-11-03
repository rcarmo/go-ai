package durable

import (
	"errors"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Native, source-derived ownership controls, not translated upstream test
// bodies. Like the progress fixtures, these admit real tasks/documents through
// Session commits without enabling scheduling. The competing run is an explicit
// adversarial state, not a claim that normal Submit produces this interleaving.
type mainFailureOwnershipFixture struct {
	h       *Harness
	current Task
	foreign Task
	cp      generationCheckpoint
	live    ID
}

func mainFailureOwnershipSeed(t *testing.T, b sqliteFixture, handoff, placed bool, owner string) mainFailureOwnershipFixture {
	t.Helper()
	h := openHarness(t, b.store, Options{Settings: &HarnessSettings{AfterFailedRun: "keep"}})
	c := root(t, h, ModelRef{})
	var tasks [2]ID
	var checkpoints [2]generationCheckpoint
	for i := range tasks {
		_, err := h.session.Commit(bg, func(tx *Tx) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			tasks[i] = id
			sub, err := tx.MintID()
			if err != nil {
				return err
			}
			cp := generationCheckpoint{Phase: "queued", Submission: sub, Input: "current input"}
			if i == 1 {
				cp.Phase, cp.Input, cp.Attempt = "intent", "foreign input", 1
				cp.Partial = &MessageReceipt{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "foreign partial"}}}
			}
			value := JSON{"content": cp.Input}
			if placed || i == 1 || handoff {
				entry, err := tx.MintID()
				if err != nil {
					return err
				}
				receipt, err := inputReceipt(cp.Input, nil, tx.limits)
				if err != nil {
					return err
				}
				wire, err := dtoObject(receipt, tx.limits)
				if err != nil {
					return err
				}
				if err := tx.AppendEntry(Entry{ID: entry, Conversation: c.ID(), Kind: "message", ByTask: id, Value: wire}); err != nil {
					return err
				}
				cp.InputEntry, value["placedEntry"] = entry, entry
			}
			if handoff && i == 0 {
				cp.Phase = "tools"
				cp.AssistantEntry, err = tx.MintID()
				if err != nil {
					return err
				}
				// An actual stored, correctly attributed tool-calling assistant
				// satisfies the method's receipt prerequisite; no tool is run.
				wire, err := dtoObject(MessageReceipt{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse,
					Content: []goai.ContentBlock{{Type: "toolCall", ID: "ownership-call", Name: "ownership", Arguments: JSON{}}}}, tx.limits)
				if err != nil {
					return err
				}
				if err := tx.AppendEntry(Entry{ID: cp.AssistantEntry, Conversation: c.ID(), Kind: "message", ByTask: id, Value: wire}); err != nil {
					return err
				}
			}
			if err := tx.PutSubmission(Submission{ID: sub, Conversation: c.ID(), Type: "follow-up", Status: "pending", Value: value}); err != nil {
				return err
			}
			wire, err := dtoObject(cp, tx.limits)
			if err != nil {
				return err
			}
			checkpoints[i] = cp
			return tx.PutTask(Task{ID: id, Conversation: c.ID(), Kind: "pi.generation", Status: "pending", Checkpoint: wire})
		})
		if err != nil {
			t.Fatal("admit ownership fixture task", err)
		}
	}
	var liveID ID
	_, err := h.session.Commit(bg, func(tx *Tx) error {
		live, err := builtin(tx, c.ID(), "pi.live")
		if err != nil {
			return err
		}
		liveID = live.id
		return live.Update(func(value JSON) error {
			if owner == "absent" {
				return nil
			}
			i := 0
			if owner == "foreign" {
				i = 1
			}
			cp := checkpoints[i]
			if err := startLiveRun(tx, value, tx.state.Tasks[tasks[i]], cp, []any{cp.Submission}); err != nil {
				return err
			}
			if i == 1 {
				message, err := dtoObject(*cp.Partial, tx.limits)
				if err != nil {
					return err
				}
				value["generation"] = JSON{"attempt": cp.Attempt, "message": message}
			}
			return nil
		})
	})
	if err != nil {
		t.Fatal("admit ownership fixture live run", err)
	}
	state, err := h.session.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	if h.scheduler.enabled.Load() || len(state.Tasks) != 2 || state.Tasks[tasks[0]].ID == 0 || state.Tasks[tasks[1]].ID == 0 {
		t.Fatal("fixture did not retain two unscheduled existing tasks")
	}
	return mainFailureOwnershipFixture{h: h, current: state.Tasks[tasks[0]], foreign: state.Tasks[tasks[1]], cp: checkpoints[0], live: liveID}
}

func mainFailureOwnershipUnchanged(t *testing.T, f mainFailureOwnershipFixture, b sqliteFixture, before Snapshot, err error) {
	t.Helper()
	var rejected *StorageRejected
	if !errors.As(err, &rejected) || rejected.Reason != "generation run ownership changed" {
		t.Fatalf("want ownership StorageRejected from actual method, got %v", err)
	}
	for _, source := range []struct {
		name string
		read func() (Snapshot, error)
	}{
		{"session", func() (Snapshot, error) { return f.h.session.Snapshot(bg) }},
		{"storage", func() (Snapshot, error) { return b.store.Snapshot(bg) }},
	} {
		after, err := source.read()
		if err != nil {
			t.Fatal(source.name, err)
		}
		// MintID reservations are deliberately not transactional. Compare every
		// adopted table, revision and sequence, but allow that allocator fence.
		if after.HighWater < before.HighWater {
			t.Fatal(source.name, "allocator ownership moved backwards")
		}
		after.HighWater = before.HighWater
		if !equalSnapshot(before, after) {
			t.Fatalf("%s adopted partial writes: seq %d -> %d; tasks %d -> %d; entries %d -> %d",
				source.name, before.Seq, after.Seq, len(before.Tasks), len(after.Tasks), len(before.Entries), len(after.Entries))
		}
		if submissionEntryID(generationObject(after.Documents[f.live].Value["run"])["task"]) != f.foreign.ID {
			t.Fatal(source.name, "foreign live.run lost ownership")
		}
	}
}

func TestMainRunFailureNoModelOwnershipFence(t *testing.T) {
	for _, mode := range []string{"foreign-unplaced", "foreign-placed", "owned-placed", "absent-unplaced"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				owner, placed := "foreign", mode == "foreign-placed" || mode == "owned-placed"
				if mode == "owned-placed" {
					owner = "owned"
				} else if mode == "absent-unplaced" {
					owner = "absent"
				}
				f := mainFailureOwnershipSeed(t, b, false, placed, owner)
				before := snap(t, b.store)
				err := f.h.finishGenerationNoModel(f.current, f.cp, &generationNoModel{})
				if owner == "foreign" {
					mainFailureOwnershipUnchanged(t, f, b, before, err)
					return
				}
				if err != nil {
					t.Fatal("positive no-model control", err)
				}
				after := snap(t, b.store)
				sub := publicSubmission(after.Submissions[f.cp.Submission])
				failure := generationObject(after.Documents[f.live].Value["runFailure"])
				if after.Seq != before.Seq+1 || after.Tasks[f.current.ID].Status != "failed" || sub.Status != "unanswered" || sub.Reason != "no_model" || sub.Entry == 0 || submissionEntryID(failure["taskId"]) != f.current.ID {
					t.Fatal("positive no-model control failed to adopt its own run", sub, failure)
				}
				if !equalJSONValue(before.Tasks[f.foreign.ID], after.Tasks[f.foreign.ID]) {
					t.Fatal("positive no-model control changed foreign task")
				}
			})
		})
	}
}

func TestMainRunFailureToolHandoffOwnershipFence(t *testing.T) {
	for _, owner := range []string{"foreign", "owned"} {
		t.Run(owner, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				f := mainFailureOwnershipSeed(t, b, true, true, owner)
				before := snap(t, b.store)
				var successor Task
				var next generationCheckpoint
				var stagedSuccessor, stagedCompletion bool
				_, err := f.h.session.invocationCommit(bg, f.current.ID, func(tx *Tx) error {
					current := tx.state.Tasks[f.current.ID]
					var cp generationCheckpoint
					if err := fromObject(current.Checkpoint, &cp, tx.limits); err != nil {
						return err
					}
					var err error
					successor, next, err = f.h.handoffToolGeneration(tx, current, cp)
					for _, write := range tx.writes {
						if write.Task != nil {
							stagedSuccessor = stagedSuccessor || write.Task.ID != current.ID && write.Task.Kind == "pi.generation"
							stagedCompletion = stagedCompletion || write.Task.ID == current.ID && write.Task.Status == "done"
						}
					}
					return err
				})
				if !stagedSuccessor || !stagedCompletion {
					t.Fatal("actual handoff did not reach late live.run fence after staging both task writes", err)
				}
				if owner == "foreign" {
					mainFailureOwnershipUnchanged(t, f, b, before, err)
					if successor.ID != 0 || next.Phase != "" {
						t.Fatal("rejected handoff returned an adoptable successor", successor, next)
					}
					return
				}
				if err != nil {
					t.Fatal("positive handoff control", err)
				}
				after := snap(t, b.store)
				run := generationObject(after.Documents[f.live].Value["run"])
				inputs, ok := run["inputs"].([]any)
				if !ok || len(inputs) != 1 || submissionEntryID(inputs[0]) != f.cp.Submission {
					t.Fatal("positive handoff lost its input ownership", run)
				}
				if after.Seq != before.Seq+1 || len(after.Tasks) != len(before.Tasks)+1 || after.Tasks[f.current.ID].Status != "done" || successor.ID == 0 || after.Tasks[successor.ID].Owner != 0 || next.Phase != "prepare-next" || submissionEntryID(run["task"]) != successor.ID || submissionEntryID(run["first"]) != f.cp.InputEntry {
					t.Fatal("positive handoff control failed atomic successor transfer", successor, next, run)
				}
				if len(after.Entries) != len(before.Entries) || !equalJSONValue(before.Tasks[f.foreign.ID], after.Tasks[f.foreign.ID]) || !equalJSONValue(before.Submissions, after.Submissions) {
					t.Fatal("positive handoff duplicated receipts, settled inputs or changed foreign task")
				}
			})
		})
	}
}
