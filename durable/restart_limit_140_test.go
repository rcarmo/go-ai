package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
)

func seedInterrupted140(t *testing.T, h *Harness, id ID) {
	t.Helper()
	_, err := h.session.Commit(bg, func(tx *Tx) error {
		task := tx.state.Tasks[id]
		task.Status = "running"
		native := *task.Execution.Native
		native.State.Status = "running"
		task.Execution = &TaskExecution{Tag: nativeTaskTag, Native: &native}
		return tx.stage(Write{Op: "put-task", Task: &task})
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestRestartLimit140RecoveryParkingRetryRaiseAndAbort(t *testing.T) {
	for _, action := range []string{"retry", "raise", "abort"} {
		t.Run(action, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var runs, reports atomic.Int64
				events := make(chan observedReport140, 8)
				def := taskDefinition(t, "task.restart-limit-140", func(ctx context.Context, record TaskRecord, r *TaskRuntime) error {
					runs.Add(1)
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("done"), nil })
				})
				limit := uint64(2)
				options := Options{Settings: &HarnessSettings{RestartLimit: &limit}, OnReport: func(error) { reports.Add(1) }, OnReportWithInfo: func(ctx context.Context, err error, info ReportInfo) {
					events <- observedReport140{ctx: ctx, err: err, info: info}
				}}
				h := taskTestHarnessOptions(t, b.store, options, def)
				id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				for interruption := uint64(1); interruption <= 2; interruption++ {
					seedInterrupted140(t, h, id)
					if err := h.Close(bg); err != nil {
						t.Fatal(err)
					}
					h = taskTestHarnessOptions(t, interruptedStore140(t, reopenStoreAfterHarnessClose(t, h.session.store), id), options, def)
					state, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					if task := state.Tasks[id]; task.Restarts != interruption || task.Status != "pending" {
						t.Fatalf("recovery: %+v", task)
					}
				}
				parkedReport := awaitReport140(t, events)
				if parkedReport.info.Type != "task_parked" || parkedReport.info.Task == nil || parkedReport.info.Task.ID != id || parkedReport.info.Task.Kind != def.Kind() || parkedReport.info.Task.ConversationID != 1 || parkedReport.info.Reason != "" {
					t.Fatal(parkedReport)
				}
				limit = 999 // Open must detach live host settings.
				if h.RestartLimit() != 2 || reports.Load() != 1 || runs.Load() != 0 {
					t.Fatal(h.RestartLimit(), reports.Load(), runs.Load())
				}
				inspect, err := h.InspectTasks(bg)
				if err != nil || len(inspect.Tasks) != 1 || inspect.Tasks[0].Reason != "restart_limit" || inspect.Tasks[0].Record.Restarts != 2 {
					t.Fatalf("%+v %v", inspect, err)
				}
				if err = h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				switch action {
				case "retry":
					if err = h.RetryTask(bg, id); err != nil {
						t.Fatal(err)
					}
				case "raise":
					cap := uint64(3)
					if err = h.SetSettings(bg, HarnessSettings{RestartLimit: &cap}); err != nil {
						t.Fatal(err)
					}
				case "abort":
					if _, err = h.AbortTask(bg, id); err != nil {
						t.Fatal(err)
					}
				}
				result := waitPublicTask(t, h, id)
				want := "completed"
				if action == "abort" {
					want = "aborted"
				}
				if result.State.Outcome.Status != want || result.Restarts != 0 {
					t.Fatalf("%+v", result)
				}
				expected := int64(1)
				if action == "abort" {
					expected = 0
				}
				if runs.Load() != expected {
					t.Fatal(runs.Load())
				}
				if err = h.RetryTask(bg, id); err != nil {
					t.Fatal("terminal retry must be no-op", err)
				}
				if err = h.RetryTask(bg, id+999); err == nil {
					t.Fatal("unknown task retry accepted")
				}
			})
		})
	}
}

func TestRestartLimit140ProgressNotIntentOrMemo(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		def := taskDefinition(t, "task.restart-progress-140", func(ctx context.Context, record TaskRecord, r *TaskRuntime) error {
			if record.Restarts != 1 {
				return fmt.Errorf("entry count %d", record.Restarts)
			}
			if err := r.Commit(ctx, func(_ *Tx, current TaskRecord) (*TaskState, error) {
				state := current.State
				state.Checkpoint["number"] = json.Number("1.0")
				return &state, nil
			}); err != nil {
				return err
			}
			if _, err := r.MemoCandidate(ctx, "memo", "value"); err != nil {
				return err
			}
			if err := r.Commit(ctx, func(_ *Tx, current TaskRecord) (*TaskState, error) {
				if current.Restarts != 1 {
					return nil, fmt.Errorf("identical intent/memo reset count: %d", current.Restarts)
				}
				return &TaskState{Status: "running", Checkpoint: JSON{"phase": "work", "progress": true}}, nil
			}); err != nil {
				return err
			}
			return r.Commit(ctx, func(_ *Tx, current TaskRecord) (*TaskState, error) {
				if current.Restarts != 0 {
					return nil, fmt.Errorf("progress retained count: %d", current.Restarts)
				}
				return taskDone(nil), nil
			})
		})
		h := taskTestHarness(t, b.store, def)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		_, err := h.session.Commit(bg, func(tx *Tx) error {
			task, err := copyTask(tx.state.Tasks[id], tx.limits)
			if err != nil {
				return err
			}
			task.Checkpoint = JSON{}
			task.Execution.Native.State.Checkpoint["number"] = json.Number("1")
			return tx.stage(Write{Op: "put-task", Task: &task})
		})
		if err != nil {
			t.Fatal(err)
		}
		seedInterrupted140(t, h, id)
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}
		h = taskTestHarness(t, interruptedStore140(t, reopenStoreAfterHarnessClose(t, h.session.store), id), def)
		if h.RestartLimit() != DefaultRestartLimit {
			t.Fatal(h.RestartLimit())
		}
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		record := waitPublicTask(t, h, id)
		if record.State.Outcome == nil || record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v", record.State.Outcome)
		}
	})
}

func TestRestartLimit140InterruptedAbortFaultsBeforeDefinitionResolution(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing=%v", missing), func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var abortCalls atomic.Int64
				events := make(chan observedReport140, 8)
				definition, err := DefineTask(TaskDefinitionOptions{Kind: "task.abort-cutoff-140", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "work"}, nil }, Phases: map[string]TaskPhase{"work": func(context.Context, TaskRecord, *TaskRuntime) error { return fmt.Errorf("ordinary phase ran") }}, Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					abortCalls.Add(1)
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
					})
				}})
				if err != nil {
					t.Fatal(err)
				}
				limit := uint64(2)
				options := Options{Settings: &HarnessSettings{RestartLimit: &limit}, OnReportWithInfo: func(ctx context.Context, err error, info ReportInfo) {
					events <- observedReport140{ctx: ctx, err: err, info: info}
				}}
				h := taskTestHarnessOptions(t, b.store, options, definition)
				id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if _, err = h.AbortTask(bg, id); err != nil {
					t.Fatal(err)
				}
				for interruption := uint64(1); interruption <= limit; interruption++ {
					seedInterrupted140(t, h, id)
					if err = h.Close(bg); err != nil {
						t.Fatal(err)
					}
					definitions := []*TaskDefinition{definition}
					if missing && interruption == limit {
						definitions = nil
					}
					h = taskTestHarnessOptions(t, interruptedStore140(t, reopenStoreAfterHarnessClose(t, h.session.store), id), options, definitions...)
				}
				if err = h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				report := awaitReport140(t, events)
				result := waitPublicTask(t, h, id)
				if result.State.Outcome == nil || result.State.Outcome.Status != "faulted" || result.State.Outcome.Error == nil || result.State.Outcome.Error.Detail == nil || !equalJSONValue(result.State.Outcome.Error.Detail.Value, JSON{"reason": "restart_limit"}) || result.Restarts != 0 {
					t.Fatalf("%+v", result)
				}
				if report.info.Type != "task_faulted" || report.info.Reason != "restart_limit" || report.info.Task.ID != id || abortCalls.Load() != 0 {
					t.Fatal(report, abortCalls.Load())
				}
			})
		})
	}
}

func TestRestartLimit140RejectedCutoffDoesNotReportFault(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		events := make(chan observedReport140, 8)
		definition := taskDefinition(t, "task.reject-cutoff-140", func(context.Context, TaskRecord, *TaskRuntime) error { return fmt.Errorf("must not run") })
		limit := uint64(2)
		h := taskTestHarnessOptions(t, b.store, Options{Settings: &HarnessSettings{RestartLimit: &limit}, OnReportWithInfo: func(ctx context.Context, err error, info ReportInfo) {
			events <- observedReport140{ctx: ctx, err: err, info: info}
		}}, definition)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		// AbortTask resumes scheduling, allowing the cutoff decision to race the
		// injected rejection. Seed the marked cutoff atomically without enabling
		// scheduling; reservePass below exercises the real decision/adoption path.
		if _, err := h.session.Commit(bg, func(tx *Tx) error {
			task := markTask(tx.state.Tasks[id])
			task.Restarts = limit
			return tx.stage(Write{Op: "put-task", Task: &task})
		}); err != nil {
			t.Fatal(err)
		}
		var original func(byte, uint64, uint64, []byte) error
		h.session.taskBookkeeping(func() {
			original = h.session.core.appendFrame
			h.session.core.appendFrame = func(byte, uint64, uint64, []byte) error { return reject("cutoff adoption rejected") }
		})
		reservations, err := h.scheduler.reservePass(false)
		h.session.taskBookkeeping(func() { h.session.core.appendFrame = original })
		if err == nil || len(reservations) != 0 {
			t.Fatal("rejected cutoff admitted", reservations, err)
		}
		report := awaitReport140(t, events)
		if report.info.Type != "session_failed" {
			t.Fatal("unadopted fault was reported", report)
		}
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}
		reopened := reopenStoreAfterHarnessClose(t, h.session.store)
		defer reopened.Close(bg)
		snapshot, err := reopened.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		task := snapshot.Tasks[id]
		if task.Status != "pending" || taskHasDecidedOutcome(task) || task.Restarts != limit || !taskAborted(task) {
			t.Fatal("unadopted cutoff changed durable state", task)
		}
		select {
		case extra := <-events:
			t.Fatal("extra cutoff report", extra)
		default:
		}
	})
}

func TestRestartLimit140BuiltinGenerationAbortCutoffSettlesSubmission(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		events := make(chan observedReport140, 8)
		limit := uint64(1)
		options := Options{Settings: &HarnessSettings{RestartLimit: &limit}, OnReportWithInfo: func(ctx context.Context, err error, info ReportInfo) {
			events <- observedReport140{ctx: ctx, err: err, info: info}
		}}
		h := openHarness(t, b.store, options)
		conversation, err := h.Root(bg, AgentChange{})
		if err != nil {
			t.Fatal(err)
		}
		submission, err := conversation.Submit(bg, Input{Content: "cutoff"})
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		var id ID
		for _, task := range snapshot.Tasks {
			if task.Kind == "pi.generation" {
				id = task.ID
			}
		}
		if id == 0 {
			t.Fatal("generation missing")
		}
		if _, err = h.AbortTask(bg, id); err != nil {
			t.Fatal(err)
		}
		if _, err = h.session.Commit(bg, func(tx *Tx) error {
			task := tx.state.Tasks[id]
			task.Status = "running"
			return tx.stage(Write{Op: "put-task", Task: &task})
		}); err != nil {
			t.Fatal(err)
		}
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}
		h = openHarness(t, interruptedStore140(t, reopenStoreAfterHarnessClose(t, h.session.store), id), options)
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		report := awaitReport140(t, events)
		result := waitPublicTask(t, h, id)
		if report.info.Type != "task_faulted" || report.info.Reason != "restart_limit" || report.info.Task.ID != id || result.State.Outcome == nil || result.State.Outcome.Status != "faulted" || result.Restarts != 0 {
			t.Fatal(report, result)
		}
		if err = h.WaitForIdle(bg); err != nil {
			t.Fatal(err)
		}
		snapshot, err = h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if settled := snapshot.Submissions[submission.ID()]; settled.Status != "failed" || settled.Value["errorCode"] != "faulted" {
			t.Fatal("cutoff did not settle submission", snapshot.Submissions[submission.ID()])
		}
	})
}

// Crash fixtures must retain a running record explicitly: a successful Close
// now requeues joined hosts without consuming the interruption budget.
func interruptedStore140(t *testing.T, store Storage, id ID) Storage {
	t.Helper()
	snapshot, err := store.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	task := snapshot.Tasks[id]
	task.Status = "running"
	if task.Execution != nil && task.Execution.Native != nil {
		task.Execution.Native.State.Status = "running"
	}
	if _, err := store.Apply(bg, Batch{Writes: []Write{{Op: "put-task", Task: &task}}}); err != nil {
		t.Fatal(err)
	}
	return store
}
