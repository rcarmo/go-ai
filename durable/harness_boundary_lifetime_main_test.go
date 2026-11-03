package durable

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Source-derived oracle, not a complete upstream test-body port: official
// v1.5.0 f9a26365568127461c47a07a37dcb42c140a3ac5 submissions.ts
// admitSubmission/startRun and generation.ts abort/live.ts endRun. Admission
// owns the placed input before model resolution or attempt presentation. Native
// adapters: numeric run.task, queued preparation checkpoint, initial Attempt=0.
func TestMainBoundaryLifetimeBeforeModelResolution(t *testing.T) {
	for _, mode := range []string{"conversation-abort", "task-abort", "legacy-first-absent"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
				providerEntered := make(chan struct{}, 1)
				var resolutions, calls atomic.Int64
				ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					calls.Add(1)
					events := make(chan goai.Event)
					select {
					case providerEntered <- struct{}{}:
					default:
					}
					go func() { <-ctx.Done(); close(events) }()
					return events
				})
				resolve := options.Models
				options.Models = func(provider goai.Provider, id string) *goai.Model {
					if resolutions.Add(1) == 1 {
						close(entered)
						<-release // Deliberately retain the actual callback after cancellation.
						defer close(returned)
					}
					return resolve(provider, id) // Valid model: cancellation, not nil, must fence intent.
				}
				options.OnReport = func(err error) { t.Error("scheduler", err) }
				h := openHarness(t, b.store, options)
				cleanupTaskGates(t, release) // Release before Harness.Close, including assertion failures.
				c := root(t, h, ref)
				sub, err := c.Submit(bg, Input{Content: "admitted before model resolution"})
				if err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, entered)
				before, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				live := mainAbortFailureLive(t, before, c.ID())
				run := generationObject(live["run"])
				owner := submissionEntryID(run["task"])
				status := publicSubmission(before.Submissions[sub.ID()])
				first := status.Entry
				task, exists := before.Tasks[owner]
				if owner == 0 || !exists || task.Kind != "pi.generation" || task.Conversation != c.ID() || task.Status != "running" || status.Status != "placed" || first == 0 || status.Answer != 0 {
					t.Fatal("idle admission must already own a real placed run before resolution", task, status, live)
				}
				wantRun, err := dtoObject(JSON{"task": owner, "inputs": []any{sub.ID()}, "first": first}, h.session.limits)
				if err != nil {
					t.Fatal(err)
				}
				wantLive, err := dtoObject(JSON{"run": wantRun}, h.session.limits)
				if err != nil {
					t.Fatal(err)
				}
				if !equalJSONValue(live, wantLive) {
					t.Fatalf("admission must publish only its own run, with no attempt presentation: got %v; want %v", live, wantLive)
				}
				entry, exists := before.Entries[first]
				var input MessageReceipt
				if !exists || entry.Conversation != c.ID() || entry.ByTask != 0 || entry.Kind != "message" || entry.Seq == 0 {
					t.Fatal("admission first must be its actual persisted input, not a synthetic ID", entry)
				}
				if err := fromObject(entry.Value, &input, h.session.limits); err != nil {
					t.Fatal(err)
				}
				if input.Role != goai.RoleUser || len(input.Content) != 1 || input.Content[0].Text != "admitted before model resolution" {
					t.Fatal("admission lost its actual user contribution", input)
				}
				decode := func(task Task) generationCheckpoint {
					t.Helper()
					var cp generationCheckpoint
					if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
						t.Fatal(err)
					}
					return cp
				}
				cp := decode(task)
				if cp.Phase != "queued" || cp.Model != nil || cp.Attempt != 0 || cp.Submission != sub.ID() || cp.InputEntry != first || cp.Abort || len(cp.Steered) != 0 || calls.Load() != 0 || resolutions.Load() != 1 {
					t.Fatal("gate must precede pinned model, intent, attempts and dispatch", cp, calls.Load(), resolutions.Load())
				}
				for _, doc := range before.Documents {
					if !doc.Retired && doc.Owner == c.ID() && doc.Kind == "pi.inbox" {
						items, ok := doc.Value["items"].([]any)
						if !ok || len(items) != 0 {
							t.Fatal("placed input must no longer occupy the queue", doc.Value)
						}
					}
				}
				var old *TaskRuntime
				if err := h.session.readTasks(bg, func(Snapshot) error {
					old = h.scheduler.invocations[owner]
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				if old == nil || old.abortMode || old.ended.Load() {
					t.Fatal("resolver must retain its actual ordinary invocation")
				}

				if mode == "legacy-first-absent" {
					// Explicit legacy control only: remove first from THIS actual run.
					// Do not fabricate a run, entry, checkpoint, inbox or settlement.
					if _, err := h.session.Commit(bg, func(tx *Tx) error {
						doc, err := builtin(tx, c.ID(), "pi.live")
						if err != nil {
							return err
						}
						return doc.Update(func(value JSON) error {
							delete(generationObject(value["run"]), "first")
							return nil
						})
					}); err != nil {
						t.Fatal(err)
					}
					delete(wantRun, "first")
					releaseTaskGate(release)
					awaitTaskSignal(t, providerEntered)
					prepared, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					preparedRun := generationObject(mainAbortFailureLive(t, prepared, c.ID())["run"])
					preparedCP := decode(prepared.Tasks[owner])
					if !equalJSONValue(preparedRun, wantRun) || preparedCP.Phase != "intent" || preparedCP.Model == nil || preparedCP.Attempt != 1 || preparedCP.InputEntry != first {
						t.Error("preparation must preserve own-run first absence, not backfill from InputEntry", preparedRun, preparedCP)
					}
					if err := c.Abort(bg); err != nil {
						t.Fatal(err)
					}
				} else {
					aborted := make(chan struct{})
					var abortErr error
					var disposition string
					go func() {
						defer close(aborted)
						if mode == "conversation-abort" {
							abortErr = c.Abort(bg)
						} else {
							disposition, abortErr = h.AbortTask(bg, owner)
						}
					}()
					t.Cleanup(func() {
						releaseTaskGate(release)
						awaitTaskSignal(t, aborted)
					})
					ctx, cancel := context.WithTimeout(bg, 3*time.Second)
					defer cancel()
					for {
						record, found, err := h.Task(ctx, owner)
						if err != nil || !found {
							t.Fatal("abort mark lookup", record, err)
						}
						if record.AbortRequested {
							break
						}
						if ctx.Err() != nil {
							t.Fatal("abort mark was not adopted", ctx.Err())
						}
						runtime.Gosched()
					}
					awaitTaskSignal(t, old.Context().Done())
					marked, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					markedCP := decode(marked.Tasks[owner])
					markedStatus := publicSubmission(marked.Submissions[sub.ID()])
					if terminalStatus(marked.Tasks[owner].Status) || markedStatus.Status != "placed" || markedStatus.Entry != first || markedStatus.Answer != 0 || !equalJSONValue(mainAbortFailureLive(t, marked, c.ID()), wantLive) || markedCP.Phase != "queued" || markedCP.Model != nil || markedCP.Attempt != 0 || markedCP.InputEntry != first || !markedCP.Abort {
						t.Error("abort must mark/join the admitted run, not withdraw placed input as queued", marked.Tasks[owner], markedStatus, mainAbortFailureLive(t, marked, c.ID()))
					}
					for label, signal := range map[string]<-chan struct{}{"resolver": returned, "invocation": old.done, "public abort": aborted} {
						select {
						case <-signal:
							t.Error(label, "joined before the blocked model callback returned")
						default:
						}
					}
					var effects int
					if err := old.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) { effects++; return nil, nil }); err == nil || effects != 0 {
						t.Error("marked invocation admitted a mutation callback", err, effects)
					}
					if calls.Load() != 0 || resolutions.Load() != 1 {
						t.Error("abort dispatched or resolved again before callback join", calls.Load(), resolutions.Load())
					}
					releaseTaskGate(release) // Never synchronously wait for Abort while holding this gate.
					awaitTaskSignal(t, aborted)
					if abortErr != nil || mode == "task-abort" && disposition != "marked" {
						t.Error("public abort", disposition, abortErr)
					}
				}

				awaitTaskSignal(t, returned)
				awaitTaskSignal(t, old.done)
				if _, err := old.Registry(); !errors.Is(err, ErrSealed) {
					t.Error("returned resolver invocation retained authority", err)
				}
				settled := waitSubmission(t, sub)
				result := waitPublicTask(t, h, owner)
				idleFailed140(t, h)
				if settled.Task.ID != owner || settled.Record.Status != "unanswered" || settled.Record.Reason != "aborted" || settled.Record.Entry != first || settled.Record.Answer != 0 || settled.Record.Detail != nil || settled.Message != nil || result.State.Outcome == nil || result.State.Outcome.Status != "aborted" {
					t.Error("actual admitted run must settle unanswered/aborted at its own entry, not queue withdrawal", settled, result)
				}
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				failure := JSON{"taskId": owner, "inputs": []any{sub.ID()}, "reason": "aborted"}
				if mode != "legacy-first-absent" {
					failure["first"] = first
					finalCP := decode(state.Tasks[owner])
					if finalCP.Phase != "terminal" || !finalCP.Abort || finalCP.Model != nil || finalCP.Attempt != 0 || finalCP.InputEntry != first || finalCP.Submission != sub.ID() {
						t.Error("cancelled resolver resurrected request intent or lost its placement", finalCP)
					}
				}
				want, err := dtoObject(JSON{"runFailure": failure}, h.session.limits)
				if err != nil {
					t.Fatal(err)
				}
				if got := mainAbortFailureLive(t, state, c.ID()); !equalJSONValue(got, want) {
					t.Errorf("abort must clear run/presentation and record its actual failure: got %v; want %v", got, want)
				}
				if len(state.Entries) != len(before.Entries) || !equalJSONValue(state.Entries[first].Value, entry.Value) {
					t.Error("abort changed transcript or synthesized an assistant without a committed partial")
				}
				for id, doc := range before.Documents {
					if doc.Kind == "pi.usage" && !equalJSONValue(state.Documents[id].Value, doc.Value) {
						t.Error("abort without provider output changed usage")
					}
				}
				wantCalls, wantResolutions := int64(0), int64(1)
				if mode == "legacy-first-absent" {
					wantCalls, wantResolutions = 1, 2
				}
				if calls.Load() != wantCalls || resolutions.Load() != wantResolutions {
					t.Error("unexpected provider/model callbacks after join", calls.Load(), resolutions.Load())
				}
			})
		})
	}
}

// Native compatibility control, not an upstream legacy-state test-body port:
// conversationGenerationBusy must include an actual reserved/running generation
// whose checkpoint is still queued and whose first-admission live.run is absent.
// Seed that valid pre-boundary state on the real Session before Resume; all IDs
// are minted there, and preparation itself must append the first real entry.
func TestMainBoundaryLegacyRecoveryBusyControl(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
		providerRelease := make(chan struct{})
		providerEntered := make(chan []string, 1)
		var resolutions, calls atomic.Int64
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			if calls.Add(1) == 1 {
				var users []string
				for _, message := range input.Messages {
					if message.Role == goai.RoleUser {
						for _, block := range message.Content {
							users = append(users, block.Text)
						}
					}
				}
				providerEntered <- users
			}
			events := make(chan goai.Event, 1)
			go func() {
				defer close(events)
				select {
				case <-providerRelease:
					events <- terminal("prepared answer")
				case <-ctx.Done():
				}
			}()
			return events
		})
		resolve := options.Models
		options.Models = func(provider goai.Provider, id string) *goai.Model {
			if resolutions.Add(1) == 1 {
				close(entered)
				<-release
				defer close(returned)
			}
			return resolve(provider, id)
		}
		options.Settings = &HarnessSettings{FollowUpMode: "one-at-a-time"}
		options.OnReport = func(err error) { t.Error("scheduler", err) }
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release, providerRelease) // Release callbacks before Close, including failures.
		c := root(t, h, ref)
		var input, owner ID
		if _, err := h.session.Commit(bg, func(tx *Tx) error {
			var err error
			input, err = tx.MintID()
			if err != nil {
				return err
			}
			owner, err = tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.PutSubmission(Submission{ID: input, Conversation: c.ID(), Type: "follow-up", Status: "pending", Value: JSON{"content": "A"}}); err != nil {
				return err
			}
			checkpoint, err := dtoObject(generationCheckpoint{Phase: "queued", Submission: input, Input: "A"}, tx.limits)
			if err != nil {
				return err
			}
			if err := tx.PutTask(Task{ID: owner, Conversation: c.ID(), Kind: "pi.generation", Status: "pending", Checkpoint: checkpoint}); err != nil {
				return err
			}
			inbox, err := builtin(tx, c.ID(), "pi.inbox")
			if err != nil {
				return err
			}
			return inbox.Update(func(value JSON) error {
				value["items"] = []any{JSON{"id": input, "mode": "followUp", "input": JSON{"content": "A"}}}
				return nil
			})
		}); err != nil {
			t.Fatal("seed valid native compatibility checkpoint", err)
		}
		snapshot := func() Snapshot {
			t.Helper()
			state, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			return state
		}
		decode := func(task Task) generationCheckpoint {
			t.Helper()
			var cp generationCheckpoint
			if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
				t.Fatal(err)
			}
			return cp
		}
		requireInbox := func(state Snapshot, want ...ID) {
			t.Helper()
			found := 0
			for _, doc := range state.Documents {
				if doc.Owner != c.ID() || doc.Kind != "pi.inbox" || doc.Retired {
					continue
				}
				found++
				items, ok := doc.Value["items"].([]any)
				if !ok || len(items) != len(want) {
					t.Fatal("unexpected actual inbox", doc.Value, want)
				}
				for i, id := range want {
					if submissionEntryID(generationObject(items[i])["id"]) != id {
						t.Fatal("inbox lost or reordered actual input", doc.Value, want)
					}
				}
			}
			if found != 1 {
				t.Fatal("expected exactly one inbox", found)
			}
		}
		seeded := snapshot()
		if h.scheduler.enabled.Load() || seeded.Tasks[owner].Status != "pending" || len(generationObject(mainAbortFailureLive(t, seeded, c.ID())["run"])) != 0 || publicSubmission(seeded.Submissions[input]).Entry != 0 {
			t.Fatal("fixture must precede scheduling and first admission", seeded.Tasks[owner])
		}
		requireInbox(seeded, input)
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		before := snapshot()
		cp := decode(before.Tasks[owner])
		var invocation *TaskRuntime
		if err := h.session.readTasks(bg, func(Snapshot) error {
			invocation = h.scheduler.invocations[owner]
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if invocation == nil || invocation.abortMode || invocation.ended.Load() || before.Tasks[owner].Status != "running" || cp.Phase != "queued" || cp.Submission != input || cp.InputEntry != 0 || cp.Model != nil || cp.Attempt != 0 || cp.Abort || len(generationObject(mainAbortFailureLive(t, before, c.ID())["run"])) != 0 || calls.Load() != 0 || resolutions.Load() != 1 {
			t.Fatal("must gate actual reserved/running queued preparation without live.run", before.Tasks[owner], cp)
		}
		rejected, err := c.Submit(bg, Input{Content: "must reject", WhenBusy: "reject"})
		if !errors.Is(err, ErrConversationBusy) || rejected != nil {
			t.Fatal("legacy queued preparation is busy; rejection must not steal its run", rejected, err)
		}
		afterReject := snapshot()
		if len(afterReject.Submissions) != len(before.Submissions) || len(afterReject.Tasks) != len(before.Tasks) || len(afterReject.Entries) != len(before.Entries) || !equalJSONValue(afterReject.Tasks[owner].Checkpoint, before.Tasks[owner].Checkpoint) || !equalJSONValue(mainAbortFailureLive(t, afterReject, c.ID()), mainAbortFailureLive(t, before, c.ID())) {
			t.Fatal("rejected admission changed durable work or live ownership")
		}
		requireInbox(afterReject, input)
		queued, err := c.Submit(bg, Input{Content: "B"})
		if err != nil {
			t.Fatal("default busy input must queue", err)
		}
		queuedState := snapshot()
		status := publicSubmission(queuedState.Submissions[queued.ID()])
		if status.Status != "queued" || status.Entry != 0 || status.Answer != 0 || len(queuedState.Entries) != len(before.Entries) || !equalJSONValue(queuedState.Tasks[owner].Checkpoint, before.Tasks[owner].Checkpoint) || len(generationObject(mainAbortFailureLive(t, queuedState, c.ID())["run"])) != 0 || calls.Load() != 0 || resolutions.Load() != 1 {
			t.Fatal("default input must stay queued/unplaced without stealing A's preparation", status)
		}
		requireInbox(queuedState, input, queued.ID())
		if err := h.session.readTasks(bg, func(Snapshot) error {
			if h.scheduler.invocations[owner] != invocation || invocation.ended.Load() {
				t.Error("new admission replaced or ended A's actual reservation")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(release)
		awaitTaskSignal(t, returned)
		users := watchReceive(t, providerEntered)
		if len(users) != 1 || users[0] != "A" {
			t.Fatal("A's preparation must dispatch its own input, not the queued successor", users)
		}
		prepared := snapshot()
		preparedCP := decode(prepared.Tasks[owner])
		run := generationObject(mainAbortFailureLive(t, prepared, c.ID())["run"])
		first := submissionEntryID(run["first"])
		entry, exists := prepared.Entries[first]
		var receipt MessageReceipt
		if !exists || first == 0 || entry.Conversation != c.ID() || entry.Kind != "message" || entry.Seq == 0 {
			t.Fatal("preparation must append an actual first entry", run, entry)
		}
		if err := fromObject(entry.Value, &receipt, h.session.limits); err != nil {
			t.Fatal(err)
		}
		inputs, ok := run["inputs"].([]any)
		if submissionEntryID(run["task"]) != owner || !ok || len(inputs) != 1 || submissionEntryID(inputs[0]) != input || preparedCP.Phase != "intent" || preparedCP.InputEntry != first || preparedCP.Model == nil || preparedCP.Attempt != 1 || receipt.Role != goai.RoleUser || len(receipt.Content) != 1 || receipt.Content[0].Text != "A" || publicSubmission(prepared.Submissions[input]).Entry != first || publicSubmission(prepared.Submissions[queued.ID()]).Status != "queued" || publicSubmission(prepared.Submissions[queued.ID()]).Entry != 0 {
			t.Fatal("A must prepare under its own run; B must remain unplaced", run, preparedCP)
		}
		requireInbox(prepared, queued.ID())
		releaseTaskGate(providerRelease)
		a, err := h.Submission(bg, input)
		if err != nil || a == nil {
			t.Fatal("recover actual seeded submission", a, err)
		}
		settled := waitSubmission(t, a)
		result := waitPublicTask(t, h, owner)
		if settled.Task.ID != owner || settled.Record.Status != "done" || settled.Record.Entry != first || settled.Record.Answer == 0 || settled.Message == nil || settled.Message.ErrorCode != "" || len(settled.Message.Content) != 1 || settled.Message.Content[0].Text != "prepared answer" || result.State.Outcome == nil || result.State.Outcome.Status != "completed" {
			t.Fatal("released A must complete, not fail invalid_preparation", settled, result)
		}
		successor := waitSubmission(t, queued)
		idleFailed140(t, h)
		if successor.Task.ID == owner || successor.Record.Status != "done" || successor.Record.Entry == 0 || successor.Record.Entry == first || successor.Record.Answer == 0 || successor.Message == nil || successor.Message.ErrorCode != "" || calls.Load() != 2 || resolutions.Load() != 4 {
			t.Fatal("queued successor must complete independently without invalid_preparation", successor, calls.Load(), resolutions.Load())
		}
	})
}
