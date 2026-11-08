package durable

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Native source-derived invariants, NOT full upstream test-body ports. Official
// v1.5.0 f9a26365568127461c47a07a37dcb42c140a3ac5 inbox.ts applyBoundary
// supplies the queue-mode oracle; inherited cancellation is a native ownership
// fence for generation-task placeholders, which upstream does not represent.
//
// Conversations/tasks are created through plain Session APIs, then real Open
// and Submit admission creates every input, checkpoint, inbox slot and run.
// Scheduling is sealed BEFORE admission, with no invocations ever dispatched.
// We explicitly clear A's real run (not simulate its completion), optionally
// mark its conversation's parent, and inspect rejected boundary candidates.
// This does not cover automatic abort withdrawal/cascade, model execution,
// settlement, or scheduler-driven boundary timing/publication.
func mainEligibilityHarness(t *testing.T, b sqliteFixture, owned bool, settings *HarnessSettings) (*Harness, *ConversationHandle, ID) {
	t.Helper()
	seedRootConversation(t, b.store)
	plainSession, err := OpenSession(b.store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plainSession.Close(bg) })
	registry := NewRegistry()
	definition := taskDefinition(t, "task.boundary.eligibility.parent", func(context.Context, TaskRecord, *TaskRuntime) error {
		return errors.New("eligibility fixture must not dispatch tasks")
	})
	if _, err := registry.RegisterTask(definition); err != nil {
		t.Fatal(err)
	}
	var conversation, parent ID
	if _, err := plainSession.Commit(bg, func(tx *Tx) error {
		if owned {
			ownerConversation, err := tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.CreateConversation(Conversation{ID: ownerConversation}); err != nil {
				return err
			}
			parent, err = tx.CreateTask(definition, nil, TaskOptions{Conversation: ownerConversation, Ownership: TaskOwnership{Kind: "conversation"}})
			if err != nil {
				return err
			}
		}
		var err error
		conversation, err = tx.MintID()
		if err != nil {
			return err
		}
		return tx.CreateConversation(Conversation{ID: conversation, Owner: parent})
	}); err != nil {
		t.Fatal(err)
	}
	if err := plainSession.Close(bg); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, utf16ReopenStore(t, b.store), Options{Registry: registry, Settings: settings, OnReport: func(err error) { t.Error("scheduler", err) }})
	// No task can run before the first Submit enables scheduling. Sealing here
	// leaves the real admission and boundary implementations intact, but prevents
	// reconcile from withdrawing B before we can probe either selector directly.
	h.scheduler.sealed.Store(true)
	c, err := h.Conversation(bg, conversation)
	if err != nil {
		t.Fatal(err)
	}
	state, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	if state.Conversations[conversation].Parent != 0 || state.Conversations[conversation].Owner != parent {
		t.Fatal("expected actual plain-Session root/owned conversation", state.Conversations[conversation])
	}
	if _, exists := agentDocument(state, conversation); exists {
		t.Fatal("plain-Session conversation must have no pi.agent")
	}
	return h, c, parent
}

func mainEligibilityDoc(t *testing.T, state Snapshot, conversation ID, kind string) Document {
	t.Helper()
	var found Document
	count := 0
	for _, doc := range state.Documents {
		if !doc.Retired && doc.Scope == "conversation" && doc.Owner == conversation && doc.Kind == kind {
			found, count = doc, count+1
		}
	}
	if count != 1 {
		t.Fatalf("expected one actual %s document, got %d", kind, count)
	}
	return found
}

func mainEligibilityTask(t *testing.T, state Snapshot, sub ID, limits Limits) (Task, generationCheckpoint) {
	t.Helper()
	var found Task
	var checkpoint generationCheckpoint
	count := 0
	for _, task := range state.Tasks {
		if task.Kind != "pi.generation" {
			continue
		}
		var cp generationCheckpoint
		if err := fromObject(task.Checkpoint, &cp, limits); err != nil {
			t.Fatal(err)
		}
		if cp.Submission == sub {
			found, checkpoint, count = task, cp, count+1
		}
	}
	if count != 1 {
		t.Fatalf("expected one real generation for %d, got %d", sub, count)
	}
	return found, checkpoint
}

func mainEligibilityProbe(t *testing.T, h *Harness, probe func(*Tx, Snapshot) error) {
	t.Helper()
	before, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	refusal := errors.New("discard inspected eligibility candidate")
	visited := false
	seq, err := h.session.Commit(bg, func(tx *Tx) error {
		if len(h.scheduler.invocations) != 0 || !h.scheduler.sealed.Load() {
			t.Fatal("native invariant requires scheduling sealed before any invocation")
		}
		if err := probe(tx, before); err != nil {
			return err
		}
		visited = true
		return refusal
	})
	if !visited || seq != 0 || !errors.Is(err, refusal) {
		t.Fatal("boundary probe did not reach its deliberate rejection", visited, seq, err)
	}
	for _, read := range []func(context.Context) (Snapshot, error){h.Snapshot, h.session.store.Snapshot} {
		after, err := read(bg)
		if err != nil {
			t.Fatal(err)
		}
		// Real MintID reservations survive rejection; no table/document may.
		if after.HighWater < before.HighWater {
			t.Fatal("allocator moved backwards")
		}
		after.HighWater = before.HighWater
		if !equalSnapshot(before, after) {
			t.Error("candidate probe changed adopted state")
		}
	}
}

func TestMainBoundaryEligibilityInheritedCancellationBeforePlacement(t *testing.T) {
	for _, lane := range []string{"start", "initial-follow-up", "post-tools-steer"} {
		for _, marked := range []bool{false, true} {
			name := "unmarked-positive"
			if marked {
				name = "inherited-abort"
			}
			t.Run(lane+"/"+name, func(t *testing.T) {
				sqliteStore(t, func(t *testing.T, b sqliteFixture) {
					h, c, parent := mainEligibilityHarness(t, b, true, &HarnessSettings{FollowUpMode: "all", SteeringMode: "all"})
					a, err := c.Submit(bg, Input{Content: "A"})
					if err != nil {
						t.Fatal("real A admission", err)
					}
					kind := "follow-up"
					if lane == "post-tools-steer" {
						kind = "steer"
					}
					queued, err := c.Submit(bg, Input{Type: kind, Content: "B"})
					if err != nil {
						t.Fatal("real B admission", err)
					}
					// Explicit end-boundary and adopted-parent-mark controls only.
					if _, err := h.session.Commit(bg, func(tx *Tx) error {
						live, err := builtin(tx, c.ID(), "pi.live")
						if err != nil {
							return err
						}
						if err := live.Update(func(value JSON) error { delete(value, "run"); return nil }); err != nil {
							return err
						}
						if marked {
							task := markTask(tx.state.Tasks[parent])
							return tx.stage(Write{Op: "put-task", Task: &task})
						}
						return nil
					}); err != nil {
						t.Fatal("prepare native ownership control", err)
					}
					mainEligibilityProbe(t, h, func(tx *Tx, before Snapshot) error {
						ending, cp := mainEligibilityTask(t, before, a.ID(), tx.limits)
						task, queuedCP := mainEligibilityTask(t, before, queued.ID(), tx.limits)
						status := publicSubmission(before.Submissions[queued.ID()])
						inbox := mainEligibilityDoc(t, before, c.ID(), "pi.inbox")
						items, ok := inbox.Value["items"].([]any)
						if status.Status != "queued" || status.Entry != 0 || status.Answer != 0 || queuedCP.Phase != "queued" || queuedCP.InputEntry != 0 || queuedCP.Input != "B" || queuedCP.Submission != queued.ID() || len(queuedCP.Steered) != 0 || !ok || len(items) != 1 || submissionEntryID(generationObject(items[0])["id"]) != queued.ID() {
							t.Fatal("B must be a genuine, unplaced admission in its actual inbox slot", status, queuedCP, inbox.Value)
						}
						if ending.Conversation != c.ID() || cp.InputEntry == 0 || cp.Submission != a.ID() || before.Entries[cp.InputEntry].Conversation != c.ID() || task.Owner != 0 || before.Conversations[c.ID()].Owner != parent || taskAborted(task) || taskHasDecidedOutcome(task) || terminalStatus(task.Status) || taskSelectedFailFastCancellation(before, task.ID) || len(h.scheduler.ownedLive(before, task.ID)) != 0 || taskBelowCancelled(before, task) != marked || taskAborted(before.Tasks[parent]) != marked {
							t.Fatal("inherited parent mark must be the only B eligibility difference", task, queuedCP)
						}
						if lane == "start" {
							if err := h.startQueuedRunBoundary(tx, c.ID(), ending.ID, false); err != nil {
								return err
							}
						} else if err := h.applyQueuedInputs(tx, ending, &cp, lane == "initial-follow-up"); err != nil {
							return err
						}
						candidate, err := tx.current()
						if err != nil {
							return err
						}
						live := mainEligibilityDoc(t, candidate, c.ID(), "pi.live").Value
						afterInbox := mainEligibilityDoc(t, candidate, c.ID(), "pi.inbox").Value
						placed := publicSubmission(candidate.Submissions[queued.ID()])
						if marked {
							if len(candidate.Entries) != len(before.Entries) || !reflect.DeepEqual(candidate.Submissions[queued.ID()], before.Submissions[queued.ID()]) || !reflect.DeepEqual(candidate.Tasks[task.ID], task) || !equalJSONValue(afterInbox, inbox.Value) || len(cp.Steered) != 0 {
								t.Error("inherited cancellation placed/consumed B or changed its placeholder", placed, cp)
							}
						} else {
							entry := candidate.Entries[placed.Entry]
							var message MessageReceipt
							if err := decodeEntryMessage(entry, tx.limits, &message); err != nil {
								return err
							}
							if placed.Status != "placed" || placed.Entry == 0 || placed.Answer != 0 || len(candidate.Entries) != len(before.Entries)+1 || entry.Conversation != c.ID() || !isMessageEntry(entry) || message.Role != "user" || len(message.Content) != 1 || message.Content[0].Text != "B" || !equalJSONValue(afterInbox["items"], []any{}) {
								t.Error("unmarked positive control must really place and consume only B", placed, entry, afterInbox)
							}
							_, selected := mainEligibilityTask(t, candidate, queued.ID(), tx.limits)
							if selected.InputEntry != placed.Entry || selected.Submission != queued.ID() || lane == "start" && selected.Phase != "queued" || lane != "start" && (selected.Phase != "terminal" || candidate.Tasks[task.ID].Status != "done" || !reflect.DeepEqual(cp.Steered, []ID{queued.ID()})) {
								t.Error("positive selector lost B's actual checkpoint/input ownership", selected, cp)
							}
						}
						if lane == "start" && !marked {
							want, err := dtoObject(JSON{"task": task.ID, "inputs": []any{queued.ID()}, "first": placed.Entry}, tx.limits)
							if err != nil {
								return err
							}
							if !equalJSONValue(live["run"], want) {
								t.Error("positive early boundary must install B's own run", live)
							}
						} else if _, installed := live["run"]; installed {
							t.Error("cancelled candidate/attachment helper installed a run", live)
						}
						return nil
					})
				})
			})
		}
	}
}

func TestMainBoundaryEligibilityAbsentAgentHostAllModes(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c, _ := mainEligibilityHarness(t, b, false, &HarnessSettings{FollowUpMode: "all", SteeringMode: "all"})
		a, err := c.Submit(bg, Input{Content: "A"})
		if err != nil {
			t.Fatal(err)
		}
		var queued []*SubmissionHandle
		for _, input := range []Input{{Type: "follow-up", Content: "B"}, {Type: "follow-up", Content: "C"}, {Type: "steer", Content: "S"}, {Type: "steer", Content: "T"}} {
			sub, err := c.Submit(bg, input)
			if err != nil {
				t.Fatal(err)
			}
			queued = append(queued, sub)
		}
		if _, err := h.session.Commit(bg, func(tx *Tx) error {
			live, err := builtin(tx, c.ID(), "pi.live")
			if err != nil {
				return err
			}
			return live.Update(func(value JSON) error { delete(value, "run"); return nil })
		}); err != nil {
			t.Fatal(err)
		}
		mainEligibilityProbe(t, h, func(tx *Tx, before Snapshot) error {
			ending, _ := mainEligibilityTask(t, before, a.ID(), tx.limits)
			if _, exists := agentDocument(before, c.ID()); exists || before.Conversations[c.ID()].Owner != 0 || before.Conversations[c.ID()].Parent != 0 {
				t.Fatal("host-mode oracle requires an actual agent-absent plain-Session root")
			}
			items, ok := mainEligibilityDoc(t, before, c.ID(), "pi.inbox").Value["items"].([]any)
			if !ok || len(items) != len(queued) {
				t.Fatal("expected four actual queued inputs", items)
			}
			for i, sub := range queued {
				task, cp := mainEligibilityTask(t, before, sub.ID(), tx.limits)
				status := publicSubmission(before.Submissions[sub.ID()])
				if status.Status != "queued" || status.Entry != 0 || status.Answer != 0 || cp.InputEntry != 0 || cp.Phase != "queued" || cp.Agent.Settings.FollowUpMode != "" || cp.Agent.Settings.SteeringMode != "" || submissionEntryID(generationObject(items[i])["id"]) != sub.ID() || taskBelowCancelled(before, task) {
					t.Fatal("real admission must leave all four unplaced, without stored mode overrides", status, cp)
				}
			}
			if err := h.startQueuedRunBoundary(tx, c.ID(), ending.ID, false); err != nil {
				return err
			}
			candidate, err := tx.current()
			if err != nil {
				return err
			}
			owner, cp := mainEligibilityTask(t, candidate, queued[0].ID(), tx.limits)
			var inputs []any
			var attached []ID
			var first ID
			lastPosition := -1
			for i, sub := range queued {
				status := publicSubmission(candidate.Submissions[sub.ID()])
				entry := candidate.Entries[status.Entry]
				var message MessageReceipt
				if err := decodeEntryMessage(entry, tx.limits, &message); err != nil {
					return err
				}
				wantText := []string{"B", "C", "S", "T"}[i]
				if status.Status != "placed" || status.Entry == 0 || status.Answer != 0 || entry.Conversation != c.ID() || !isMessageEntry(entry) || entry.Seq != before.Seq+1 || int(entry.Position) <= lastPosition || message.Role != "user" || len(message.Content) != 1 || message.Content[0].Text != wantText {
					t.Error("host all/all must place every real input in ID order", status, entry)
				}
				lastPosition = int(entry.Position)
				if i == 0 {
					first = status.Entry
				} else {
					attached = append(attached, sub.ID())
					task, selected := mainEligibilityTask(t, candidate, sub.ID(), tx.limits)
					if task.Status != "done" || selected.Phase != "terminal" || selected.InputEntry != status.Entry {
						t.Error("selected attachment must retain its own input entry", task, selected)
					}
				}
				inputs = append(inputs, sub.ID())
			}
			want, err := dtoObject(JSON{"task": owner.ID, "inputs": inputs, "first": first}, tx.limits)
			if err != nil {
				return err
			}
			live := mainEligibilityDoc(t, candidate, c.ID(), "pi.live").Value
			inbox := mainEligibilityDoc(t, candidate, c.ID(), "pi.inbox").Value
			if cp.InputEntry != first || cp.Submission != queued[0].ID() || cp.Phase != "queued" || !reflect.DeepEqual(cp.Steered, attached) || !equalJSONValue(live["run"], want) || !equalJSONValue(inbox["items"], []any{}) || len(candidate.Entries) != len(before.Entries)+4 {
				t.Error("agent-absent early boundary ignored host all/all or changed run ownership", cp, live, inbox)
			}
			if _, exists := agentDocument(candidate, c.ID()); exists {
				t.Error("early placement must not install optional pi.agent")
			}
			return nil
		})
	})
}

// Native source-derived regression, not an upstream test-body port. A's real
// provider failure and the B/all follow-up boundary share one deciding commit.
// A native waiter selects only A/C for failFast, so C must remain unplaced in
// that very commit, BEFORE bounded reconciliation can mark/withdraw it. Merely
// testing a previously failed or abort-marked A would miss cleanupGeneration's
// locally decided outcome: its caller has not staged A when cleanup begins.
// Both final and held outcomes are exercised. Only the held variant adds a
// real native child blocked on A's cancellation; the original no-child case
// must also pass, so a duplicate terminal-stage regression is not masked.
//
// Native fixture adaptation: Submit creates top-level legacy generation rows.
// While A is sealed before dispatch (and C is genuinely queued), privately set
// their Owner to the real native waiter. No execution envelope is replaced,
// no outcome/abort/live/checkpoint is seeded, and the wait itself is admitted
// through the waiter's real TaskRuntime.Commit with direct-child validation.
func TestMainBoundaryEligibilityLocallyDecidedFailFast(t *testing.T) {
	for _, held := range []bool{false, true} {
		name := "final"
		if held {
			name = "held"
		}
		t.Run(name, func(t *testing.T) {
			for _, policy := range []string{"failFast", "allSettled"} {
				t.Run(policy, func(t *testing.T) {
					sqliteStore(t, func(t *testing.T, b sqliteFixture) {
						aEntered, aRelease := make(chan struct{}), make(chan struct{})
						bEntered, bRelease := make(chan struct{}), make(chan struct{})
						requestB := make(chan []string, 1)
						members, waiting := make(chan []ID, 1), make(chan struct{})
						var calls atomic.Int64
						ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
							ch := make(chan goai.Event, 1)
							n := calls.Add(1)
							go func() {
								defer close(ch)
								gate := bRelease
								if n == 1 {
									gate = aRelease
									close(aEntered)
								} else if n == 2 {
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
								} else {
									t.Errorf("unexpected provider dispatch %d", n)
								}
								select {
								case <-gate:
								case <-ctx.Done():
									return
								}
								if n == 1 {
									msg := terminal("failed").Message
									msg.StopReason, msg.ErrorMessage = goai.StopReasonError, "A locally decided failure"
									ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: msg}
								} else {
									ch <- terminal("B answer")
								}
							}()
							return ch
						})
						waiter, err := DefineTask(TaskDefinitionOptions{
							Kind: "task.boundary.eligibility.waiter", Version: 1,
							Initial: func(any) (JSON, error) { return JSON{"phase": "wait"}, nil },
							Abort:   taskAbort,
							Phases: map[string]TaskPhase{
								"wait": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
									var on []ID
									select {
									case on = <-members:
									case <-ctx.Done():
										return ctx.Err()
									}
									if err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
										queued, err := copyTask(tx.state.Tasks[on[1]], tx.limits)
										if err != nil {
											return nil, err
										}
										if queued.Execution != nil || queued.Owner != 0 {
											return nil, errors.New("C must be an unreserved legacy admission before assigning fixture ownership")
										}
										queued.Owner = r.TaskID()
										if err := tx.stage(Write{Op: "put-task", Task: &queued}); err != nil {
											return nil, err
										}
										return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "join"}, On: on, Policy: policy}, nil
									}); err != nil {
										return err
									}
									close(waiting)
									return nil
								},
								"join": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
									return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("joined"), nil })
								},
							},
						})
						if err != nil {
							t.Fatal(err)
						}
						options.Registry = NewRegistry()
						if _, err := options.Registry.RegisterTask(waiter); err != nil {
							t.Fatal(err)
						}
						var child *TaskDefinition
						childEntered := make(chan struct{})
						if held {
							child = taskDefinition(t, "task.boundary.eligibility.held-child", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
								close(childEntered)
								<-ctx.Done()
								return ctx.Err()
							})
							if _, err := options.Registry.RegisterTask(child); err != nil {
								t.Fatal(err)
							}
						}
						options.Settings = &HarnessSettings{AfterFailedRun: "continue", FollowUpMode: "all"}
						options.OnReport = func(err error) { t.Error("scheduler", err) }
						h := openHarness(t, b.store, options)
						cleanupTaskGates(t, aRelease, bRelease)
						c := root(t, h, ref)
						h.scheduler.sealed.Store(true)
						var waiterID ID
						if _, err := h.CommitTasks(bg, c.ID(), func(tx *Tx) error {
							var err error
							waiterID, err = tx.CreateTask(waiter, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
							return err
						}); err != nil {
							t.Fatal(err)
						}
						a, err := c.Submit(bg, Input{Content: "A"})
						if err != nil {
							t.Fatal(err)
						}
						if _, err := h.session.Commit(bg, func(tx *Tx) error {
							task, _ := mainEligibilityTask(t, tx.state, a.ID(), tx.limits)
							if task.Execution != nil || len(h.scheduler.invocations) != 0 {
								return errors.New("A ownership must precede every dispatch")
							}
							task.Owner = waiterID
							return tx.stage(Write{Op: "put-task", Task: &task})
						}); err != nil {
							t.Fatal(err)
						}
						h.scheduler.sealed.Store(false)
						if err := h.Resume(bg); err != nil {
							t.Fatal(err)
						}
						awaitTaskSignal(t, aEntered)
						if held {
							if _, err := h.CommitTasks(bg, c.ID(), func(tx *Tx) error {
								aTask, _ := mainEligibilityTask(t, tx.state, a.ID(), tx.limits)
								_, err := tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: aTask.ID}})
								return err
							}); err != nil {
								t.Fatal(err)
							}
							awaitTaskSignal(t, childEntered)
						}
						var queued []*SubmissionHandle
						for _, text := range []string{"B", "C"} {
							sub, err := c.Submit(bg, Input{Type: "follow-up", Content: text})
							if err != nil {
								t.Fatal(err)
							}
							queued = append(queued, sub)
						}
						before, err := h.Snapshot(bg)
						if err != nil {
							t.Fatal(err)
						}
						aTask, _ := mainEligibilityTask(t, before, a.ID(), h.session.limits)
						cTask, _ := mainEligibilityTask(t, before, queued[1].ID(), h.session.limits)
						members <- []ID{aTask.ID, cTask.ID}
						awaitTaskSignal(t, waiting)
						before, err = h.Snapshot(bg)
						if err != nil {
							t.Fatal(err)
						}
						aTask, aCP := mainEligibilityTask(t, before, a.ID(), h.session.limits)
						bTask, _ := mainEligibilityTask(t, before, queued[0].ID(), h.session.limits)
						cTask, _ = mainEligibilityTask(t, before, queued[1].ID(), h.session.limits)
						wait := before.Tasks[waiterID]
						if wait.Execution == nil || wait.Execution.Native == nil || wait.Execution.Native.State.Status != "waiting" || wait.Execution.Native.State.Policy != policy || !reflect.DeepEqual(wait.Execution.Native.State.On, []ID{aTask.ID, cTask.ID}) || aTask.Owner != waiterID || cTask.Owner != waiterID || bTask.Owner != 0 || before.Conversations[c.ID()].Owner != 0 {
							t.Fatal("requires a valid real native wait on A/C only, with B outside its ownership", wait, aTask, bTask, cTask)
						}
						if aTask.Status != "running" || taskHasDecidedOutcome(aTask) || taskAborted(aTask) || aCP.InputEntry == 0 || len(aCP.Steered) != 0 || calls.Load() != 1 {
							t.Fatal("A must be running at the real provider, not an existing failure/abort", aTask, aCP, calls.Load())
						}
						if got := len(taskOwnedLive(before, aTask.ID)); got != 0 && !held || got != 1 && held {
							t.Fatal("requires exactly the selected final/held workload before A decides", held, got)
						}
						for _, sub := range queued {
							task, cp := mainEligibilityTask(t, before, sub.ID(), h.session.limits)
							status := publicSubmission(before.Submissions[sub.ID()])
							if status.Status != "queued" || status.Entry != 0 || status.Answer != 0 || cp.Phase != "queued" || cp.InputEntry != 0 || taskAborted(task) || taskHasDecidedOutcome(task) || taskBelowCancelled(before, task) || taskSelectedFailFastCancellation(before, task.ID) {
								t.Fatal("B/C must be eligible unplaced inputs before A decides", status, task, cp)
							}
						}
						items := mainEligibilityDoc(t, before, c.ID(), "pi.inbox").Value["items"].([]any)
						if len(items) != 2 || submissionEntryID(generationObject(items[0])["id"]) != queued[0].ID() || submissionEntryID(generationObject(items[1])["id"]) != queued[1].ID() {
							t.Fatal("requires real B/C inbox slots", items)
						}
						decision := mainPreparationTerminalPublication(t, h, a.ID())
						releaseTaskGate(aRelease)
						ended := waitSubmission(t, a)
						publication := decision()
						awaitTaskSignal(t, bEntered)
						if ended.Record.Status != "unanswered" || ended.Record.Reason != "model_error" || ended.Record.Answer != 0 || ended.Record.Detail == nil || ended.Record.Detail.Value != "A locally decided failure" {
							t.Fatal("real A failure settlement", ended.Record)
						}
						// Inspect the ACTUAL A-deciding batch, not a later snapshot where
						// reconciliation could hide a transiently placed/consumed C.
						subs, tasks := map[ID]Submission{}, map[ID]Task{}
						var live, inbox JSON
						var userTexts []string
						for _, write := range publication.Tables {
							if write.Submission != nil {
								subs[write.Submission.ID] = *write.Submission
							}
							if write.Task != nil {
								tasks[write.Task.ID] = *write.Task
							}
							if write.Op == "append-entry" && write.Entry != nil && write.Entry.Conversation == c.ID() {
								var message MessageReceipt
								if err := decodeEntryMessage(*write.Entry, h.session.limits, &message); err != nil {
									t.Fatal(err)
								}
								if message.Role == "user" {
									for _, block := range message.Content {
										userTexts = append(userTexts, block.Text)
									}
								}
							}
						}
						for _, change := range publication.Documents {
							if change.Record.Owner == c.ID() && !change.Record.Retired {
								switch change.Record.Kind {
								case "pi.live":
									live = change.Value
								case "pi.inbox":
									inbox = change.Value
								}
							}
						}
						if !taskFailedOutcome(tasks[aTask.ID]) || taskAborted(tasks[aTask.ID]) || publicSubmission(subs[a.ID()]).Reason != "model_error" {
							t.Fatal("batch must decide A's failure without a preexisting abort", tasks[aTask.ID], subs[a.ID()])
						}
						placedB := publicSubmission(subs[queued[0].ID()])
						var bCP generationCheckpoint
						if err := fromObject(tasks[bTask.ID].Checkpoint, &bCP, h.session.limits); err != nil {
							t.Fatal(err)
						}
						if placedB.Status != "placed" || placedB.Entry == 0 || bCP.InputEntry != placedB.Entry || bCP.Submission != queued[0].ID() {
							t.Fatal("A-deciding batch must start real B, not suppress the whole boundary", placedB, bCP)
						}
						wantInputs, wantSteered := []any{queued[0].ID()}, []ID(nil)
						wantUsers, wantRequest := []string{"B"}, []string{"A", "B"}
						if policy == "failFast" {
							_, touchedSub := subs[queued[1].ID()]
							_, touchedTask := tasks[cTask.ID]
							if touchedSub || touchedTask {
								t.Errorf("A-deciding failFast boundary processed C before reconciliation: placedEntry=%d taskStatus=%s", publicSubmission(subs[queued[1].ID()]).Entry, tasks[cTask.ID].Status)
							}
							remaining, ok := inbox["items"].([]any)
							if !ok || len(remaining) != 1 || submissionEntryID(generationObject(remaining[0])["id"]) != queued[1].ID() {
								t.Error("failFast C must retain its actual inbox slot in A's deciding batch", inbox)
							}
						} else {
							// Identical failure/ownership with allSettled is the positive
							// control: FollowUpMode=all really attaches and consumes C.
							placedC := publicSubmission(subs[queued[1].ID()])
							var attached generationCheckpoint
							if err := fromObject(tasks[cTask.ID].Checkpoint, &attached, h.session.limits); err != nil {
								t.Fatal(err)
							}
							if placedC.Status != "placed" || placedC.Entry == 0 || placedC.Answer != 0 || tasks[cTask.ID].Status != "done" || attached.Phase != "terminal" || attached.InputEntry != placedC.Entry || !equalJSONValue(inbox["items"], []any{}) {
								t.Error("allSettled positive control must process C in A's deciding batch", placedC, attached, inbox)
							}
							wantInputs = append(wantInputs, queued[1].ID())
							wantSteered = []ID{queued[1].ID()}
							wantUsers, wantRequest = []string{"B", "C"}, []string{"A", "B", "C"}
						}
						wantRun, err := dtoObject(JSON{"task": bTask.ID, "inputs": wantInputs, "first": placedB.Entry}, h.session.limits)
						if err != nil {
							t.Fatal(err)
						}
						if !equalJSONValue(live["run"], wantRun) || !reflect.DeepEqual(bCP.Steered, wantSteered) || !reflect.DeepEqual(userTexts, wantUsers) {
							t.Error("wrong same-transaction B/C placement or run ownership", live, bCP, userTexts)
						}
						if got := <-requestB; !reflect.DeepEqual(got, wantRequest) || calls.Load() != 2 {
							t.Error("actual successor provider request processed the wrong inputs", got, wantRequest, calls.Load())
						}
						t.Logf("A-deciding seq=%d policy=%s: new users=%v; B steered=%v; C touched=%t; provider calls=%d", publication.Seq, policy, userTexts, bCP.Steered, subs[queued[1].ID()].ID != 0, calls.Load())
						// Cancel gated B; do not let an ordinary answer mask C's prefix.
						if err := h.Close(bg); err != nil {
							t.Fatal(err)
						}
					})
				})
			}
		})
	}
}
