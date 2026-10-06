package durable

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTaskOwnershipPinnedAllSettledTerminalAndLiveForeignAndEmptyWait(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		liveEntered, liveRelease := make(chan struct{}), make(chan struct{})
		cleanupTaskGates(t, liveRelease)
		var resumes atomic.Int64
		seen := []string{}
		doneDef := taskDefinition(t, "task.foreign.done", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("done"), nil })
		})
		liveDef := taskDefinition(t, "task.foreign.live", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(liveEntered)
			<-liveRelease
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "live_failed"}}}, nil
			})
		})
		var doneID, liveID ID
		waiting := make(chan struct{})
		parentDef, err := DefineTask(TaskDefinitionOptions{Kind: "task.foreign.parent", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "run"}, nil }, Abort: func(context.Context, TaskRecord, *TaskRuntime) error { return nil }, Phases: map[string]TaskPhase{
			"run": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "join"}, On: []ID{doneID, liveID}, Policy: "allSettled"}, nil
				}); err != nil {
					return err
				}
				close(waiting)
				return nil
			},
			"join": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				resumes.Add(1)
				outcomes, err := r.Outcomes(ctx, []ID{doneID, liveID})
				if err != nil {
					return err
				}
				for _, outcome := range outcomes {
					seen = append(seen, outcome.Status)
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("parent"), nil })
			},
		}})
		if err != nil {
			t.Fatal(err)
		}
		emptyLog := []string{}
		emptyDef, err := DefineTask(TaskDefinitionOptions{Kind: "task.foreign.empty", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "run"}, nil }, Abort: func(context.Context, TaskRecord, *TaskRuntime) error { return nil }, Phases: map[string]TaskPhase{
			"run": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				emptyLog = append(emptyLog, "run")
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "join"}, On: []ID{}, Policy: "failFast"}, nil
				})
			},
			"join": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				emptyLog = append(emptyLog, "resume")
				outcomes, err := r.Outcomes(ctx, []ID{})
				if err != nil || len(outcomes) != 0 {
					t.Error(outcomes, err)
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("empty"), nil })
			},
		}})
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, b.store, doneDef, liveDef, parentDef, emptyDef)
		doneID = createPublicTask(t, h, doneDef, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if _, err := h.WaitForTask(bg, doneID); err != nil {
			t.Fatal(err)
		}
		liveID = createPublicTask(t, h, liveDef, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		awaitTaskSignal(t, liveEntered)
		parentID := createPublicTask(t, h, parentDef, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		awaitTaskSignal(t, waiting)
		state, err := h.Snapshot(bg)
		if err != nil || state.Tasks[parentID].Status != "waiting" || resumes.Load() != 0 || state.Tasks[liveID].Owner != 0 {
			t.Fatal("live foreign prematurely resumed/owned", state.Tasks[parentID], err, resumes.Load())
		}
		close(liveRelease)
		parent, err := h.WaitForTask(bg, parentID)
		if err != nil || parent.State.Outcome.Status != "completed" || strings.Join(seen, ",") != "completed,failed" || resumes.Load() != 1 {
			t.Fatal(parent, err, seen, resumes.Load())
		}
		emptyID := createPublicTask(t, h, emptyDef, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if _, err := h.WaitForTask(bg, emptyID); err != nil {
			t.Fatal(err)
		}
		if strings.Join(emptyLog, ",") != "run,resume" {
			t.Fatal(emptyLog)
		}
	})
}
