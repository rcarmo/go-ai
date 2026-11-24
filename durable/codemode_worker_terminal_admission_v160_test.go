package durable

import (
	"context"
	"sync/atomic"
	"testing"
)

// codemode.test.ts:522–538. The portable guest reports its drain and end
// together instead of holding a storage commit until the script has ended.
func TestSourceV160CodemodePublishedWorkerDoesNotAdmitCallAtFinalDrain(t *testing.T) {
	sandbox, remote := newJournalWorkerSandboxV160(t)
	registry := NewRegistry()
	var echoes atomic.Int32
	echo := wrapRegistration("echo")
	echo.Execute = func(context.Context, any, *ToolAPI) (ToolResult, error) {
		echoes.Add(1)
		return ToolResult{Content: "late"}, nil
	}
	if err := registry.Register(echo); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		result CodemodeResult
		err    error
	}
	outcomes := make(chan outcome, 1)
	def := taskDefinition(t, "codemode.final.drain", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		result, err := r.RunCodemode(ctx, "run", `tools.echo({ text: "late" }); return 1;`, sandbox)
		outcomes <- outcome{result, err}
		if err != nil {
			return err
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	if _, err := registry.RegisterTask(def); err != nil {
		t.Fatal(err)
	}
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, Options{Registry: registry})
	id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	if err := h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	if task := waitPublicTask(t, h, id); task.State.Outcome.Status != "completed" {
		t.Fatalf("host did not complete: %+v", task.State)
	}
	got := watchReceive(t, outcomes)
	if got.err != nil || got.result.Error != "" || !got.result.HasValue || got.result.Output != "1" || !equalJSONValue(got.result.Value, 1) {
		t.Errorf("script result = %+v, error=%v; want 1 without error", got.result, got.err)
	}
	if count := echoes.Load(); count != 0 {
		t.Errorf("echo ran %d times; want zero", count)
	}
	scanned, err := h.Commit(bg, func(tx *Tx) (any, error) { return tx.ScanTasks(TaskQuery{Kind: "pi.tool"}, 10, nil) })
	if err != nil {
		t.Fatal(err)
	}
	if page := scanned.(Page[TaskRecord]); len(page.Items) != 0 || page.Next != nil {
		t.Errorf("admitted tool tasks = %+v; want none", page)
	}
	if remote.starts.Load() != 1 || remote.deliveries.Load() != 0 {
		t.Errorf("guest starts=%d deliveries=%d; want one start and no replies", remote.starts.Load(), remote.deliveries.Load())
	}
}
