package durable

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source-derived regressions, not translated upstream test declarations:
// official v1.5.0 f9a26365568127461c47a07a37dcb42c140a3ac5,
// packages/pi-durable/src/harness/generation.ts failNoModel/endFailedRun/startRun
// and live.ts endRun. failNoModel's message is runFailure.detail, while its
// explicit submission settlement deliberately omits detail. Preparation can
// fail before a request intent; that must not lose the run's own task, placed
// inputs, first entry, or reason.
func mainRequirePreparationNoModel(t *testing.T, h *Harness, c *ConversationHandle, sub *SubmissionHandle, result Settlement, detail string, held bool) Snapshot {
	t.Helper()
	state := mainRequirePreparationNoModelState(t, h, c, sub, result, detail)
	// Compare the entire source live value, including runFailure. Queue
	// retention is observable in Inbox, not a native failedRun flag.
	mainRequireRunFailure(t, state, c.ID(), result.Task.ID, sub.ID(), result.Record.Entry, "no_model", detail, held)
	return state
}

// A boundary may already have started the successor in the failure's commit.
// Check the failed run independently; its caller must assert the complete live
// value appropriate to that boundary, rather than expecting an idle document.
func mainRequirePreparationNoModelState(t *testing.T, h *Harness, c *ConversationHandle, sub *SubmissionHandle, result Settlement, detail string) Snapshot {
	t.Helper()
	if result.Record.ID != sub.ID() || result.Record.Status != "unanswered" || result.Record.Reason != "no_model" || result.Record.Detail != nil && result.Record.Detail.Present || result.Message != nil {
		t.Fatalf("no-model settlement = %+v, message = %+v; want unanswered/no_model without detail or synthetic answer", result.Record, result.Message)
	}
	if result.Task.ID == 0 || result.Task.Conversation != c.ID() || result.Record.Entry == 0 {
		t.Fatal("no-model run lacks its own task/placed entry", result)
	}
	task, err := CanonicalTask(result.Task, h.session.limits)
	if err != nil {
		t.Fatal(err)
	}
	if task.Kind != "pi.generation" || task.Version != 2 || task.Input == nil || !task.Input.Present || !equalJSONValue(task.Input.Value, JSON{}) || task.State.Status != "terminal" || task.State.Checkpoint != nil || task.State.Outcome == nil || task.State.Outcome.Status != "failed" || task.State.Outcome.Error == nil || task.State.Outcome.Error.Message != detail {
		t.Fatal("no-model task outcome", task)
	}
	state, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	if stored := publicSubmission(state.Submissions[sub.ID()]); stored.Entry != result.Record.Entry || stored.Status != "unanswered" || stored.Reason != "no_model" {
		t.Fatal("no-model placement/settlement not persisted", stored)
	}
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
					sqliteStore(t, func(t *testing.T, b sqliteFixture) {
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
							if legacyEntryValue(entry)["role"] == "assistant" {
								t.Error("no-model invented assistant", entry)
							}
							if legacyEntryValue(entry)["role"] == "user" {
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
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
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
			if entry.Conversation == c.ID() && legacyEntryValue(entry)["role"] == "assistant" {
				if _, existed := before.Entries[id]; !existed {
					t.Error("fresh no-model run invented assistant", entry)
				}
			}
		}
	})
}

// Observe the real adopted decision, so a later repair cannot hide a missing
// atomic successor start or placement lost in the terminal settlement.
func mainPreparationTerminalPublication(t *testing.T, h *Harness, id ID) func() sessionPublication {
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
	return func() sessionPublication {
		t.Helper()
		awaitTaskSignal(t, ready)
		frame := <-frames
		if frame.Snapshot != nil || frame.Publication.Seq == 0 {
			t.Fatal("expected exact terminal decision publication, not a resnapshot", frame)
		}
		return frame.Publication
	}
}

// Retain actual public publications by sequence, with a deterministic delivery
// signal. This never reconstructs a publication from a later snapshot.
func mainPreparationSourcePublications(t *testing.T, h *Harness) func(uint64) CommitPublication {
	t.Helper()
	var mu sync.Mutex
	frames := map[uint64]CommitPublication{}
	waiters := map[uint64]chan struct{}{}
	guard, err := h.ObserveCommits(CommitObserver{Advance: func(_ context.Context, frame CommitPublication) error {
		mu.Lock()
		defer mu.Unlock()
		frames[frame.Seq] = frame
		if waiter := waiters[frame.Seq]; waiter != nil {
			close(waiter)
			delete(waiters, frame.Seq)
		}
		return nil
	}, End: func(WatchEnd) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(guard.Detach)
	return func(seq uint64) CommitPublication {
		t.Helper()
		mu.Lock()
		if frame, found := frames[seq]; found {
			mu.Unlock()
			return frame
		}
		waiter := make(chan struct{})
		waiters[seq] = waiter
		mu.Unlock()
		awaitTaskSignal(t, waiter)
		mu.Lock()
		defer mu.Unlock()
		return frames[seq]
	}
}

func mainPreparationPublishedRecord(t *testing.T, frame CommitPublication, kind string, id ID) ReadonlyValue {
	t.Helper()
	for i := range frame.Changes.Len() {
		change, _ := frame.Changes.Index(i)
		if immutableViewMember(t, change, "type").String() != kind {
			continue
		}
		value := immutableViewMember(t, change, "value")
		if submissionEntryID(immutableViewMember(t, value, "id").Number()) == id {
			return value
		}
	}
	t.Fatalf("missing %s %d in deciding public publication %d", kind, id, frame.Seq)
	return nil
}

func mainRequirePreparationInputSettlement(t *testing.T, h *Harness, c *ConversationHandle, sub *SubmissionHandle, result Settlement, state Snapshot, publication sessionPublication, source CommitPublication, text string) {
	t.Helper()
	r := result.Record
	wireRecord, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var expected JSON
	if err := json.Unmarshal(wireRecord, &expected); err != nil {
		t.Fatal(err)
	}
	if source.Seq != publication.Seq || !equalJSONValue(mainPreparationPublishedRecord(t, source, "submission", sub.ID()).Copy(), expected) {
		t.Fatal("exact source deciding settlement record", source.Seq, publication.Seq, expected)
	}
	if r.ID != sub.ID() || r.Conversation != c.ID() || r.Type != "input" || r.RequestID != "" || r.Status != "unanswered" || r.Reason != "no_model" || r.Detail != nil || r.Entry == 0 || r.Answer != 0 || result.Message != nil || result.Task.ID == 0 || result.Task.Conversation != c.ID() {
		t.Fatalf("expected exact unanswered/no_model without detail/answer, with own entry: %+v", result)
	}
	stored, found, err := h.session.Submission(bg, sub.ID())
	if err != nil || !found || !equalJSONValue(stored, r) {
		t.Fatal("exact canonical no-model settlement", stored, r, err)
	}
	input, err := inputReceipt(text, nil, h.session.limits)
	if err != nil {
		t.Fatal(err)
	}
	entry, exists := state.Entries[r.Entry]
	if !exists || len(entry.Model) != 1 {
		t.Fatal("placed input is missing", entry)
	}
	input.Timestamp = entry.Model[0].Timestamp
	wire, err := dtoObject(input, h.session.limits)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || entry.Conversation != c.ID() || !isMessageEntry(entry) || !equalJSONValue(messageEntryValue(entry), wire) {
		t.Fatal("settlement must name its exact persisted user input", entry, wire)
	}
	copies := 0
	for _, entry := range state.Entries {
		if entry.Conversation == c.ID() && isMessageEntry(entry) && equalJSONValue(messageEntryValue(entry), wire) {
			copies++
		}
	}
	if copies != 1 {
		t.Fatal("terminal settlement must not duplicate its already placed user input", copies, r)
	}
	var final *Submission
	for _, write := range publication.Tables {
		if write.Submission != nil && write.Submission.ID == sub.ID() {
			final = write.Submission
		}
		if write.Op == "append-entry" && write.Entry != nil && (write.Entry.ID == r.Entry || write.Entry.Conversation == c.ID() && equalJSONValue(messageEntryValue(*write.Entry), wire)) {
			t.Fatal("terminal decision must reuse its input placement, not append it again", publication, r)
		}
	}
	if final == nil || final.Status != "failed" || final.Value["errorCode"] != "no_model" || submissionEntryID(final.Value["placedEntry"]) != r.Entry {
		t.Fatal("terminal decision must retain the start decision's exact placed entry", publication, r)
	}
}

// Assert startRun/applyBoundary at the earlier run's deciding commit, before the
// successor's resolver can return. Both the atomic publication and the gated
// snapshot must contain exactly the selected inputs and the same live owner.
func mainRequirePreparationStart(t *testing.T, h *Harness, c *ConversationHandle, state Snapshot, publication sessionPublication, source CommitPublication, group []*SubmissionHandle, texts map[ID]string, failure JSON) (ID, map[ID]ID) {
	t.Helper()
	liveValue := mainEligibilityDoc(t, state, c.ID(), "pi.live").Value
	run := generationObject(liveValue["run"])
	owner := submissionEntryID(run["taskId"])
	next, found, err := h.Task(bg, owner)
	if err != nil || !found || next.Kind != "pi.generation" || next.Version != 2 || next.Owner != 0 || next.Conversation != c.ID() || next.State.Status != "running" || !equalJSONValue(next.State.Checkpoint, JSON{"phase": "prepare", "attempt": 1}) || next.Input == nil || !next.Input.Present || !equalJSONValue(next.Input.Value, JSON{}) {
		t.Fatal("successor must own canonical prepare before model resolution", next, err)
	}
	if source.Seq != publication.Seq {
		t.Fatal("source/native deciding sequence differs", source.Seq, publication.Seq)
	}
	created := mainPreparationPublishedRecord(t, source, "task", owner)
	if immutableViewMember(t, created, "kind").String() != "pi.generation" || immutableViewMember(t, created, "version").Number().String() != "2" || !equalJSONValue(immutableViewMember(t, created, "input").Copy(), JSON{}) || !equalJSONValue(immutableViewMember(t, created, "state").Copy(), JSON{"status": "pending", "checkpoint": JSON{"phase": "prepare", "attempt": 1}}) {
		t.Fatal("exact fresh successor in deciding source publication", created.Copy())
	}
	entries, seen := map[ID]ID{}, map[ID]bool{}
	inputIDs := []any{}
	for _, sub := range group {
		status := publicSubmission(state.Submissions[sub.ID()])
		input, err := inputReceipt(texts[sub.ID()], nil, h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		entry, exists := state.Entries[status.Entry]
		if !exists || len(entry.Model) != 1 {
			t.Fatal("placed input is missing", entry)
		}
		input.Timestamp = entry.Model[0].Timestamp
		wire, err := dtoObject(input, h.session.limits)
		if err != nil {
			t.Fatal(err)
		}

		if status.Status != "placed" || status.Entry == 0 || status.Answer != 0 || seen[status.Entry] || !exists || entry.Conversation != c.ID() || !isMessageEntry(entry) || !equalJSONValue(messageEntryValue(entry), wire) {
			t.Fatal("successor must already own its distinct placed input before resolution", status, entry, next)
		}
		copies, appends, placements := 0, 0, 0
		for _, candidate := range state.Entries {
			if candidate.Conversation == c.ID() && isMessageEntry(candidate) && equalJSONValue(messageEntryValue(candidate), wire) {
				copies++
			}
		}
		for _, write := range publication.Tables {
			if write.Op == "append-entry" && write.Entry != nil && write.Entry.ID == status.Entry {
				appends++
				if write.Entry.Conversation != c.ID() || !isMessageEntry(*write.Entry) || write.Entry.ByTask != entry.ByTask || !equalJSONValue(messageEntryValue(*write.Entry), wire) {
					t.Fatal("start publication must append the exact persisted successor input", write, entry)
				}
			}
			if write.Submission != nil && write.Submission.ID == sub.ID() {
				placed := publicSubmission(*write.Submission)
				if placed.Status != "placed" || placed.Entry != status.Entry || placed.Answer != 0 || placed.Reason != "" || placed.Detail != nil {
					t.Fatal("earlier decision must place, never settle, successor inputs", write)
				}
				placements++
			}
		}
		if copies != 1 || appends != 1 || placements == 0 {
			t.Fatal("placement must be unique and atomic with the earlier decision", sub.ID(), copies, appends, placements, publication)
		}
		entries[sub.ID()], seen[status.Entry] = status.Entry, true
		inputIDs = append(inputIDs, sub.ID())
	}
	want := JSON{"run": JSON{"taskId": owner, "inputs": inputIDs, "first": entries[group[0].ID()], "turns": 0}}
	if failure != nil {
		want["runFailure"] = failure
	}
	wire, err := dtoObject(want, h.session.limits)
	if err != nil {
		t.Fatal(err)
	}
	liveCount, publishedLive, userAppends := 0, 0, 0
	var publishedOwner *Task
	for _, doc := range state.Documents {
		if !doc.Retired && doc.Kind == "pi.live" && doc.Owner == c.ID() {
			liveCount++
			if !equalJSONValue(doc.Value, wire) {
				t.Fatalf("exact pre-resolver successor projection: got %v; want %v", doc.Value, wire)
			}
		}
	}
	for _, change := range publication.Documents {
		if !change.Record.Retired && change.Record.Kind == "pi.live" && change.Record.Owner == c.ID() {
			publishedLive++
			if !equalJSONValue(change.Value, wire) {
				t.Fatalf("same start commit must publish exact successor and failure: got %v; want %v", change.Value, wire)
			}
		}
	}
	for _, write := range publication.Tables {
		if write.Op == "append-entry" && write.Entry != nil && write.Entry.Conversation == c.ID() && messageEntryValue(*write.Entry)["role"] == "user" {
			userAppends++
		}
		if write.Task != nil && write.Task.ID == owner {
			publishedOwner = write.Task
		}
	}
	if publishedOwner != nil && (publishedOwner.Kind != "pi.generation" || publishedOwner.Conversation != c.ID() || terminalStatus(publishedOwner.Status)) {
		t.Fatal("start publication must establish the actual successor task", publishedOwner)
	}
	if liveCount != 1 || publishedLive != 1 || publishedOwner == nil || userAppends != len(group) {
		t.Fatal("earlier decision must atomically publish one actual start, not a later repair", liveCount, publishedLive, publishedOwner, userAppends, publication)
	}
	return owner, entries
}

// Source-derived final-review regression: the follow-up is unplaced while A is
// active, but A's final boundary places it and starts B before B resolves a model.
// A real successful provider run precedes it; no live/checkpoint data is seeded.
func TestMainRunFailureQueuedFollowUpNoModelRetainsDecisionPlacement(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
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
		sourceAt := mainPreparationSourcePublications(t, h)
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
		startDecision := mainPreparationTerminalPublication(t, h, a.ID())
		missing.Store(true)
		releaseTaskGate(providerRelease)
		earlier := waitSubmission(t, a)
		if earlier.Record.Status != "done" || earlier.Record.Answer == 0 {
			t.Fatal("earlier run must complete successfully", earlier)
		}
		awaitTaskSignal(t, resolverEntered)
		queued, err = followup.Status(bg)
		if err != nil || queued.Status != "placed" || queued.Entry == 0 || queued.Answer != 0 {
			t.Fatal("A's final boundary must place B before B's resolver", queued, err)
		}
		started, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		startPublication := startDecision()
		owner, entries := mainRequirePreparationStart(t, h, c, started, startPublication, sourceAt(startPublication.Seq), []*SubmissionHandle{followup}, map[ID]string{followup.ID(): "queued follow-up"}, nil)
		decision := mainPreparationTerminalPublication(t, h, followup.ID())
		releaseTaskGate(resolverRelease)
		result := waitSubmission(t, followup)
		idleFailed140(t, h)
		state := mainRequirePreparationNoModel(t, h, c, followup, result, "Model openai/durable-test is not available", true)
		terminalPublication := decision()
		mainRequirePreparationInputSettlement(t, h, c, followup, result, state, terminalPublication, sourceAt(terminalPublication.Seq), "queued follow-up")
		if terminalPublication.Seq <= startPublication.Seq || result.Task.ID != owner || result.Record.Entry != entries[followup.ID()] || result.Record.Entry != queued.Entry {
			t.Fatal("no-model settlement must reuse the earlier start's owner and placement", startPublication, terminalPublication, queued, result)
		}
		if result.Task.ID == earlier.Task.ID || result.Record.Entry == earlier.Record.Entry || result.Record.Entry == earlier.Record.Answer || calls.Load() != 1 || missingResolutions.Load() != 1 {
			t.Fatal("follow-up must fail in its own run without provider dispatch/replay", earlier, result, calls.Load(), missingResolutions.Load())
		}
	})
}

// Source-derived final-review regression, f9a26365 generation.ts:600-654:
// failNoModel ends the existing A run before the final boundary selects B/C.
// Neither queued steer belongs to A's failure. A's deciding commit places and
// starts B+C for steering=all, or B alone for one-at-a-time, before B resolves;
// only the immediate successor retains A's failure. Resolver/provider gates
// observe real decisions and ownership, without seeding live/checkpoint data.
func TestMainRunFailureNoModelAttachedSteersAndRemainingSuccessor(t *testing.T) {
	for _, mode := range []string{"all", "one-at-a-time"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
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
				sourceAt := mainPreparationSourcePublications(t, h)
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
				ending, found, err := h.Task(bg, failed.Task.ID)
				if err != nil || !found || ending.State.Status != "terminal" || ending.State.Checkpoint != nil || ending.State.Outcome == nil || ending.State.Outcome.Status != "failed" || failed.Record.Entry == 0 {
					t.Fatal("no-model must end only A's actual canonical run", ending, failed.Record, err)
				}
				awaitTaskSignal(t, resolverEntered[1])
				publication := decision()
				state := mainRequirePreparationNoModelState(t, h, c, primary, failed, "Model openai/durable-test is not available")
				mainRequirePreparationInputSettlement(t, h, c, primary, failed, state, publication, sourceAt(publication.Seq), "A")
				if calls.Load() != 0 || resolutions.Load() != 2 {
					t.Fatal("A must fail without provider dispatch; B's resolver must remain gated", calls.Load(), resolutions.Load())
				}
				failure := JSON{"taskId": failed.Task.ID, "inputs": []any{primary.ID()}, "first": failed.Record.Entry, "reason": "no_model", "detail": "Model openai/durable-test is not available"}
				texts := map[ID]string{primary.ID(): "A", inputs[1].ID(): "B", inputs[2].ID(): "C"}
				group := inputs[1:]
				if mode == "one-at-a-time" {
					group = group[:1]
					status := publicSubmission(state.Submissions[inputs[2].ID()])
					if status.Status != "queued" || status.Entry != 0 || status.Answer != 0 {
						t.Fatal("one-at-a-time must leave C queued during B's resolver", status)
					}
					for _, write := range publication.Tables {
						if write.Submission != nil && write.Submission.ID == inputs[2].ID() {
							t.Fatal("A's boundary must neither place nor settle unselected C", write)
						}
					}
				}
				startedOwner, entries := mainRequirePreparationStart(t, h, c, state, publication, sourceAt(publication.Seq), group, texts, failure)
				if startedOwner == failed.Task.ID {
					t.Fatal("B must not reuse A's failed run", startedOwner)
				}
				entries[primary.ID()] = failed.Record.Entry
				seen := map[ID]bool{}
				for _, entry := range entries {
					if seen[entry] {
						t.Fatal("A/B/C must own distinct placed entries", entries)
					}
					seen[entry] = true
				}
				active := func(group []*SubmissionHandle, keepFailure bool) ID {
					t.Helper()
					state, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					live := mainEligibilityDoc(t, state, c.ID(), "pi.live").Value
					owner := submissionEntryID(generationObject(live["run"])["taskId"])
					next, found, err := h.Task(bg, owner)
					if err != nil || !found || owner == failed.Task.ID || next.Kind != "pi.generation" || next.Version != 2 || next.Owner != 0 || next.State.Status != "running" || taskCheckpointMap(next.State.Checkpoint)["phase"] != "request" {
						t.Fatal("missing actual canonical successor", next, err)
					}
					inputIDs := []any{}
					for _, sub := range group {
						status := publicSubmission(state.Submissions[sub.ID()])
						input, err := inputReceipt(texts[sub.ID()], nil, h.session.limits)
						if err != nil {
							t.Fatal(err)
						}
						entry, exists := state.Entries[status.Entry]
						if !exists || len(entry.Model) != 1 {
							t.Fatal("placed input is missing", entry)
						}
						input.Timestamp = entry.Model[0].Timestamp
						wire, err := dtoObject(input, h.session.limits)
						if err != nil {
							t.Fatal(err)
						}

						if status.Status != "placed" || status.Entry == 0 || status.Answer != 0 || entries[sub.ID()] != 0 && entries[sub.ID()] != status.Entry || entries[sub.ID()] == 0 && seen[status.Entry] || !exists || entry.Conversation != c.ID() || !isMessageEntry(entry) || !equalJSONValue(messageEntryValue(entry), wire) {
							t.Fatal("successor input must retain its exact distinct boundary placement", status, entry, next)
						}
						entries[sub.ID()], seen[status.Entry] = status.Entry, true
						inputIDs = append(inputIDs, sub.ID())
					}
					want := JSON{"run": JSON{"taskId": owner, "inputs": inputIDs, "first": entries[group[0].ID()], "turns": 0}, "generation": JSON{"attempt": 1}}
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
				settled := func(sub *SubmissionHandle, owner ID, publication sessionPublication, answer string) Settlement {
					t.Helper()
					result := waitSubmission(t, sub)
					r := result.Record
					state, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					entry := state.Entries[r.Answer]
					stored, found, err := h.session.Submission(bg, sub.ID())
					if err != nil || !found || !equalJSONValue(stored, r) {
						t.Fatal("exact canonical answer settlement", stored, r, err)
					}
					if r.ID != sub.ID() || r.Conversation != c.ID() || r.Type != "input" || r.RequestID != "" || r.Status != "done" || r.Reason != "" || r.Detail != nil || r.Entry != entries[sub.ID()] || r.Answer == 0 || seen[r.Answer] || result.Task.ID != owner || result.Task.Conversation != c.ID() || result.Message == nil || len(result.Message.Content) != 1 || result.Message.Content[0].Text != answer || entry.Conversation != c.ID() || legacyEntryValue(entry)["role"] != "assistant" || entry.ByTask != owner || result.Submission.Status != "done" {
						t.Fatal("successor must settle uniformly with its own entry and actual shared answer", result, entry)
					}
					placedInput := state.Entries[entries[sub.ID()]]
					copies := 0
					for _, candidate := range state.Entries {
						if candidate.Conversation == c.ID() && isMessageEntry(candidate) && equalJSONValue(messageEntryValue(candidate), messageEntryValue(placedInput)) {
							copies++
						}
					}
					if copies != 1 {
						t.Fatal("successor settlement duplicated its boundary input", sub.ID(), copies)
					}
					var final *Submission
					for _, write := range publication.Tables {
						if write.Op == "append-entry" && write.Entry != nil && (write.Entry.ID == r.Entry || write.Entry.Conversation == c.ID() && equalJSONValue(messageEntryValue(*write.Entry), messageEntryValue(placedInput))) {
							t.Fatal("successor settlement must not append its input again", sub.ID(), write)
						}
						if write.Submission != nil && write.Submission.ID == sub.ID() {
							final = write.Submission
						}
					}
					if final == nil || final.Status != "done" || submissionEntryID(final.Value["placedEntry"]) != r.Entry || submissionEntryID(final.Value["answerEntry"]) != r.Answer {
						t.Fatal("same successor decision must retain each input's own placement", publication, r)
					}
					wireRecord, err := json.Marshal(r)
					if err != nil {
						t.Fatal(err)
					}
					var expected JSON
					if err := json.Unmarshal(wireRecord, &expected); err != nil {
						t.Fatal(err)
					}
					if !equalJSONValue(mainPreparationPublishedRecord(t, sourceAt(publication.Seq), "submission", sub.ID()).Copy(), expected) {
						t.Fatal("exact source answered record", r)
					}
					return result
				}
				releaseTaskGate(resolverRelease[1])
				awaitTaskSignal(t, providerEntered[0])
				if mode == "one-at-a-time" {
					status, err := inputs[2].Status(bg)
					if err != nil || status.Status != "queued" || status.Entry != 0 {
						t.Fatal("one-at-a-time must leave C queued behind B", status, err)
					}
				}
				owner := active(group, true)
				if owner != startedOwner {
					t.Fatal("provider dispatch must retain A's atomic successor owner", owner, startedOwner)
				}
				answeredDecision := mainPreparationTerminalPublication(t, h, inputs[1].ID())
				releaseTaskGate(providerRelease[0])
				answeredPublication := answeredDecision()
				if answeredPublication.Seq <= publication.Seq {
					t.Fatal("B/C must settle strictly after A's failure/start publication", publication, answeredPublication)
				}
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
