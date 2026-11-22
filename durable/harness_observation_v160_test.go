package durable

import (
	"context"
	"testing"
	"time"
)

func pausedV160Harness(t *testing.T, ctx context.Context) (*Harness, *ConversationHandle, ID, *SubmissionHandle, <-chan struct{}) {
	t.Helper()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	ran := make(chan struct{}, 1)
	marker := taskDefinition(t, "test.marker", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		ran <- struct{}{}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	h := taskTestHarness(t, store, marker)
	root, err := h.Root(ctx, AgentChange{})
	if err != nil {
		t.Fatal(err)
	}
	id := createPublicTask(t, h, marker, nil, TaskOptions{Conversation: root.ID(), Background: true, Ownership: TaskOwnership{Kind: "conversation"}})
	var sid ID
	if _, err := h.Commit(ctx, func(tx *Tx) (any, error) {
		sub, err := tx.CreateSubmission(SubmissionCreate{Conversation: root.ID(), Type: "write", Status: "queued"})
		sid = sub.ID
		return nil, err
	}); err != nil {
		t.Fatal(err)
	}
	sub, err := h.Submission(ctx, sid)
	if err != nil || sub == nil {
		t.Fatalf("paused submission: %v, %v", sub, err)
	}
	return h, root, id, sub, ran
}

// Source harness-lifecycle.test.ts:635. A writable Harness remains paused if
// callers only observe it; this is separate from opening a read-only database.
func TestSourceV160HarnessObservationDoesNotSchedule(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	h, root, id, sub, ran := pausedV160Harness(t, ctx)
	notes, err := DefineDocument(DefinitionOptions{Kind: "test.viewer-notes", Scope: "session", Version: 1,
		Initial:        func(JSON) (JSON, error) { return JSON{"text": ""}, nil },
		CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Commit(ctx, func(tx *Tx) (any, error) {
		_, err := tx.AcquireDocument(notes, 0, nil, nil)
		return nil, err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Inspect(ctx); err != nil {
		t.Fatal(err)
	}
	if _, found, err := h.Task(ctx, id); err != nil || !found {
		t.Fatalf("view task: %t, %v", found, err)
	}
	if _, err := h.Usage(ctx); err != nil {
		t.Fatal(err)
	}
	if observed, err := h.Conversation(ctx, root.ID()); err != nil || observed == nil || observed.ID() != root.ID() {
		t.Fatalf("view conversation: %v, %v", observed, err)
	}
	if _, found, err := h.SnapshotDefinition(ctx, notes, 0, nil); err != nil || !found {
		t.Fatalf("view document: %t, %v", found, err)
	}
	watch, found, err := h.WatchDefinition(ctx, notes, 0, nil)
	if err != nil || !found {
		t.Fatalf("watch document: %t, %v", found, err)
	}
	watch.Stop()
	if status, err := sub.Status(ctx); err != nil || status.Status != "queued" {
		t.Fatalf("view submission: %+v, %v", status, err)
	}
	if _, err := root.Agent(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Context(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Entries(ctx, EntryQuery{}, 10, nil); err != nil {
		t.Fatal(err)
	}
	conversationWatch, err := root.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conversationWatch.Stop()
	graph, err := h.WatchTaskGraph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	graph.Stop()
	if inspected, err := h.Inspect(ctx); err != nil || inspected.Scheduling != "paused" {
		t.Fatalf("observations enabled scheduling: %+v, %v", inspected, err)
	}
	if record, found, err := h.Task(ctx, id); err != nil || !found || record.State.Status != "pending" || len(ran) != 0 {
		t.Fatalf("observation ran marker: %+v, found %t, err %v", record, found, err)
	}
}

// Source harness-lifecycle.test.ts:665. The marker is background so a scoped
// idle/abort can return without waiting for it, yet must enable scheduling.
func TestSourceV160HarnessProgressCallsSchedule(t *testing.T) {
	type progress func(context.Context, *Harness, *ConversationHandle, ID, *SubmissionHandle) error
	calls := map[string]progress{
		"submit": func(ctx context.Context, _ *Harness, root *ConversationHandle, _ ID, _ *SubmissionHandle) error {
			_, err := root.Submit(ctx, Input{Type: "write", Entry: &Entry{Kind: "note"}})
			return err
		},
		"compact": func(ctx context.Context, _ *Harness, root *ConversationHandle, _ ID, _ *SubmissionHandle) error {
			_, err := root.Compact(ctx, nil)
			return err
		},
		"abort": func(ctx context.Context, _ *Harness, root *ConversationHandle, _ ID, _ *SubmissionHandle) error {
			return root.Abort(ctx)
		},
		"conversationIdle": func(ctx context.Context, _ *Harness, root *ConversationHandle, _ ID, _ *SubmissionHandle) error {
			return root.WaitForIdle(ctx)
		},
		"submissionWait": func(ctx context.Context, _ *Harness, _ *ConversationHandle, _ ID, submission *SubmissionHandle) error {
			_, err := submission.Wait(ctx)
			return err
		},
		"waitForTask": func(ctx context.Context, h *Harness, _ *ConversationHandle, id ID, _ *SubmissionHandle) error {
			_, err := h.WaitForTask(ctx, id)
			return err
		},
		"harnessIdle": func(ctx context.Context, h *Harness, _ *ConversationHandle, _ ID, _ *SubmissionHandle) error {
			return h.WaitForIdle(ctx)
		},
	}
	for _, name := range []string{"submit", "compact", "abort", "conversationIdle", "submissionWait", "waitForTask", "harnessIdle"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(bg, 5*time.Second)
			defer cancel()
			h, root, id, sub, ran := pausedV160Harness(t, ctx)
			settled := make(chan error, 1)
			go func() { settled <- calls[name](ctx, h, root, id, sub) }()
			select {
			case <-ran:
			case <-ctx.Done():
				t.Fatalf("%s did not enable scheduling: %v", name, ctx.Err())
			}
			if err := h.Close(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case <-settled:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}

// Source harness-lifecycle.test.ts:690. Reads and registration do not resume;
// a pending task resolves its definition only from the registry at admission.
func TestSourceV160HarnessLateRegistryDefinition(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	ran := make(chan string, 3)
	define := func(label string) *TaskDefinition {
		return taskDefinition(t, "test.late-definition", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			ran <- label
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
	}
	h := taskTestHarness(t, store)
	root, err := h.Root(ctx, AgentChange{})
	if err != nil {
		t.Fatal(err)
	}
	id := createPublicTask(t, h, define("unused"), nil, TaskOptions{Conversation: root.ID(), Ownership: TaskOwnership{Kind: "conversation"}})
	inspect := func(kind, reason string) {
		t.Helper()
		view, err := h.Inspect(ctx)
		if err != nil || view.Scheduling != "paused" || len(view.Tasks) != 1 || view.Tasks[0].Record.ID != id ||
			view.Tasks[0].Kind != kind || view.Tasks[0].Reason != reason || view.Tasks[0].Record.State.Status != "pending" {
			t.Fatalf("late registry inspection = %+v, %v; want %s/%s for pending %d", view, err, kind, reason, id)
		}
	}
	inspect("blocked", "missing_task")
	dispose, err := h.options.Registry.RegisterTask(define("v1"))
	if err != nil {
		t.Fatal(err)
	}
	inspect("ready", "")
	dispose()
	if _, err := h.options.Registry.RegisterTask(define("v2")); err != nil {
		t.Fatal(err)
	}
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	if result, err := h.WaitForTask(ctx, id); err != nil || result.State.Outcome == nil || result.State.Outcome.Status != "completed" {
		t.Fatalf("late installed definition did not finish: %+v, %v", result, err)
	}
	if count := len(ran); count != 1 || <-ran != "v2" {
		t.Fatalf("newest definition did not run exactly once; count %d", count)
	}
}
