package durable

import (
	"context"
	"fmt"
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

// Capture the canonical public record directly. Native stored/local taps below
// remain supplemental publication-lane controls, not source wire conversions.
func mainAtomicSourceDecision(t *testing.T, h *Harness, id ID) func() CommitPublication {
	t.Helper()
	frames := make(chan CommitPublication, 1)
	guard, err := h.ObserveCommits(CommitObserver{Advance: func(_ context.Context, publication CommitPublication) error {
		for i := range publication.Changes.Len() {
			change, _ := publication.Changes.Index(i)
			if immutableViewMember(t, change, "type").String() != "submission" {
				continue
			}
			value := immutableViewMember(t, change, "value")
			if submissionEntryID(immutableViewMember(t, value, "id").Number()) != id {
				continue
			}
			status := immutableViewMember(t, value, "status").String()
			if status == "done" || status == "unanswered" {
				select {
				case frames <- publication:
				default:
				}
			}
		}
		return nil
	}, End: func(WatchEnd) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(guard.Detach)
	return func() CommitPublication { return watchReceive(t, frames) }
}

// Source-derived oracle, official v1.5.0 f9a26365 generation.ts
// answer/endFailedRun/failNoModel/startRun and inbox.ts applyBoundary. A later
// request-preparation commit must not repair an observable empty-run boundary:
// A's deciding batch already places writes before users and starts B. Numeric
// task IDs are the run owners; placed submission entries identify B, not W.
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
				w, err := c.Submit(bg, Input{Type: "write", Entry: &Entry{Kind: "app.note", HasData: true, Data: JSON{"text": "W"}, Model: []MessageReceipt{userReceipt("W")}}})
				if err != nil {
					t.Fatal(err)
				}
				for _, sub := range []*SubmissionHandle{queued[0], queued[1], w} {
					status, err := sub.Status(bg)
					if err != nil || status.Status != "queued" || status.Entry != 0 || status.Answer != 0 {
						t.Fatal("B/C/W must be genuinely queued and unplaced while A is gated", status, err)
					}
				}
				before, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				liveBefore := mainEligibilityDoc(t, before, c.ID(), "pi.live").Value
				aRun := generationObject(liveBefore["run"])
				aTaskID := submissionEntryID(aRun["taskId"])
				aStatus, err := a.Status(bg)
				if err != nil || aTaskID == 0 || aStatus.Status != "placed" || aStatus.Entry == 0 || !equalJSONValue(aRun, JSON{"taskId": float64(aTaskID), "inputs": []any{float64(a.ID())}, "first": float64(aStatus.Entry), "turns": float64(0)}) {
					t.Fatal("A must own only its original placed input before the decision", aRun, aStatus, err)
				}
				sourceDecision := mainAtomicSourceDecision(t, h, a.ID())
				storedDecision := mainPreparationTerminalPublication(t, h, a.ID())
				localDecision := mainAtomicLocalDecision(t, h.session, a.ID())
				releaseTaskGate(aRelease)
				ended := waitSubmission(t, a)
				publication, local, source := storedDecision(), localDecision(), sourceDecision()
				awaitTaskSignal(t, bEntered)
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				aTask, found, err := h.Task(bg, aTaskID)
				if err != nil || !found || aTask.Kind != "pi.generation" || aTask.Version != 2 || aTask.State.Status != "terminal" || aTask.State.Checkpoint != nil || aTask.State.Outcome == nil || aTask.Input == nil || !aTask.Input.Present || !equalJSONValue(aTask.Input.Value, JSON{}) || ended.Record.Entry != aStatus.Entry {
					t.Fatal("A terminal canonical task and original placement", aTask, ended.Record, err)
				}
				if source.Seq != publication.Seq {
					t.Fatal("public observer must expose the same deciding adoption", source.Seq, publication.Seq)
				}
				sourceTasks, sourceSubs := map[ID]ReadonlyValue{}, map[ID]ReadonlyValue{}
				var sourceEntries []ID
				sourceDocs := map[string]ReadonlyValue{}
				for i := range source.Changes.Len() {
					change, _ := source.Changes.Index(i)
					value := immutableViewMember(t, change, "value")
					switch immutableViewMember(t, change, "type").String() {
					case "task":
						sourceTasks[submissionEntryID(immutableViewMember(t, value, "id").Number())] = value
					case "submission":
						sourceSubs[submissionEntryID(immutableViewMember(t, value, "id").Number())] = value
					case "entry":
						sourceEntries = append(sourceEntries, submissionEntryID(immutableViewMember(t, value, "id").Number()))
						if _, present := value.Get("seq"); present {
							t.Error("source entry must not expose a private sequence field", value)
						}
					case "document":
						metadata := immutableViewMember(t, change, "record")
						sourceDocs[immutableViewMember(t, metadata, "kind").String()] = value
					}
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
				wantA := JSON{"id": float64(a.ID()), "conversationId": float64(c.ID()), "type": "input", "status": ended.Record.Status, "entry": float64(aStatus.Entry)}
				var outcome JSON
				if scenario.ending == "answer" {
					wantA["answer"] = float64(ended.Record.Answer)
					outcome = JSON{"status": "completed", "result": JSON{"entryId": float64(ended.Record.Answer)}}
				} else {
					wantA["reason"] = scenario.ending
					detail := "A literal failure"
					if scenario.ending == "no_model" {
						detail = "Model openai/durable-test is not available"
					} else {
						wantA["detail"] = detail
					}
					outcome = JSON{"status": "failed", "error": JSON{"message": detail, "detail": JSON{"reason": scenario.ending}}}
				}
				if sourceSubs[a.ID()] == nil || !equalJSONValue(sourceSubs[a.ID()].Copy(), wantA) || sourceTasks[aTaskID] == nil || !equalJSONValue(immutableViewMember(t, sourceTasks[aTaskID], "state").Copy(), JSON{"status": "terminal", "outcome": outcome}) {
					t.Fatal("exact source deciding settlement and task outcome", sourceSubs[a.ID()], sourceTasks[aTaskID], wantA, outcome)
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
					subs := map[ID]Submission{}
					var entries []ID
					var live, inbox JSON
					for _, write := range tables {
						if write.Submission != nil {
							subs[write.Submission.ID] = *write.Submission
						}
						if write.Op == "append-entry" && write.Entry != nil && write.Entry.Conversation == c.ID() {
							entries = append(entries, write.Entry.ID)
							if write.Entry.Seq != seq || !equalJSONValue(messageEntryValue(*write.Entry), messageEntryValue(state.Entries[write.Entry.ID])) {
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
							if state.Entries[id].Kind == "pi.assistant" {
								if state.Entries[id].ByTask != aTaskID {
									t.Errorf("%s: assistant receipt must belong to A", lane)
								}
								if scenario.ending == "no_model" {
									t.Errorf("%s: no_model invented an A transcript entry: %+v", lane, state.Entries[id])
								}
								wantEntries = append(wantEntries, id)
							}
						}
					}
					if len(sourceTasks) != 2 || len(sourceSubs) != 2+len(group) {
						t.Errorf("canonical deciding task/submission cardinality: %d/%d", len(sourceTasks), len(sourceSubs))
					}
					wantW := JSON{"id": float64(w.ID()), "conversationId": float64(c.ID()), "type": "write", "status": "done", "entry": float64(wStatus.Entry)}
					if sourceSubs[w.ID()] == nil || !equalJSONValue(sourceSubs[w.ID()].Copy(), wantW) {
						t.Error("exact canonical passive-write settlement", sourceSubs[w.ID()])
					}
					wantEntries = append(wantEntries, wStatus.Entry)
					inputs := []any{}
					for _, sub := range group {
						status := publicSubmission(state.Submissions[sub.ID()])
						wantEntries = append(wantEntries, status.Entry)
						inputs = append(inputs, sub.ID())
						wantB := JSON{"id": float64(sub.ID()), "conversationId": float64(c.ID()), "type": "input", "status": "placed", "entry": float64(status.Entry)}
						if sourceSubs[sub.ID()] == nil || !equalJSONValue(sourceSubs[sub.ID()].Copy(), wantB) {
							t.Error("exact same-batch canonical user placement", sourceSubs[sub.ID()], wantB)
						}
						if placed := publicSubmission(subs[sub.ID()]); placed.Status != "placed" || placed.Entry != status.Entry || placed.Answer != 0 {
							t.Errorf("%s: A-end seq=%d lacks same-batch placement of %d (later entry seq=%d): %+v", lane, seq, sub.ID(), state.Entries[status.Entry].Seq, placed)
						}
					}
					if !reflect.DeepEqual(entries, wantEntries) || !reflect.DeepEqual(sourceEntries, wantEntries) {
						t.Errorf("%s: A-end must append answer/error then W then selected users: got %v; want %v", lane, entries, wantEntries)
					}
					if got := publicSubmission(subs[w.ID()]); got.Status != "done" || got.Entry != wStatus.Entry || got.Answer != 0 {
						t.Errorf("%s: W must settle at its own entry in A-end: %+v", lane, got)
					}
					run := generationObject(live["run"])
					owner := submissionEntryID(run["taskId"])
					wantRun, err := dtoObject(JSON{"taskId": owner, "inputs": inputs, "first": wStatus.Entry, "turns": 0}, h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					if owner == 0 || owner == aTaskID || !equalJSONValue(run, wantRun) {
						t.Errorf("%s: A-end must already start B with first=W and only selected user inputs: got %v; want %v", lane, run, wantRun)
					} else {
						// Read the exact public publication record, not a normalized
						// private task checkpoint or a later request-preparation image.
						task := sourceTasks[owner]
						if task == nil || immutableViewMember(t, task, "kind").String() != "pi.generation" || immutableViewMember(t, task, "version").Number().String() != "2" || immutableViewMember(t, task, "conversationId").Number().String() != fmt.Sprint(c.ID()) || !equalJSONValue(immutableViewMember(t, task, "input").Copy(), JSON{}) || !equalJSONValue(immutableViewMember(t, task, "state").Copy(), JSON{"status": "pending", "checkpoint": JSON{"phase": "prepare", "attempt": 1}}) {
							t.Errorf("%s: same-batch B must be a fresh source generation: %v", lane, task)
						}
					}
					var wantFailure any
					if scenario.ending != "answer" {
						detail := "A literal failure"
						if scenario.ending == "no_model" {
							detail = "Model openai/durable-test is not available"
						}
						wire, err := dtoObject(JSON{"failure": JSON{"taskId": aTaskID, "inputs": []any{a.ID()}, "first": ended.Record.Entry, "reason": scenario.ending, "detail": detail}}, h.session.limits)
						if err != nil {
							t.Fatal(err)
						}
						wantFailure = wire["failure"]
					}
					if !equalJSONValue(live["runFailure"], wantFailure) || live["failedRun"] != nil {
						t.Errorf("%s: automatic successor must retain exactly A's failure without holding queue: %v", lane, live)
					}
					if sourceDocs["pi.live"] == nil || !equalJSONValue(sourceDocs["pi.live"].Copy(), live) || sourceDocs["pi.inbox"] == nil || !equalJSONValue(sourceDocs["pi.inbox"].Copy(), inbox) {
						t.Error("canonical deciding document values differ from actual adoption", sourceDocs)
					}
					items, ok := inbox["items"].([]any)
					if !ok || len(items) != len(queued)-len(group) || len(items) == 1 && submissionEntryID(generationObject(items[0])["id"]) != queued[1].ID() {
						t.Errorf("%s: boundary must preserve all/one inbox selection: %v", lane, inbox)
					}
					if len(group) == 1 {
						if _, touched := subs[queued[1].ID()]; touched || sourceSubs[queued[1].ID()] != nil || publicSubmission(state.Submissions[queued[1].ID()]).Entry != 0 {
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

// Source generation.ts reads queue modes inside the boundary commit, after
// awaited onYield/afterTools hooks. This is narrower than the source inbox test
// which additionally occupies the Session line with a separate waiting commit.
func TestMainBoundaryReadsLiveQueueModesAfterHooks(t *testing.T) {
	for _, boundary := range []string{"final", "postTools"} {
		for _, mode := range []string{"all", "one-at-a-time"} {
			t.Run(boundary+"/"+mode, func(t *testing.T) {
				sqliteStore(t, func(t *testing.T, b sqliteFixture) {
					initial := "all"
					if mode == "all" {
						initial = "one-at-a-time"
					}
					entered, release, secondEntered, secondRelease := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
					var hooks, requests atomic.Int64
					gate := func(ctx context.Context) error {
						if hooks.Add(1) == 1 {
							close(entered)
							select {
							case <-release:
							case <-ctx.Done():
								return ctx.Err()
							}
						}
						return nil
					}
					registry := NewRegistry()
					extension := &Extension{Name: "queue-boundary-gate"}
					if boundary == "final" {
						extension.Hooks.OnYield = func(ctx context.Context, _ MessageReceipt, _ *HookAPI) (*Input, error) { return nil, gate(ctx) }
					} else {
						extension.Hooks.AfterTools = func(ctx context.Context, _ []MessageReceipt, _ *HookAPI) error { return gate(ctx) }
						reg := wrapRegistration("noop")
						reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) { return ToolResult{Content: "ok"}, nil }
						extension.Tools = []ToolRegistration{reg}
					}
					if err := registry.Install(extension); err != nil {
						t.Fatal(err)
					}
					users := make(chan []string, 1)
					ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
						ch := make(chan goai.Event, 1)
						if requests.Add(1) == 1 {
							if boundary == "final" {
								ch <- terminal("A answer")
							} else {
								ch <- toolAnswer("noop-call", "noop", JSON{})
							}
							close(ch)
						} else {
							var texts []string
							for _, message := range input.Messages {
								if message.Role == goai.RoleUser {
									for _, block := range message.Content {
										texts = append(texts, block.Text)
									}
								}
							}
							users <- texts
							close(secondEntered)
							go func() {
								defer close(ch)
								select {
								case <-secondRelease:
									ch <- terminal("B answer")
								case <-ctx.Done():
								}
							}()
						}
						return ch
					})
					options.Registry = registry
					options.Settings = &HarnessSettings{FollowUpMode: initial, SteeringMode: initial}
					h := openHarness(t, b.store, options)
					cleanupTaskGates(t, release, secondRelease)
					c := root(t, h, ref)
					if _, err := c.Submit(bg, Input{Content: "A"}); err != nil {
						t.Fatal(err)
					}
					awaitTaskSignal(t, entered)
					var queued []*SubmissionHandle
					for _, text := range []string{"B", "C"} {
						input := Input{Content: text}
						if boundary == "postTools" {
							input.WhenBusy = "steer"
						}
						sub, err := c.Submit(bg, input)
						if err != nil {
							t.Fatal(err)
						}
						statusFailed140(t, sub, "queued", "")
						queued = append(queued, sub)
					}
					if err := h.SetSettings(bg, HarnessSettings{FollowUpMode: mode, SteeringMode: mode}); err != nil {
						t.Fatal(err)
					}
					releaseTaskGate(release)
					awaitTaskSignal(t, secondEntered)
					want := "A,B"
					cStatus := "queued"
					if mode == "all" {
						want, cStatus = "A,B,C", "placed"
					}
					if got := strings.Join(watchReceive(t, users), ","); got != want {
						t.Error("boundary used pre-hook queue mode", got, want)
					}
					statusFailed140(t, queued[0], "placed", "")
					statusFailed140(t, queued[1], cStatus, "")
					if requests.Load() != 2 {
						t.Error("unexpected provider dispatch", requests.Load())
					}
					if err := h.Close(bg); err != nil {
						t.Fatal(err)
					}
				})
			})
		}
	}
}
