package durable

import (
	"context"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source-derived regressions, not translated upstream test declarations:
// official v1.5.0 f9a26365568127461c47a07a37dcb42c140a3ac5,
// packages/pi-durable/src/harness/generation.ts failNoModel/endFailedRun/startRun
// and live.ts endRun. failNoModel's message is runFailure.detail, while its
// explicit submission settlement deliberately omits detail. Native preparation
// can fail before prepareRequest creates live.run; that must not lose the run's
// own task, placed inputs, first entry, or reason.
func mainRequirePreparationNoModel(t *testing.T, h *Harness, c *ConversationHandle, sub *SubmissionHandle, result Settlement, detail string, held bool) Snapshot {
	t.Helper()
	if result.Record.ID != sub.ID() || result.Record.Status != "unanswered" || result.Record.Reason != "no_model" || result.Record.Detail != nil && result.Record.Detail.Present || result.Message != nil {
		t.Fatalf("no-model settlement = %+v, message = %+v; want unanswered/no_model without detail or synthetic answer", result.Record, result.Message)
	}
	if result.Task.ID == 0 || result.Task.Conversation != c.ID() || result.Record.Entry == 0 {
		t.Fatal("no-model run lacks its own task/placed entry", result)
	}
	var cp generationCheckpoint
	if err := fromObject(result.Task.Checkpoint, &cp, h.session.limits); err != nil {
		t.Fatal(err)
	}
	if cp.Submission != sub.ID() || cp.InputEntry != result.Record.Entry {
		t.Fatal("no-model failure must identify its own placed input", cp, result.Record)
	}
	task, err := CanonicalTask(result.Task, h.session.limits)
	if err != nil {
		t.Fatal(err)
	}
	if task.State.Outcome == nil || task.State.Outcome.Status != "failed" || task.State.Outcome.Error == nil || task.State.Outcome.Error.Message != detail {
		t.Fatal("no-model task outcome", task)
	}
	state, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	if stored := publicSubmission(state.Submissions[sub.ID()]); stored.Entry != result.Record.Entry || stored.Status != "unanswered" || stored.Reason != "no_model" {
		t.Fatal("no-model placement/settlement not persisted", stored)
	}
	// Native adapters: numeric IDs and the explicitly permitted private
	// failedRun queue gate. This existing helper compares the entire live value,
	// including the complete source runFailure, with no arbitrary fields hidden.
	mainRequireRunFailure(t, state, c.ID(), result.Task.ID, sub.ID(), result.Record.Entry, "no_model", detail, held)
	return state
}

func TestMainRunFailureNoModelBeforeRequest(t *testing.T) {
	for _, configured := range []bool{false, true} {
		name := "unconfigured"
		if configured {
			name = "unavailable"
		}
		t.Run(name, func(t *testing.T) {
			for _, policy := range []string{"keep", "withdraw", "continue"} {
				t.Run(policy, func(t *testing.T) {
					utf16Backends(t, func(t *testing.T, b backend) {
						var calls, resolutions atomic.Int64
						_, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
							calls.Add(1)
							ch := make(chan goai.Event, 1)
							ch <- terminal("unexpected answer")
							close(ch)
							return ch
						})
						entered, release := make(chan struct{}), make(chan struct{})
						options.Settings = &HarnessSettings{AfterFailedRun: policy}
						options.Models = func(goai.Provider, string) *goai.Model {
							if resolutions.Add(1) == 1 {
								close(entered)
								<-release
							}
							return nil
						}
						h := openHarness(t, b.store, options)
						// Release the resolver before Harness cleanup, even on failure.
						cleanupTaskGates(t, release)
						ref, detail := ModelRef{}, "No model is configured"
						if configured {
							ref = ModelRef{Provider: goai.ProviderOpenAI, ID: "missing"}
							detail = "Model openai/missing is not available"
						}
						c := root(t, h, ref)
						sub, err := c.Submit(bg, Input{Content: "no-model input"})
						if err != nil {
							t.Fatal(err)
						}
						var write *SubmissionHandle
						if configured {
							// The real resolver is the gate: queue a passive write while
							// preparation is in flight, not by fabricating live.run.
							awaitTaskSignal(t, entered)
							write, err = c.Submit(bg, Input{Type: "write", Content: "passive"})
							if err != nil {
								t.Fatal(err)
							}
							statusFailed140(t, write, "queued", "")
						}
						releaseTaskGate(release)
						result := waitSubmission(t, sub)
						idleFailed140(t, h)
						state := mainRequirePreparationNoModel(t, h, c, sub, result, detail, policy == "keep")
						if calls.Load() != 0 || !configured && resolutions.Load() != 0 || configured && resolutions.Load() != 1 {
							t.Error("no-model path invoked provider or unexpected resolver", calls.Load(), resolutions.Load())
						}
						users := 0
						for _, entry := range state.Entries {
							if entry.Conversation != c.ID() {
								continue
							}
							if entry.Value["role"] == "assistant" {
								t.Error("no-model invented assistant", entry)
							}
							if entry.Value["role"] == "user" {
								users++
							}
						}
						wantUsers := 1
						if write != nil {
							wantStatus := "queued"
							if policy == "continue" {
								wantStatus, wantUsers = "done", 2
							}
							statusFailed140(t, write, wantStatus, "")
							placed := submissionEntryID(state.Submissions[write.ID()].Value["placedEntry"])
							if (placed != 0) != (policy == "continue") || placed == result.Record.Entry {
								t.Error("passive write placement changed no-model boundary", placed, result.Record.Entry)
							}
						}
						if users != wantUsers {
							t.Errorf("persisted user entries = %d; want %d", users, wantUsers)
						}
					})
				})
			}
		})
	}
}

// Source startRun clears an older failure for fresh input; failNoModel/endRun
// then records the newest run. Native failedRun must not preserve stale public
// metadata merely because the new run fails before prepareRequest's intent.
func TestMainRunFailureNoModelReplacesPreviousModelError(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		ref, options, calls, entered, release := failedRunProvider140(t)
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		c := root(t, h, ref)
		first, err := c.Submit(bg, Input{Content: "model-error input"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		releaseTaskGate(release)
		old := waitSubmission(t, first)
		idleFailed140(t, h)
		statusFailed140(t, first, "unanswered", "model_error")
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		mainRequireRunFailure(t, before, c.ID(), old.Task.ID, first.ID(), old.Record.Entry, "model_error", "Model response ended with stop reason error", true)
		if err := c.ConfigurePatch(bg, AgentPatch{Clear: []string{"model"}}); err != nil {
			t.Fatal(err)
		}
		fresh, err := c.Submit(bg, Input{Content: "fresh unconfigured input", WhenBusy: "reject"})
		if err != nil {
			t.Fatal("held failed run must admit fresh input", err)
		}
		result := waitSubmission(t, fresh)
		idleFailed140(t, h)
		if result.Task.ID == old.Task.ID || result.Record.Entry == old.Record.Entry || fresh.ID() == first.ID() {
			t.Fatal("fresh run reused previous failure identity", old, result)
		}
		after := mainRequirePreparationNoModel(t, h, c, fresh, result, "No model is configured", true)
		if calls.Load() != 1 {
			t.Error("removing model must not invoke provider again", calls.Load())
		}
		for id, entry := range after.Entries {
			if entry.Conversation == c.ID() && entry.Value["role"] == "assistant" {
				if _, existed := before.Entries[id]; !existed {
					t.Error("fresh no-model run invented assistant", entry)
				}
			}
		}
	})
}

// Observe the real adopted decision, so a later repair cannot hide placement
// lost by cleanup in the transaction that appends the queued input.
func mainPreparationTerminalPublication(t *testing.T, h *Harness, id ID) func() CommitPublication {
	t.Helper()
	_, observer, err := h.SubscribeCommits(bg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { observer.Stop() })
	frames, ready := make(chan PublicationFrame, 1), make(chan struct{}, 1)
	if err := observer.Start(func(_ context.Context, frame PublicationFrame) error {
		for _, write := range frame.Publication.Tables {
			if write.Submission != nil && write.Submission.ID == id && terminalStatus(write.Submission.Status) {
				select {
				case frames <- frame:
					ready <- struct{}{}
				default:
				}
				break
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return func() CommitPublication {
		t.Helper()
		awaitTaskSignal(t, ready)
		frame := <-frames
		if frame.Snapshot != nil || frame.Publication.Seq == 0 {
			t.Fatal("expected exact no-model decision publication, not a resnapshot", frame)
		}
		return frame.Publication
	}
}

func mainRequirePreparationInputSettlement(t *testing.T, h *Harness, c *ConversationHandle, sub *SubmissionHandle, result Settlement, state Snapshot, publication CommitPublication, text string, newlyPlaced bool) {
	t.Helper()
	r := result.Record
	if r.ID != sub.ID() || r.Conversation != c.ID() || r.Type != "input" || r.RequestID != "" || r.Status != "unanswered" || r.Reason != "no_model" || r.Detail != nil || r.Entry == 0 || r.Answer != 0 || result.Message != nil || result.Task.ID == 0 || result.Task.Conversation != c.ID() {
		t.Fatalf("expected exact unanswered/no_model without detail/answer, with own entry: %+v", result)
	}
	want, err := dtoObject(JSON{"errorCode": "no_model", "taskId": result.Task.ID, "placedEntry": r.Entry}, h.session.limits)
	if err != nil {
		t.Fatal(err)
	}
	stored := state.Submissions[sub.ID()]
	if result.Submission.Status != "failed" || stored.Status != "failed" || !equalJSONValue(r.Value, want) || !equalJSONValue(result.Submission.Value, want) || !equalJSONValue(stored.Value, want) {
		t.Fatal("settlement and stored envelope must retain only their own placed entry and shared failure", result, stored, want)
	}
	input, err := inputReceipt(text, nil, h.session.limits)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := dtoObject(input, h.session.limits)
	if err != nil {
		t.Fatal(err)
	}
	entry, exists := state.Entries[r.Entry]
	if !exists || entry.Conversation != c.ID() || entry.Kind != "message" || !equalJSONValue(entry.Value, wire) {
		t.Fatal("settlement must name its exact persisted user input", entry, wire)
	}
	var final *Submission
	appended := false
	for _, write := range publication.Tables {
		if write.Submission != nil && write.Submission.ID == sub.ID() {
			final = write.Submission
		}
		if write.Op == "append-entry" && write.Entry != nil && write.Entry.ID == r.Entry {
			appended = write.Entry.ByTask == result.Task.ID && equalJSONValue(write.Entry.Value, wire)
		}
	}
	if final == nil || final.Status != "failed" || !equalJSONValue(final.Value, want) || newlyPlaced && !appended {
		t.Fatal("same decision must append actual input and retain its entry in terminal submission", publication, r)
	}
}

// Source-derived final-review regression: unlike an initially admitted input,
// this follow-up has no entry until finishGenerationNoModel's own transaction.
// A real successful provider run precedes it; no live/checkpoint data is seeded.
func TestMainRunFailureQueuedFollowUpNoModelRetainsDecisionPlacement(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		providerEntered, providerRelease := make(chan struct{}), make(chan struct{})
		resolverEntered, resolverRelease := make(chan struct{}), make(chan struct{})
		var calls, missingResolutions atomic.Int64
		var missing atomic.Bool
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			n := calls.Add(1)
			ch := make(chan goai.Event, 1)
			go func() {
				defer close(ch)
				if n != 1 {
					t.Errorf("unexpected provider call %d", n)
					ch <- terminal("unexpected")
					return
				}
				close(providerEntered)
				select {
				case <-providerRelease:
					ch <- terminal("earlier answer")
				case <-ctx.Done():
				}
			}()
			return ch
		})
		resolve := options.Models
		options.Models = func(provider goai.Provider, id string) *goai.Model {
			if !missing.Load() {
				return resolve(provider, id)
			}
			if missingResolutions.Add(1) == 1 {
				close(resolverEntered)
			}
			<-resolverRelease
			return nil
		}
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, providerRelease, resolverRelease)
		c := root(t, h, ref)
		a, err := c.Submit(bg, Input{Content: "earlier input"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, providerEntered)
		followup, err := c.Submit(bg, Input{Content: "queued follow-up", Type: "follow-up"})
		if err != nil {
			t.Fatal(err)
		}
		queued, err := followup.Status(bg)
		if err != nil || queued.Status != "queued" || queued.Entry != 0 {
			t.Fatal("follow-up must have no placed entry while earlier run is active", queued, err)
		}
		missing.Store(true)
		releaseTaskGate(providerRelease)
		earlier := waitSubmission(t, a)
		if earlier.Record.Status != "done" || earlier.Record.Answer == 0 {
			t.Fatal("earlier run must complete successfully", earlier)
		}
		awaitTaskSignal(t, resolverEntered)
		queued, err = followup.Status(bg)
		if err != nil || queued.Status != "queued" || queued.Entry != 0 {
			t.Fatal("resolver must fail before follow-up placement", queued, err)
		}
		decision := mainPreparationTerminalPublication(t, h, followup.ID())
		releaseTaskGate(resolverRelease)
		result := waitSubmission(t, followup)
		idleFailed140(t, h)
		state := mainRequirePreparationNoModel(t, h, c, followup, result, "Model openai/durable-test is not available", true)
		mainRequirePreparationInputSettlement(t, h, c, followup, result, state, decision(), "queued follow-up", true)
		if result.Task.ID == earlier.Task.ID || result.Record.Entry == earlier.Record.Entry || result.Record.Entry == earlier.Record.Answer || calls.Load() != 1 || missingResolutions.Load() != 1 {
			t.Fatal("follow-up must fail in its own run without provider dispatch/replay", earlier, result, calls.Load(), missingResolutions.Load())
		}
	})
}

// Source-derived final-review regression, f9a26365 generation.ts:600-654:
// failNoModel ends the existing A run before the final boundary selects B/C.
// Neither queued steer belongs to A's failure. Native request preparation groups
// B+C for steering=all, or starts separate B then C runs for one-at-a-time;
// only the immediate successor retains A's failure. Resolver/provider gates
// observe real decisions and ownership, without seeding live/checkpoint data.
func TestMainRunFailureNoModelAttachedSteersAndRemainingSuccessor(t *testing.T) {
	for _, mode := range []string{"all", "one-at-a-time"} {
		t.Run(mode, func(t *testing.T) {
			utf16Backends(t, func(t *testing.T, b backend) {
				resolverEntered := []chan struct{}{make(chan struct{}), make(chan struct{})}
				resolverRelease := []chan struct{}{make(chan struct{}), make(chan struct{})}
				providerEntered := []chan struct{}{make(chan struct{}), make(chan struct{})}
				providerRelease := []chan struct{}{make(chan struct{}), make(chan struct{})}
				var resolutions, calls atomic.Int64
				ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					n := calls.Add(1)
					ch := make(chan goai.Event, 1)
					go func() {
						defer close(ch)
						if n > 2 || mode == "all" && n != 1 {
							t.Errorf("unexpected provider call %d in %s", n, mode)
							ch <- terminal("unexpected")
							return
						}
						var users []string
						for _, message := range input.Messages {
							if message.Role == goai.RoleUser {
								for _, block := range message.Content {
									users = append(users, block.Text)
								}
							}
						}
						want := []string{"A", "B", "C"}
						if mode == "one-at-a-time" && n == 1 {
							want = want[:2]
						}
						if len(users) != len(want) {
							t.Errorf("provider users = %v; want %v", users, want)
						} else {
							for i := range want {
								if users[i] != want[i] {
									t.Errorf("provider users = %v; want %v", users, want)
								}
							}
						}
						close(providerEntered[n-1])
						select {
						case <-providerRelease[n-1]:
							answer := "B answer"
							if n == 2 {
								answer = "C answer"
							}
							ch <- terminal(answer)
						case <-ctx.Done():
						}
					}()
					return ch
				})
				resolve := options.Models
				options.Models = func(provider goai.Provider, id string) *goai.Model {
					n := resolutions.Add(1)
					if n <= 2 {
						close(resolverEntered[n-1])
						<-resolverRelease[n-1]
					}
					if n == 1 {
						return nil
					}
					return resolve(provider, id)
				}
				options.Settings = &HarnessSettings{AfterFailedRun: "continue", SteeringMode: mode, FollowUpMode: "one-at-a-time"}
				h := openHarness(t, b.store, options)
				cleanupTaskGates(t, resolverRelease[0], resolverRelease[1], providerRelease[0], providerRelease[1])
				c := root(t, h, ref)
				primary, err := c.Submit(bg, Input{Content: "A"})
				if err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, resolverEntered[0])
				inputs := []*SubmissionHandle{primary}
				for _, text := range []string{"B", "C"} {
					sub, err := c.Submit(bg, Input{Type: "steer", Content: text})
					if err != nil {
						t.Fatal(err)
					}
					status, err := sub.Status(bg)
					if err != nil || status.Status != "queued" || status.Entry != 0 {
						t.Fatal("steers must be genuinely queued before no-model decision", status, err)
					}
					inputs = append(inputs, sub)
				}
				decision := mainPreparationTerminalPublication(t, h, primary.ID())
				releaseTaskGate(resolverRelease[0])
				failed := waitSubmission(t, primary)
				var cp generationCheckpoint
				if err := fromObject(failed.Task.Checkpoint, &cp, h.session.limits); err != nil {
					t.Fatal(err)
				}
				if cp.Submission != primary.ID() || cp.InputEntry != failed.Record.Entry || cp.AfterFailedRun != "continue" || cp.FailureCut != inputs[1].ID() || cp.Phase != "terminal" || len(cp.Steered) != 0 {
					t.Fatal("no-model must end A alone and reserve exactly B, not attach queued B/C", cp)
				}
				awaitTaskSignal(t, resolverEntered[1])
				publication := decision()
				state := mainRequirePreparationNoModel(t, h, c, primary, failed, "Model openai/durable-test is not available", false)
				mainRequirePreparationInputSettlement(t, h, c, primary, failed, state, publication, "A", false)
				for _, sub := range inputs[1:] {
					status := publicSubmission(state.Submissions[sub.ID()])
					if status.Status != "queued" || status.Entry != 0 || status.Answer != 0 {
						t.Fatal("B/C must remain unplaced before successor request preparation", status)
					}
					for _, write := range publication.Tables {
						if write.Submission != nil && write.Submission.ID == sub.ID() {
							t.Fatal("A's deciding commit must not place or settle B/C", write)
						}
					}
				}
				if calls.Load() != 0 || resolutions.Load() != 2 {
					t.Fatal("no-model must not dispatch provider; B must be gated before placement", calls.Load(), resolutions.Load())
				}
				failure := JSON{"taskId": failed.Task.ID, "inputs": []any{primary.ID()}, "first": failed.Record.Entry, "reason": "no_model", "detail": "Model openai/durable-test is not available"}
				texts := map[ID]string{primary.ID(): "A", inputs[1].ID(): "B", inputs[2].ID(): "C"}
				entries := map[ID]ID{primary.ID(): failed.Record.Entry}
				seen := map[ID]bool{failed.Record.Entry: true}
				active := func(group []*SubmissionHandle, keepFailure bool) ID {
					t.Helper()
					state, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					var owner ID
					var next generationCheckpoint
					for _, task := range state.Tasks {
						if task.Kind != "pi.generation" || task.Conversation != c.ID() || terminalStatus(task.Status) {
							continue
						}
						var candidate generationCheckpoint
						if err := fromObject(task.Checkpoint, &candidate, h.session.limits); err != nil {
							t.Fatal(err)
						}
						if candidate.Submission == group[0].ID() {
							if owner != 0 || task.ID == failed.Task.ID {
								t.Fatal("successor reused/duplicated failure owner", task)
							}
							owner, next = task.ID, candidate
						}
					}
					if owner == 0 || len(next.Steered) != len(group)-1 {
						t.Fatal("missing actual successor or wrong grouping", owner, next)
					}
					inputIDs := []any{}
					for i, sub := range group {
						status := publicSubmission(state.Submissions[sub.ID()])
						input, err := inputReceipt(texts[sub.ID()], nil, h.session.limits)
						if err != nil {
							t.Fatal(err)
						}
						wire, err := dtoObject(input, h.session.limits)
						if err != nil {
							t.Fatal(err)
						}
						entry, exists := state.Entries[status.Entry]
						if status.Status != "placed" || status.Entry == 0 || status.Answer != 0 || seen[status.Entry] || !exists || entry.Conversation != c.ID() || entry.Kind != "message" || !equalJSONValue(entry.Value, wire) || i == 0 && next.InputEntry != status.Entry || i > 0 && next.Steered[i-1] != sub.ID() {
							t.Fatal("successor input must own its exact distinct placed entry", status, entry, next)
						}
						entries[sub.ID()], seen[status.Entry] = status.Entry, true
						inputIDs = append(inputIDs, sub.ID())
					}
					want := JSON{"run": JSON{"task": owner, "inputs": inputIDs, "first": entries[group[0].ID()]}, "generation": JSON{"attempt": 1}}
					if keepFailure {
						want["runFailure"] = failure
					}
					wire, err := dtoObject(want, h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					liveCount := 0
					for _, doc := range state.Documents {
						if !doc.Retired && doc.Kind == "pi.live" && doc.Owner == c.ID() {
							liveCount++
							if !equalJSONValue(doc.Value, wire) {
								t.Fatalf("exact successor projection: got %v; want %v", doc.Value, wire)
							}
						}
					}
					if liveCount != 1 {
						t.Fatal("expected one actual live document", liveCount)
					}
					return owner
				}
				settled := func(sub *SubmissionHandle, owner ID, publication CommitPublication, answer string) Settlement {
					t.Helper()
					result := waitSubmission(t, sub)
					r := result.Record
					state, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					entry := state.Entries[r.Answer]
					want, err := dtoObject(JSON{"taskId": owner, "placedEntry": entries[sub.ID()], "answerEntry": r.Answer, "message": entry.Value}, h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					if r.ID != sub.ID() || r.Conversation != c.ID() || r.Type != "input" || r.RequestID != "" || r.Status != "done" || r.Reason != "" || r.Detail != nil || r.Entry != entries[sub.ID()] || r.Answer == 0 || seen[r.Answer] || result.Task.ID != owner || result.Task.Conversation != c.ID() || result.Message == nil || len(result.Message.Content) != 1 || result.Message.Content[0].Text != answer || entry.Conversation != c.ID() || entry.Value["role"] != "assistant" || entry.ByTask != owner || result.Submission.Status != "done" || !equalJSONValue(r.Value, want) || !equalJSONValue(result.Submission.Value, want) || !equalJSONValue(state.Submissions[sub.ID()].Value, want) {
						t.Fatal("successor must settle uniformly with its own entry and actual shared answer", result, entry, want)
					}
					var final *Submission
					for _, write := range publication.Tables {
						if write.Submission != nil && write.Submission.ID == sub.ID() {
							final = write.Submission
						}
					}
					if final == nil || final.Status != "done" || !equalJSONValue(final.Value, want) {
						t.Fatal("same successor decision must retain each input's own placement", publication, want)
					}
					return result
				}
				releaseTaskGate(resolverRelease[1])
				awaitTaskSignal(t, providerEntered[0])
				group := inputs[1:]
				if mode == "one-at-a-time" {
					group = group[:1]
					status, err := inputs[2].Status(bg)
					if err != nil || status.Status != "queued" || status.Entry != 0 {
						t.Fatal("one-at-a-time must leave C queued behind B", status, err)
					}
				}
				owner := active(group, true)
				answeredDecision := mainPreparationTerminalPublication(t, h, inputs[1].ID())
				releaseTaskGate(providerRelease[0])
				answeredPublication := answeredDecision()
				answered := settled(inputs[1], owner, answeredPublication, "B answer")
				wantCalls := int64(1)
				if mode == "all" {
					attached := settled(inputs[2], owner, answeredPublication, "B answer")
					if attached.Record.Answer != answered.Record.Answer {
						t.Fatal("B+C must share one successor settlement/answer", answered, attached)
					}
				} else {
					awaitTaskSignal(t, providerEntered[1])
					cOwner := active(inputs[2:], false)
					if cOwner == owner {
						t.Fatal("one-at-a-time C reused B's run")
					}
					cDecision := mainPreparationTerminalPublication(t, h, inputs[2].ID())
					releaseTaskGate(providerRelease[1])
					last := settled(inputs[2], cOwner, cDecision(), "C answer")
					if last.Record.Answer == answered.Record.Answer {
						t.Fatal("separate C run reused B's answer")
					}
					wantCalls = 2
				}
				idleFailed140(t, h)
				// Successful native runs resolve at preparation and dispatch rehydration.
				if calls.Load() != wantCalls || resolutions.Load() != 2*wantCalls+1 {
					t.Fatal("no-model/attached placeholders must not dispatch or resolve again", calls.Load(), resolutions.Load(), wantCalls)
				}
			})
		})
	}
}
