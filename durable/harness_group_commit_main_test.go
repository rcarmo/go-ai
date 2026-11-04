package durable

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Held-commit capability built on the internal `appendFrame` hook, the precedent already used by
// TestMainSessionUnpublishedReadBarriers. A commit frame (kind 2) reaches storage and stays
// unpublished until release; reads during the hold see the pre-apply image. The Session cannot be
// opened with a Storage wrapper ("session requires native storage"), so this is the only way to
// reproduce the source fixture's ControlledStorage.holdCommits() here.
type groupCommitHeld struct {
	mu      sync.Mutex
	hold    bool
	counted bool
	entered chan struct{}
	release chan struct{}
}

func (h *groupCommitHeld) holdCommits() {
	h.mu.Lock()
	h.hold = true
	h.mu.Unlock()
}

func (h *groupCommitHeld) releaseHold() {
	h.mu.Lock()
	h.hold = false
	h.mu.Unlock()
	close(h.release)
}

func (h *groupCommitHeld) install(appendFrame func(byte, uint64, uint64, []byte) error) func(byte, uint64, uint64, []byte) error {
	return func(typ byte, ordinal, high uint64, payload []byte) error {
		if typ == 2 {
			h.mu.Lock()
			if h.hold && !h.counted {
				h.counted = true
				close(h.entered)
				h.mu.Unlock()
				<-h.release
			} else {
				h.mu.Unlock()
			}
		}
		if appendFrame != nil {
			return appendFrame(typ, ordinal, high, payload)
		}
		return nil
	}
}

// Actual f9a26365 harness-group-commit.test.ts case 123: "holds a live owner whose end races a
// creation below its ended tasks: live P, ended B and A, empty C". Tree P -> B -> A -> conversation
// C. A and B end, then D is created in C and is in flight when P commits its end. P's decision reads
// what the creation writes, so it must wait for that publication and hold while D is live.
func TestMainGroupCommitHoldsLiveOwnerRaceCreation(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	held := &groupCommitHeld{entered: make(chan struct{}), release: make(chan struct{})}

	gates := map[string]chan struct{}{}
	defs := map[string]*TaskDefinition{}
	for _, name := range []string{"p", "b", "a", "d"} {
		n := name
		gates[n] = make(chan struct{})
		def, err := DefineTask(TaskDefinitionOptions{
			Kind:    "task.gcrace." + n,
			Version: 1,
			Initial: func(any) (JSON, error) { return JSON{"phase": "run"}, nil },
			Phases: map[string]TaskPhase{"run": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				select {
				case <-gates[n]:
				case <-ctx.Done():
					return ctx.Err()
				}
				return r.Commit(ctx, func(_ *Tx, _ TaskRecord) (*TaskState, error) {
					return taskDone(n), nil
				})
			}},
			Abort: func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(_ *Tx, _ TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: n}}, nil
				})
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		defs[n] = def
	}
	registry := NewRegistry()
	for _, def := range defs {
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
	}
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		close(ch)
		return ch
	})
	options.Registry = registry
	h := openHarness(t, store, options)
	original := h.session.core.appendFrame
	h.session.core.appendFrame = held.install(original)
	defer func() { h.session.core.appendFrame = original }()

	c := root(t, h, ref)
	ctx, cancel := context.WithTimeout(bg, 8*time.Second)
	defer cancel()

	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	ids := map[string]ID{}
	var conv ID
	if _, err := h.CommitTasks(ctx, c.ID(), func(tx *Tx) error {
		id, err := tx.CreateTask(defs["p"], JSON{"name": "p"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err != nil {
			return err
		}
		ids["p"] = id
		id, err = tx.CreateTask(defs["b"], JSON{"name": "b"}, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: id}})
		if err != nil {
			return err
		}
		ids["b"] = id
		id, err = tx.CreateTask(defs["a"], JSON{"name": "a"}, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: id}})
		if err != nil {
			return err
		}
		ids["a"] = id
		conv, err = tx.MintID()
		if err != nil {
			return err
		}
		return tx.CreateConversation(Conversation{ID: conv, Owner: id})
	}); err != nil {
		t.Fatal(err)
	}

	close(gates["a"])
	rec, err := h.WaitForTask(ctx, ids["a"])
	if err != nil {
		t.Fatal(err)
	}
	if rec.State.Outcome == nil || rec.State.Outcome.Status != "completed" {
		t.Fatalf("A must end first: outcome=%+v err=%v", rec.State.Outcome, rec.State.Outcome.Error)
	}
	close(gates["b"])
	rec, err = h.WaitForTask(ctx, ids["b"])
	if err != nil {
		t.Fatal(err)
	}
	if rec.State.Outcome == nil || rec.State.Outcome.Status != "completed" {
		t.Fatal("B must end second", rec.State)
	}

	// D's creation is admitted and in flight when P commits its end.
	held.holdCommits()
	sub, err := h.Conversation(ctx, conv)
	if err != nil || sub == nil {
		t.Fatal("conversation C missing", err)
	}
	creating := make(chan ID, 1)
	go func() {
		if _, err := sub.Commit(ctx, func(tx *Tx) error {
			task, err := tx.CreateTask(defs["d"], JSON{"name": "d"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			if err != nil {
				return err
			}
			creating <- task
			return nil
		}); err != nil {
			t.Errorf("creation commit: %v", err)
		}
	}()
	select {
	case <-held.entered:
	case <-ctx.Done():
		t.Fatal("creation did not reach storage while held")
	}

	// P's decision must be admitted behind the in-flight creation and wait for its publication.
	close(gates["p"])
	time.Sleep(200 * time.Millisecond)
	select {
	case <-held.entered:
	default:
	}

	held.releaseHold()
	select {
	case d := <-creating:
		ids["d"] = d
	case <-time.After(3 * time.Second):
		t.Fatal("creation commit did not settle after release")
	}

	pk, ok, err := h.Task(ctx, ids["p"])
	if err != nil || !ok {
		t.Fatal("P missing", ok, err)
	}
	if pk.State.Status != "completing" {
		t.Fatalf("P must hold as completing while D is live, got %s (outcome %+v)", pk.State.Status, pk.State.Outcome)
	}
	var drec TaskRecord
	deadline := time.Now().Add(2 * time.Second)
	for {
		var ok bool
		drec, ok, err = h.Task(ctx, ids["d"])
		if err != nil || !ok {
			t.Fatal("D missing", ok, err)
		}
		if drec.State.Status == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("D never became live while P held: D=%s P=%s", drec.State.Status, pk.State.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}

	close(gates["d"])
	drec, err = h.WaitForTask(ctx, ids["d"])
	if err != nil {
		t.Fatal(err)
	}
	if drec.State.Outcome == nil || drec.State.Outcome.Status != "completed" {
		t.Fatal("D must complete", drec.State)
	}
	rec, err = h.WaitForTask(ctx, ids["p"])
	if err != nil {
		t.Fatal(err)
	}
	if rec.State.Outcome == nil || rec.State.Outcome.Status != "completed" {
		t.Fatal("P must complete after D", rec.State)
	}
	if err := h.Close(ctx); err != nil {
		t.Error(err)
	}
}

// Actual f9a26365 harness-group-commit.test.ts case 159: "holds an owner whose end follows a
// creation below its child's pending end: live P, ending B, conversation C".
// Admission order while the line is held: blocker entry, B's decision, D's creation in C, then P's
// decision. In line order D's creation precedes P's decision, so P holds while D is live.
// Same held-commit capability as the case-123 control (internal `appendFrame` hook); sleeps stand
// for the source's `flush()` event-loop turns that establish admission order.
func TestMainGroupCommitOwnerEndFollowsCreationBelowPendingEnd(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	held := &groupCommitHeld{entered: make(chan struct{}), release: make(chan struct{})}

	gates := map[string]chan struct{}{}
	defs := map[string]*TaskDefinition{}
	for _, name := range []string{"p", "b", "d"} {
		n := name
		gates[n] = make(chan struct{})
		def, err := DefineTask(TaskDefinitionOptions{
			Kind:    "task.gcrace2." + n,
			Version: 1,
			Initial: func(any) (JSON, error) { return JSON{"phase": "run"}, nil },
			Phases: map[string]TaskPhase{"run": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				select {
				case <-gates[n]:
				case <-ctx.Done():
					return ctx.Err()
				}
				return r.Commit(ctx, func(_ *Tx, _ TaskRecord) (*TaskState, error) {
					return taskDone(n), nil
				})
			}},
			Abort: func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(_ *Tx, _ TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: n}}, nil
				})
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		defs[n] = def
	}
	registry := NewRegistry()
	for _, def := range defs {
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
	}
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		close(ch)
		return ch
	})
	options.Registry = registry
	h := openHarness(t, store, options)
	original := h.session.core.appendFrame
	h.session.core.appendFrame = held.install(original)
	defer func() { h.session.core.appendFrame = original }()

	c := root(t, h, ref)
	ctx, cancel := context.WithTimeout(bg, 8*time.Second)
	defer cancel()
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	ids := map[string]ID{}
	var conv ID
	if _, err := h.CommitTasks(ctx, c.ID(), func(tx *Tx) error {
		id, err := tx.CreateTask(defs["p"], JSON{"name": "p"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err != nil {
			return err
		}
		ids["p"] = id
		id, err = tx.CreateTask(defs["b"], JSON{"name": "b"}, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: id}})
		if err != nil {
			return err
		}
		ids["b"] = id
		conv, err = tx.MintID()
		if err != nil {
			return err
		}
		return tx.CreateConversation(Conversation{ID: conv, Owner: id})
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		rec, ok, err := h.Task(ctx, ids["b"])
		if err != nil || !ok {
			t.Fatal("B missing", ok, err)
		}
		if rec.State.Status == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("B never started", rec.State.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// The conversation handle is taken before the line is held; reads cannot pass a held line.
	sub, err := h.Conversation(ctx, conv)
	if err != nil || sub == nil {
		t.Fatal("conversation C missing", err)
	}

	// A call in flight, so what follows is admitted and stays unpublished, in this order.
	held.holdCommits()
	blocker := make(chan error, 1)
	go func() {
		_, err := h.CommitTasks(ctx, c.ID(), func(tx *Tx) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: id, Conversation: c.ID(), Kind: "note", Value: JSON{"body": "blocker"}})
		})
		blocker <- err
	}()
	select {
	case <-held.entered:
	case err := <-blocker:
		t.Fatalf("blocker commit settled before reaching storage: %v", err)
	case <-ctx.Done():
		t.Fatal("blocker did not reach storage while held")
	}

	// B ends; then D is created in C while B still shows live; then P ends.
	close(gates["b"])
	time.Sleep(150 * time.Millisecond)
	creating := make(chan ID, 1)
	go func() {
		if _, err := sub.Commit(ctx, func(tx *Tx) error {
			task, err := tx.CreateTask(defs["d"], JSON{"name": "d"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			if err != nil {
				return err
			}
			creating <- task
			return nil
		}); err != nil {
			t.Errorf("creation commit: %v", err)
		}
	}()
	time.Sleep(150 * time.Millisecond)
	close(gates["p"])
	time.Sleep(150 * time.Millisecond)

	held.releaseHold()
	if err := <-blocker; err != nil {
		t.Fatal("blocker commit", err)
	}
	select {
	case d := <-creating:
		ids["d"] = d
	case <-time.After(3 * time.Second):
		t.Fatal("creation commit did not settle after release")
	}

	// In line order D's creation precedes P's decision: P holds while D is live.
	pk, ok, err := h.Task(ctx, ids["p"])
	if err != nil || !ok {
		t.Fatal("P missing", ok, err)
	}
	if pk.State.Status != "completing" {
		t.Fatalf("P must hold as completing while D is live, got %s", pk.State.Status)
	}
	// Native dispatches the created task after the held line releases; the assertion is that P is
	// still holding for it, not that it is dispatched at this instant. Poll briefly for the dispatch.
	var drec TaskRecord
	dline := time.Now().Add(2 * time.Second)
	for {
		drec, ok, err = h.Task(ctx, ids["d"])
		if err != nil || !ok {
			t.Fatal("D missing", ok, err)
		}
		if drec.State.Status == "running" {
			break
		}
		if time.Now().After(dline) {
			t.Fatalf("D never became live while P held: D=%s P=%s", drec.State.Status, pk.State.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}

	close(gates["d"])
	drec, err = h.WaitForTask(ctx, ids["d"])
	if err != nil {
		t.Fatal(err)
	}
	if drec.State.Outcome == nil || drec.State.Outcome.Status != "completed" {
		t.Fatal("D must complete", drec.State)
	}
	rec, err := h.WaitForTask(ctx, ids["p"])
	if err != nil {
		t.Fatal(err)
	}
	if rec.State.Outcome == nil || rec.State.Outcome.Status != "completed" {
		t.Fatal("P must complete after D", rec.State)
	}
	if err := h.Close(ctx); err != nil {
		t.Error(err)
	}
}

// Actual f9a26365 harness-group-commit.test.ts case 197: "ends a child before its next phase when its
// owner's abort mark is unpublished at its step". The child's step reads the intent set, which the
// unpublished mark changes, so phase two must never run. Same held-commit capability as the case-123
// fixture (internal `appendFrame` hook); the sleep stands for the source's `flush()` turn.
func TestMainGroupCommitChildEndsBeforePhaseWhenMarkUnpublished(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	held := &groupCommitHeld{entered: make(chan struct{}), release: make(chan struct{})}

	gateReturn := make(chan struct{})
	gateOwner := make(chan struct{})
	var ranTwo atomic.Int64

	registry := NewRegistry()
	ownerDef, err := DefineTask(TaskDefinitionOptions{
		Kind:    "task.gcrace3.o",
		Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "run"}, nil },
		Phases: map[string]TaskPhase{"run": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			select {
			case <-gateOwner:
			case <-ctx.Done():
				return ctx.Err()
			}
			return r.Commit(ctx, func(_ *Tx, _ TaskRecord) (*TaskState, error) { return taskDone("o"), nil })
		}},
		Abort: func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(_ *Tx, _ TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "o"}}, nil
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	childDef, err := DefineTask(TaskDefinitionOptions{
		Kind:    "task.gcrace3.t",
		Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "one"}, nil },
		Phases: map[string]TaskPhase{
			"one": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				if err := r.Commit(ctx, func(_ *Tx, _ TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "running", Checkpoint: JSON{"phase": "two"}}, nil
				}); err != nil {
					return err
				}
				select {
				case <-gateReturn:
				case <-ctx.Done():
					return ctx.Err()
				}
				return nil
			},
			"two": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				ranTwo.Add(1)
				return r.Commit(ctx, func(_ *Tx, _ TaskRecord) (*TaskState, error) { return taskDone("t"), nil })
			},
		},
		Abort: func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(_ *Tx, _ TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "t"}}, nil
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RegisterTask(ownerDef); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RegisterTask(childDef); err != nil {
		t.Fatal(err)
	}
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		close(ch)
		return ch
	})
	options.Registry = registry
	h := openHarness(t, store, options)
	original := h.session.core.appendFrame
	h.session.core.appendFrame = held.install(original)
	defer func() { h.session.core.appendFrame = original }()

	c := root(t, h, ref)
	ctx, cancel := context.WithTimeout(bg, 10*time.Second)
	defer cancel()
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	var owner, child ID
	if _, err := h.CommitTasks(ctx, c.ID(), func(tx *Tx) error {
		var err error
		owner, err = tx.CreateTask(ownerDef, JSON{"name": "o"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err != nil {
			return err
		}
		child, err = tx.CreateTask(childDef, JSON{"name": "t"}, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: owner}})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		rec, ok, err := h.Task(ctx, child)
		if err != nil || !ok {
			t.Fatal("child missing", ok, err)
		}
		if rec.State.Status == "running" && rec.State.Checkpoint["phase"] == "two" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child never reached phase two checkpoint", rec.State)
		}
		time.Sleep(5 * time.Millisecond)
	}

	held.holdCommits()
	marking := make(chan string, 1)
	go func() {
		phase, err := h.AbortTask(ctx, owner)
		if err != nil {
			t.Errorf("abort owner: %v", err)
		}
		marking <- phase
	}()
	select {
	case <-held.entered:
	case <-ctx.Done():
		t.Fatal("abort mark did not reach storage while held")
	}

	// The child's phase returns: its step reads the intent set, which the unpublished mark changes.
	close(gateReturn)
	time.Sleep(200 * time.Millisecond)
	if ranTwo.Load() != 0 {
		t.Fatalf("child ran its next phase despite the unpublished mark: ran=%d", ranTwo.Load())
	}

	held.releaseHold()
	if got := <-marking; got != "marked" {
		t.Fatalf("abort returned %q, want marked", got)
	}
	rec, err := h.WaitForTask(ctx, child)
	if err != nil {
		t.Fatal(err)
	}
	if rec.State.Outcome == nil || rec.State.Outcome.Status != "aborted" {
		t.Fatalf("child must end aborted: %+v", rec.State)
	}
	rec, err = h.WaitForTask(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if rec.State.Outcome == nil || rec.State.Outcome.Status != "aborted" {
		t.Fatalf("owner must end aborted: %+v", rec.State)
	}
	if ranTwo.Load() != 0 {
		t.Fatalf("child ran its next phase after release: ran=%d", ranTwo.Load())
	}
	if err := h.Close(ctx); err != nil {
		t.Error(err)
	}
}
