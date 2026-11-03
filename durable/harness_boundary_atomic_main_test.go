package durable

import (
	"context"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Capture the real Session-local delivery, not a publication reconstructed from
// a later Snapshot. followSessionCommits currently rejects scheduler-backed
// sessions; this white-box, read-only tap instruments its actual delivery list
// without changing that API gate, scheduler state, or any durable fixture data.
func mainAtomicLocalDecision(t *testing.T, s *Session, id ID) func() sessionObservedCommit {
	t.Helper()
	frames, ready := make(chan sessionObservedCommit, 1), make(chan struct{}, 1)
	listener := &sessionCommitListener{callback: func(_ context.Context, event sessionObservedCommit) error {
		for _, write := range event.tables {
			if write.Submission != nil && write.Submission.ID == id && terminalStatus(write.Submission.Status) {
				select {
				case frames <- event:
					ready <- struct{}{}
				default:
				}
				break
			}
		}
		return nil
	}}
	s.observerMu.Lock()
	s.localCommitListeners = append(s.localCommitListeners, listener)
	s.observerMu.Unlock()
	t.Cleanup(func() {
		s.observerMu.Lock()
		defer s.observerMu.Unlock()
		for i, candidate := range s.localCommitListeners {
			if candidate == listener {
				copy(s.localCommitListeners[i:], s.localCommitListeners[i+1:])
				last := len(s.localCommitListeners) - 1
				s.localCommitListeners[last] = nil
				s.localCommitListeners = s.localCommitListeners[:last]
				break
			}
		}
	})
	return func() sessionObservedCommit {
		t.Helper()
		awaitTaskSignal(t, ready)
		return <-frames
	}
}

// Source-derived oracle, official v1.5.0 f9a26365 generation.ts
// answer/endFailedRun/failNoModel/startRun and inbox.ts applyBoundary. A later
// request-preparation commit must not repair an observable empty-run boundary:
// A's deciding batch already places writes before users and starts B. Numeric
// task IDs are the native run owner; checkpoint InputEntry still belongs to B.
func TestMainBoundaryAtomicSuccessorPublication(t *testing.T) {
	for _, scenario := range []struct{ ending, mode string }{
		{"answer", "all"}, {"answer", "one-at-a-time"},
		{"model_error", "one-at-a-time"}, {"no_model", "one-at-a-time"},
	} {
		t.Run(scenario.ending+"/"+scenario.mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, backend sqliteFixture) {
				aEntered, aRelease := make(chan struct{}), make(chan struct{})
				bEntered, bRelease := make(chan struct{}), make(chan struct{})
				requestB := make(chan []string, 1)
				var calls, resolutions atomic.Int64
				ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					ch := make(chan goai.Event, 1)
					n := calls.Add(1)
					isA := scenario.ending != "no_model" && n == 1
					go func() {
						defer close(ch)
						gate := bRelease
						if isA {
							gate = aRelease
							close(aEntered)
						} else if n == 2 || scenario.ending == "no_model" && n == 1 {
							var users []string
							for _, message := range input.Messages {
								if message.Role == goai.RoleUser {
									for _, block := range message.Content {
										users = append(users, block.Text)
									}
								}
							}
							requestB <- users
							close(bEntered)
						}
						select {
						case <-gate:
						case <-ctx.Done():
							return
						}
						if isA && scenario.ending == "model_error" {
							msg := terminal("failed").Message
							msg.StopReason, msg.ErrorMessage = goai.StopReasonError, "A literal failure"
							ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: msg}
						} else {
							ch <- terminal("A answer")
						}
					}()
					return ch
				})
				resolve := options.Models
				options.Models = func(provider goai.Provider, id string) *goai.Model {
					if resolutions.Add(1) == 1 && scenario.ending == "no_model" {
						close(aEntered)
						<-aRelease
						return nil
					}
					return resolve(provider, id)
				}
				options.Settings = &HarnessSettings{AfterFailedRun: "continue", FollowUpMode: scenario.mode}
				options.OnReport = func(err error) { t.Error("scheduler", err) }
				h := openHarness(t, backend.store, options)
				cleanupTaskGates(t, aRelease, bRelease)
				c := root(t, h, ref)
				a, err := c.Submit(bg, Input{Content: "A"})
				if err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, aEntered)
				var queued []*SubmissionHandle
				for _, text := range []string{"B", "C"} {
					sub, err := c.Submit(bg, Input{Type: "follow-up", Content: text})
					if err != nil {
						t.Fatal(err)
					}
					queued = append(queued, sub)
				}
				w, err := c.Submit(bg, Input{Type: "write", Entry: &Entry{Kind: "app.note", Value: JSON{"text": "W"}, Model: []MessageReceipt{userReceipt("W")}}})
				if err != nil {
					t.Fatal(err)
				}
				for _, sub := range []*SubmissionHandle{queued[0], queued[1], w} {
					status, err := sub.Status(bg)
					if err != nil || status.Status != "queued" || status.Entry != 0 || status.Answer != 0 {
						t.Fatal("B/C/W must be genuinely queued and unplaced while A is gated", status, err)
					}
				}
				storedDecision := mainPreparationTerminalPublication(t, h, a.ID())
				localDecision := mainAtomicLocalDecision(t, h.session, a.ID())
				releaseTaskGate(aRelease)
				ended := waitSubmission(t, a)
				publication, local := storedDecision(), localDecision()
				awaitTaskSignal(t, bEntered)
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				var aCP generationCheckpoint
				if err := fromObject(ended.Task.Checkpoint, &aCP, h.session.limits); err != nil {
					t.Fatal(err)
				}
				if ended.Task.ID == 0 || ended.Record.Entry == 0 || aCP.Submission != a.ID() || aCP.InputEntry != ended.Record.Entry || len(aCP.Steered) != 0 {
					t.Fatal("A must end alone at its own input, including no_model before request preparation", aCP, ended.Record)
				}
				if scenario.ending == "answer" {
					if ended.Record.Status != "done" || ended.Record.Answer == 0 {
						t.Fatal("A must answer", ended)
					}
				} else if ended.Record.Status != "unanswered" || ended.Record.Reason != scenario.ending || ended.Record.Answer != 0 || scenario.ending == "no_model" && ended.Record.Detail != nil {
					t.Fatal("A must fail without absorbing B or inventing an answer/detail", ended)
				}
				if scenario.ending == "model_error" && (ended.Record.Detail == nil || !ended.Record.Detail.Present || ended.Record.Detail.Value != "A literal failure") {
					t.Fatal("A settlement must retain its literal failure", ended.Record)
				}
				group := queued[:1]
				wantUsers := "A,W,B"
				if scenario.mode == "all" {
					group, wantUsers = queued, "A,W,B,C"
				}
				if got := strings.Join(<-requestB, ","); got != wantUsers {
					t.Errorf("actual successor request users = %q; want %q", got, wantUsers)
				}
				wStatus := publicSubmission(state.Submissions[w.ID()])
				bStatus := publicSubmission(state.Submissions[queued[0].ID()])
				if wStatus.Status != "done" || wStatus.Entry == 0 || bStatus.Status != "placed" || bStatus.Entry == 0 || wStatus.Entry == bStatus.Entry {
					t.Fatal("actual successor must own B's input, not passive W", wStatus, bStatus)
				}
				// Check each REAL delivery independently. Do not combine commits or
				// use the later state above as evidence of atomic publication.
				requireBatch := func(lane string, seq uint64, tables []Write, documents []DocumentChange) {
					t.Helper()
					subs, tasks := map[ID]Submission{}, map[ID]Task{}
					var entries []ID
					var live, inbox JSON
					for _, write := range tables {
						if write.Submission != nil {
							subs[write.Submission.ID] = *write.Submission
						}
						if write.Task != nil {
							tasks[write.Task.ID] = *write.Task
						}
						if write.Op == "append-entry" && write.Entry != nil && write.Entry.Conversation == c.ID() {
							entries = append(entries, write.Entry.ID)
							if write.Entry.Seq != seq || !equalJSONValue(write.Entry.Value, state.Entries[write.Entry.ID].Value) {
								t.Errorf("%s: appended entry has wrong adopted sequence: %+v", lane, write.Entry)
							}
						}
					}
					for _, change := range documents {
						if change.Record.Owner == c.ID() && !change.Record.Retired {
							switch change.Record.Kind {
							case "pi.live":
								live = change.Value
							case "pi.inbox":
								inbox = change.Value
							}
						}
					}
					if got := publicSubmission(subs[a.ID()]); got.Status != ended.Record.Status || got.Entry != ended.Record.Entry || got.Answer != ended.Record.Answer || got.Reason != ended.Record.Reason || !equalJSONValue(subs[a.ID()].Value, ended.Submission.Value) {
						t.Errorf("%s: batch must settle A exactly: %+v; want %+v", lane, got, ended.Record)
					}
					wantEntries := []ID{}
					if scenario.ending == "answer" {
						wantEntries = append(wantEntries, ended.Record.Answer)
					} else {
						// A model error may persist its real assistant error receipt;
						// no_model must never synthesize one.
						for _, id := range entries {
							if state.Entries[id].ByTask == ended.Task.ID {
								if scenario.ending == "no_model" {
									t.Errorf("%s: no_model invented an A transcript entry: %+v", lane, state.Entries[id])
								}
								wantEntries = append(wantEntries, id)
							}
						}
					}
					wantEntries = append(wantEntries, wStatus.Entry)
					inputs := []any{}
					for _, sub := range group {
						status := publicSubmission(state.Submissions[sub.ID()])
						wantEntries = append(wantEntries, status.Entry)
						inputs = append(inputs, sub.ID())
						if placed := publicSubmission(subs[sub.ID()]); placed.Status != "placed" || placed.Entry != status.Entry || placed.Answer != 0 {
							t.Errorf("%s: A-end seq=%d lacks same-batch placement of %d (later entry seq=%d): %+v", lane, seq, sub.ID(), state.Entries[status.Entry].Seq, placed)
						}
					}
					if !reflect.DeepEqual(entries, wantEntries) {
						t.Errorf("%s: A-end must append answer/error then W then selected users: got %v; want %v", lane, entries, wantEntries)
					}
					if got := publicSubmission(subs[w.ID()]); got.Status != "done" || got.Entry != wStatus.Entry || got.Answer != 0 {
						t.Errorf("%s: W must settle at its own entry in A-end: %+v", lane, got)
					}
					run := generationObject(live["run"])
					owner := submissionEntryID(run["task"])
					wantRun, err := dtoObject(JSON{"task": owner, "inputs": inputs, "first": wStatus.Entry}, h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					if owner == 0 || owner == ended.Task.ID || !equalJSONValue(run, wantRun) {
						t.Errorf("%s: A-end must already start B with first=W and only selected user inputs: got %v; want %v", lane, run, wantRun)
					} else {
						var cp generationCheckpoint
						task := tasks[owner]
						if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
							t.Fatal(err)
						}
						if task.Kind != "pi.generation" || task.Conversation != c.ID() || terminalStatus(task.Status) || cp.Submission != queued[0].ID() || cp.InputEntry != bStatus.Entry || len(cp.Steered) != len(group)-1 || len(group) == 2 && cp.Steered[0] != queued[1].ID() {
							t.Errorf("%s: same-batch B checkpoint must own B's entry, not W: %+v, %+v", lane, task, cp)
						}
					}
					var wantFailure any
					if scenario.ending != "answer" {
						detail := "A literal failure"
						if scenario.ending == "no_model" {
							detail = "Model openai/durable-test is not available"
						}
						wire, err := dtoObject(JSON{"failure": JSON{"taskId": ended.Task.ID, "inputs": []any{a.ID()}, "first": ended.Record.Entry, "reason": scenario.ending, "detail": detail}}, h.session.limits)
						if err != nil {
							t.Fatal(err)
						}
						wantFailure = wire["failure"]
					}
					if !equalJSONValue(live["runFailure"], wantFailure) || live["failedRun"] != nil {
						t.Errorf("%s: automatic successor must retain exactly A's failure without holding queue: %v", lane, live)
					}
					items, ok := inbox["items"].([]any)
					if !ok || len(items) != len(queued)-len(group) || len(items) == 1 && submissionEntryID(generationObject(items[0])["id"]) != queued[1].ID() {
						t.Errorf("%s: boundary must preserve all/one inbox selection: %v", lane, inbox)
					}
					if len(group) == 1 {
						if _, touched := subs[queued[1].ID()]; touched || publicSubmission(state.Submissions[queued[1].ID()]).Entry != 0 {
							t.Errorf("%s: one-at-a-time must leave C queued/unplaced", lane)
						}
					}
				}
				requireBatch("stored", publication.Seq, publication.Tables, publication.Documents)
				if local.kind != "stored" || local.seq == nil || *local.seq != publication.Seq {
					t.Fatal("local decision must be the same actual stored adoption", local.kind, local.seq, publication.Seq)
				}
				requireBatch("local", *local.seq, local.tables, local.documents)
				wantCalls := int64(2)
				if scenario.ending == "no_model" {
					wantCalls = 1
				}
				if calls.Load() != wantCalls {
					t.Error("only the expected A/B provider dispatches are allowed", calls.Load(), wantCalls)
				}
				// Cancel the gated successor rather than letting cleanup answer B/C.
				if err := h.Close(bg); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}
