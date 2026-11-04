package durable

// Measured 2026-11-04 (count1, via the project overlay with the protected prototype excluded from
// the compiler input): native produces no refusal for two opposite waits admitted behind each other.
// Both waits commit, and neither task settles - the mutual wait deadlocks (refusals empty, ranDone
// empty, 4.243s before the bounded window closes). The source refuses the second admitted wait with a
// wait-cycle error, the first is not refused, and the first completes. This fixture keeps the source's
// assertions; it is not a pass and needs direction before any runtime change.
// recipe: make test PACKAGES=./durable TEST_FLAGS="-overlay <run>/overlay.json -v -run '^TestPendingGroupCommitRefusesSecondOfTwoOppositeWaits' -count=1 -timeout=45s"
// The held-commit capability is the internal `appendFrame`/`appendFrames` hook on the native storage
// (a Storage wrapper cannot be used: "session requires native storage"), following the precedent in
// TestMainSessionUnpublishedReadBarriers. Native rejects an execution replacement from a commit that
// is not the task's own invocation ("execution task replacement requires invocation authority",
// task_record.go), so the memo is set with TaskRuntime.MemoCandidate in the task's own commit rather
// than through Tx.PutTask on another task's record.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Actual f9a26365 harness-group-commit.test.ts case 226: "refuses the second of two opposite waits
// admitted behind each other". Two conversation-owned tasks, each waiting on the other through a memo.
// The source asserts the second admitted wait is refused with a wait-cycle error, the first is not
// refused, and the first completes. Native has a waiting state with On/policy but no cycle check in the
// admission path, so this fixture measures what native actually does rather than assuming it.
// Same held-commit capability as the other group-commit fixtures (internal `appendFrame` hook).
func copyExecWithMemo(exec *TaskExecution, target ID) *TaskExecution {
	if exec == nil || exec.Native == nil {
		return exec
	}
	native := *exec.Native
	native.Memos = map[string]*TaskValue{"on": {Present: true, Value: target}}
	out := *exec
	out.Native = &native
	return &out
}

// Self-contained held-commit capability for this fixture (the same mechanism as the promoted
// group-commit controls; the package helper is not shared so the fixture stays standalone).
type pendingGroupCommitHeld struct {
	mu      sync.Mutex
	hold    bool
	counted bool
	entered chan struct{}
	release chan struct{}
}

func (h *pendingGroupCommitHeld) holdCommits() {
	h.mu.Lock()
	h.hold = true
	h.mu.Unlock()
}

func (h *pendingGroupCommitHeld) releaseHold() {
	h.mu.Lock()
	h.hold = false
	h.mu.Unlock()
	close(h.release)
}

func (h *pendingGroupCommitHeld) install(appendFrame func(byte, uint64, uint64, []byte) error) func(byte, uint64, uint64, []byte) error {
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

func TestPendingGroupCommitRefusesSecondOfTwoOppositeWaits(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	held := &pendingGroupCommitHeld{entered: make(chan struct{}), release: make(chan struct{})}

	var mu sync.Mutex
	nameOf := map[ID]string{}
	partner := map[string]ID{}
	partnerOf := func(name string) ID {
		mu.Lock()
		defer mu.Unlock()
		return partner[name]
	}
	refusals := map[string]string{}
	nameFor := func(id ID) string {
		mu.Lock()
		defer mu.Unlock()
		return nameOf[id]
	}
	ranDone := map[string]int{}
	gates := map[string]chan struct{}{}
	for _, name := range []string{"x", "y"} {
		gates[name] = make(chan struct{})
	}

	def, err := DefineTask(TaskDefinitionOptions{
		Kind:    "task.gcrace4.wait",
		Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "wait"}, nil },
		Phases: map[string]TaskPhase{
			"wait": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				name := nameFor(task.ID)
				select {
				case <-gates[name]:
				case <-ctx.Done():
					return ctx.Err()
				}
				target := partnerOf(name)
				if _, err := r.MemoCandidate(ctx, "on", target); err != nil {
					t.Errorf("%s: memo candidate: %v", name, err)
					return nil
				}
				err := r.Commit(ctx, func(_ *Tx, _ TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "done"}, On: []ID{target}, Policy: "allSettled"}, nil
				})
				if err != nil {
					mu.Lock()
					refusals[name] = err.Error()
					mu.Unlock()
					return r.Commit(ctx, func(_ *Tx, _ TaskRecord) (*TaskState, error) { return taskDone(name), nil })
				}
				return nil
			},
			"done": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				name := nameFor(task.ID)
				mu.Lock()
				ranDone[name]++
				mu.Unlock()
				return r.Commit(ctx, func(_ *Tx, _ TaskRecord) (*TaskState, error) { return taskDone(name), nil })
			},
		},
		Abort: func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(_ *Tx, _ TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "abort"}}, nil
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if _, err := registry.RegisterTask(def); err != nil {
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
	originalFrames := h.session.core.appendFrames
	h.session.core.appendFrames = func(frames []storageFrame) error {
		if len(frames) > 0 && frames[0].kind == 2 {
			held.mu.Lock()
			if held.hold && !held.counted {
				held.counted = true
				close(held.entered)
				held.mu.Unlock()
				<-held.release
			} else {
				held.mu.Unlock()
			}
		}
		if originalFrames != nil {
			return originalFrames(frames)
		}
		return nil
	}
	defer func() {
		h.session.core.appendFrame = original
		h.session.core.appendFrames = originalFrames
	}()

	c := root(t, h, ref)
	ctx, cancel := context.WithTimeout(bg, 12*time.Second)
	defer cancel()
	ids := map[string]ID{}
	if _, err := h.CommitTasks(ctx, c.ID(), func(tx *Tx) error {
		x, err := tx.CreateTask(def, JSON{"name": "x"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err != nil {
			return err
		}
		y, err := tx.CreateTask(def, JSON{"name": "y"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err != nil {
			return err
		}
		ids["x"], ids["y"] = x, y
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Native rejects an execution replacement from a commit that is not the task's own invocation
	// ("execution task replacement requires invocation authority"), so the memo is set inside each
	// task's own commit. Each still waits on the other, admitted behind each other.
	partner["x"], partner["y"] = ids["y"], ids["x"]
	nameOf[ids["x"]], nameOf[ids["y"]] = "x", "y"
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		xrec, okx, err := h.Task(ctx, ids["x"])
		yrec, oky, err2 := h.Task(ctx, ids["y"])
		if err != nil || err2 != nil || !okx || !oky {
			t.Fatal("tasks missing", err, err2)
		}
		if xrec.State.Status == "running" && yrec.State.Status == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("tasks never both ran", xrec.State.Status, yrec.State.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}

	held.holdCommits()
	close(gates["x"])
	select {
	case <-held.entered:
	case <-ctx.Done():
		t.Fatal("first wait did not reach storage while held")
	}
	close(gates["y"])
	time.Sleep(200 * time.Millisecond)
	held.releaseHold()

	// The second admitted wait must be refused; the first must not.
	deadline = time.Now().Add(4 * time.Second)
	for {
		mu.Lock()
		haveY := strings.Contains(refusals["y"], "wait cycle")
		mu.Unlock()
		if haveY {
			break
		}
		if time.Now().After(deadline) {
			mu.Lock()
			t.Fatalf("no wait-cycle refusal for the second admitted wait; refusals=%v ranDone=%v", refusals, ranDone)
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	if _, ok := refusals["x"]; ok {
		t.Fatalf("the first admitted wait must not be refused: refusals=%v", refusals)
	}
	mu.Unlock()

	rec, err := h.WaitForTask(ctx, ids["x"])
	if err != nil {
		t.Fatal(err)
	}
	if rec.State.Outcome == nil || rec.State.Outcome.Status != "completed" {
		t.Fatalf("the first task must complete: %+v", rec.State)
	}
	if err := h.Close(ctx); err != nil {
		t.Error(err)
	}
}
