package durable

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestMainBoundaryOwnedInvocationIdleChildAdmission(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		entered, release, submitted := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var requests atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if requests.Add(1) != 1 {
				t.Error("unexpected child provider request")
				return
			}
			close(entered)
			select {
			case <-release:
				taskHTTPRound(w, nil)
			case <-req.Context().Done():
			}
		}))
		t.Cleanup(server.Close)
		model := fakeModel(goai.ApiOpenAICompletions)
		model.BaseURL = server.URL
		model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
		var childID, subID ID
		caller := taskDefinition(t, "task.boundary.owned.caller", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			child, err := r.CreateOwnedConversation(ctx, "idle-child", nil)
			if err != nil {
				return err
			}
			childID = child.ID()
			bound, err := r.Conversation(ctx, childID)
			if err != nil {
				return err
			}
			sub, err := bound.Submit(ctx, Input{Content: "child input", WhenBusy: "reject"})
			if err != nil {
				return err
			}
			subID = sub.ID()
			close(submitted)
			settled, err := sub.Wait(ctx)
			if err != nil {
				return err
			}
			if settled.Record.Status != "done" || settled.Record.Answer == 0 {
				return fmt.Errorf("bound child did not answer: %+v", settled.Record)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("caller"), nil })
		})
		h := taskTestHarnessOptions(t, b.store, Options{Models: func(goai.Provider, string) *goai.Model { return model }, OnReport: func(err error) { t.Error("scheduler", err) }}, caller)
		cleanupTaskGates(t, release)
		c := root(t, h, ModelRef{Provider: model.Provider, ID: model.ID})
		callerID := createPublicTask(t, h, caller, nil, TaskOptions{Conversation: c.ID(), Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, submitted)
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		status := publicSubmission(state.Submissions[subID])
		run := generationObject(mainAbortFailureLive(t, state, childID)["run"])
		generationID := submissionEntryID(run["task"])
		entry, exists := state.Entries[status.Entry]
		if childID == c.ID() || state.Conversations[childID].Owner != callerID || status.Status != "placed" || status.Answer != 0 || !exists || entry.Conversation != childID || entry.ByTask != callerID || !isMessageEntry(entry) {
			t.Fatal("bound idle admission must commit its real input attributed to the caller", status, entry, state.Conversations[childID])
		}
		var input MessageReceipt
		if err := decodeEntryMessage(entry, h.session.limits, &input); err != nil || input.Role != goai.RoleUser || input.ContentText == nil || *input.ContentText != "child input" {
			t.Fatal("bound input payload", input, err)
		}
		wantRun, err := dtoObject(JSON{"task": generationID, "inputs": []any{subID}, "first": entry.ID}, h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		generation := state.Tasks[generationID]
		var cp generationCheckpoint
		if err := fromObject(generation.Checkpoint, &cp, h.session.limits); err != nil {
			t.Fatal(err)
		}
		if generationID == 0 || generationID == callerID || generation.Kind != "pi.generation" || generation.Conversation != childID || !equalJSONValue(run, wantRun) || cp.Submission != subID || cp.InputEntry != entry.ID {
			t.Fatal("bound admission must publish the child's distinct live run, not the caller's generation", run, generation, cp)
		}
		awaitTaskSignal(t, entered)
		releaseTaskGate(release)
		if result := waitPublicTask(t, h, callerID); result.State.Outcome == nil || result.State.Outcome.Status != "completed" {
			t.Fatal("caller failed to observe bound child completion", result)
		}
		waitPublicTask(t, h, generationID)
		hostSub, err := h.Submission(bg, subID)
		if err != nil || hostSub == nil {
			t.Fatal("child submission lookup", err)
		}
		settled := waitSubmission(t, hostSub)
		state, err = h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		answer := state.Entries[settled.Record.Answer]
		if settled.Task.ID != generationID || settled.Record.Status != "done" || settled.Record.Entry != entry.ID || settled.Record.Answer == 0 || answer.ByTask != generationID || answer.Conversation != childID || state.Entries[entry.ID].ByTask != callerID || len(generationObject(mainAbortFailureLive(t, state, childID)["run"])) != 0 || requests.Load() != 1 {
			t.Fatal("completion must preserve caller input and generation answer attribution", settled, answer)
		}
	})
}

func TestMainBoundaryOwnedAfterResponseHeldAdmission(t *testing.T) {
	for _, mode := range []string{"reject", "input", "passive-write", "reset"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				childEntered, releaseChild := make(chan struct{}), make(chan struct{})
				bEntered, releaseB := make(chan struct{}), make(chan struct{})
				child := taskDefinition(t, "task.boundary.response.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					close(childEntered)
					<-releaseChild
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("child"), nil })
				})
				registry := NewRegistry()
				if _, err := registry.RegisterTask(child); err != nil {
					t.Fatal(err)
				}
				var hookCalls atomic.Int64
				var ownerID, childID ID
				if err := registry.Install(&Extension{Name: "boundary.response.owned", Hooks: GenerationHooks{AfterResponse: func(ctx context.Context, _ MessageReceipt, api *HookAPI) error {
					if hookCalls.Add(1) != 1 {
						return nil
					}
					ownerID = api.TaskID()
					if err := api.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
						var err error
						childID, err = tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: ownerID}})
						return nil, err
					}); err != nil {
						return err
					}
					awaitTaskSignal(t, childEntered)
					return nil
				}}}); err != nil {
					t.Fatal(err)
				}
				var requests atomic.Int64
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					switch n := requests.Add(1); n {
					case 1:
						taskHTTPRound(w, nil)
					case 2:
						close(bEntered)
						select {
						case <-releaseB:
							taskHTTPRound(w, nil)
						case <-req.Context().Done():
						}
					default:
						t.Errorf("unexpected provider request %d", n)
					}
				}))
				t.Cleanup(server.Close)
				model := fakeModel(goai.ApiOpenAICompletions)
				model.BaseURL = server.URL
				model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
				h := openHarness(t, b.store, Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }, OnReport: func(err error) { t.Error("scheduler", err) }})
				cleanupTaskGates(t, releaseChild, releaseB)
				c := root(t, h, ModelRef{Provider: model.Provider, ID: model.ID})
				a, err := c.Submit(bg, Input{Content: "A"})
				if err != nil {
					t.Fatal(err)
				}
				aResult := waitSubmission(t, a)
				snapshot := func() Snapshot {
					t.Helper()
					state, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					return state
				}
				requireHeld := func(state Snapshot) {
					t.Helper()
					owner, err := CanonicalTask(state.Tasks[ownerID], h.session.limits)
					if err != nil || owner.State.Status != "completing" || owner.State.Outcome == nil || owner.State.Outcome.Status != "completed" || state.Tasks[childID].Owner != ownerID || terminalStatus(state.Tasks[childID].Status) {
						t.Fatal("A must remain answered but held by its real unfinished child", owner, state.Tasks[childID], err)
					}
				}
				before := snapshot()
				requireHeld(before)
				if aResult.Task.ID != ownerID || aResult.Record.Status != "done" || aResult.Record.Answer == 0 || len(generationObject(mainAbortFailureLive(t, before, c.ID())["run"])) != 0 || requests.Load() != 1 {
					t.Fatal("held A must already answer and end its live run", aResult)
				}

				var next *SubmissionHandle
				switch mode {
				case "reject", "input":
					input := Input{Content: "B"}
					if mode == "reject" {
						input.WhenBusy = "reject"
					}
					next, err = c.Submit(bg, input)
				case "passive-write":
					// Model contribution deliberately prevents the busy-write fast path.
					next, err = c.Submit(bg, Input{Type: "write", Entry: &Entry{Kind: "app.boundary.note", HasData: true, Data: JSON{"text": "W"}, Model: []MessageReceipt{userReceipt("W")}}})
				case "reset":
					err = c.Reset(bg, "fresh")
				}
				if err != nil {
					t.Fatal("fresh admission must not be busy behind answered held A", err)
				}
				placed := snapshot()
				requireHeld(placed)
				live := mainAbortFailureLive(t, placed, c.ID())
				var nextOwner ID
				if mode == "reject" || mode == "input" {
					status := publicSubmission(placed.Submissions[next.ID()])
					run := generationObject(live["run"])
					nextOwner = submissionEntryID(run["task"])
					entry := placed.Entries[status.Entry]
					want, err := dtoObject(JSON{"task": nextOwner, "inputs": []any{next.ID()}, "first": entry.ID}, h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					if status.Status != "placed" || entry.ID == 0 || entry.Conversation != c.ID() || entry.ByTask != 0 || nextOwner == 0 || nextOwner == ownerID || placed.Tasks[nextOwner].Kind != "pi.generation" || !equalJSONValue(run, want) {
						t.Fatal("fresh B must commit input and a distinct live run while A's child is unfinished", status, entry, run)
					}
					awaitTaskSignal(t, bEntered)
				} else {
					if len(generationObject(live["run"])) != 0 || requests.Load() != 1 {
						t.Fatal("passive admission must not start a provider run", live, requests.Load())
					}
					if mode == "passive-write" {
						status := publicSubmission(placed.Submissions[next.ID()])
						entry := placed.Entries[status.Entry]
						if status.Status != "done" || entry.ID == 0 || entry.Kind != "app.boundary.note" || entry.Conversation != c.ID() || entry.ByTask != 0 || generationObject(entry.Data)["text"] != "W" || len(entry.Model) != 1 || entry.Model[0].Role != goai.RoleUser || entry.Model[0].Content[0].Text != "W" {
							t.Fatal("model-contributing passive write stranded behind held A", status, entry)
						}
					} else {
						view, err := c.ContextView(bg, 0)
						if err != nil || view.Head == nil || view.Head.Kind != "pi.reset" || view.Head.ID == 0 || view.Head.Head != view.Head.ID || view.Head.ByTask != 0 || len(view.Head.Model) != 1 || view.Head.Model[0].Content[0].Text != "fresh" {
							t.Fatal("reset stranded behind held A", view, err)
						}
					}
				}
				for _, doc := range placed.Documents {
					if !doc.Retired && doc.Owner == c.ID() && doc.Kind == "pi.inbox" {
						if items, ok := doc.Value["items"].([]any); !ok || len(items) != 0 {
							t.Fatal("fresh admission left work queued behind held A", doc.Value)
						}
					}
				}
				releaseTaskGate(releaseChild)
				waitPublicTask(t, h, childID)
				if owner := waitPublicTask(t, h, ownerID); owner.State.Outcome == nil || owner.State.Outcome.Status != "completed" {
					t.Fatal(owner)
				}
				finalized := snapshot()
				if len(finalized.Entries) != len(placed.Entries) || !equalJSONValue(generationObject(mainAbortFailureLive(t, finalized, c.ID())["run"]), generationObject(live["run"])) {
					t.Fatal("late held-A finalization repeated a boundary or altered the later live run")
				}
				if nextOwner != 0 {
					releaseTaskGate(releaseB)
					settled := waitSubmission(t, next)
					if settled.Task.ID != nextOwner || settled.Record.Status != "done" || settled.Record.Answer == 0 || settled.Record.Entry != publicSubmission(placed.Submissions[next.ID()]).Entry {
						t.Fatal("B completion lost its own run/input", settled)
					}
					waitPublicTask(t, h, nextOwner)
				}
				if finalA := waitSubmission(t, a); finalA.Record.Entry != aResult.Record.Entry || finalA.Record.Answer != aResult.Record.Answer || finalA.Task.ID != ownerID || finalA.Record.Status != "done" {
					t.Fatal("late held completion changed A's answer", finalA)
				}
				ctx, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				if err := c.WaitForIdle(ctx); err != nil {
					t.Fatal(err)
				}
				wantRequests := int64(1)
				if nextOwner != 0 {
					wantRequests = 2
				}
				if requests.Load() != wantRequests || hookCalls.Load() != wantRequests || len(generationObject(mainAbortFailureLive(t, snapshot(), c.ID())["run"])) != 0 {
					t.Fatal("unexpected run after owned completion", requests.Load(), hookCalls.Load())
				}
			})
		})
	}
}
