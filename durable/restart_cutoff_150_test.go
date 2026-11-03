package durable

import (
	"context"
	"testing"
)

// Native controls for f9 scheduler.ts1933's before-definition cutoff ordering
// and its wake signal, through actual host loss and a missing reopened definition.
func TestRestartLimit150MissingDefinitionCutoffWakesAndFaults(t *testing.T) {
	mainTraceBackends(t, func(t *testing.T, b backend) {
		entered := make(chan struct{}, 1)
		definition := taskDefinition(t, "task.cutoff-missing-150", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			entered <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		})
		limit := uint64(1)
		events := make(chan observedReport140, 8)
		options := Options{Settings: &HarnessSettings{RestartLimit: &limit}, OnReportWithInfo: func(ctx context.Context, err error, info ReportInfo) {
			if info.Type != "session_failed" {
				events <- observedReport140{ctx: ctx, err: err, info: info}
			}
		}}
		h := taskTestHarnessOptions(t, b.store, options, definition)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		store := mainAfterRecoveryCrash(t, h, b.store)
		h = taskTestHarnessOptions(t, store, options)
		wake, err := h.Wake(bg)
		if err != nil || !wake.Active {
			t.Error("cutoff with missing definition needs a host", wake, err)
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		task := waitPublicTask(t, h, id)
		if task.State.Outcome == nil || task.State.Outcome.Status != "faulted" || task.State.Outcome.Error == nil || task.State.Outcome.Error.Detail == nil || !equalJSONValue(task.State.Outcome.Error.Detail.Value, JSON{"reason": "restart_limit"}) {
			t.Fatal(task)
		}
		report := awaitReport140(t, events)
		if report.info.Type != "task_faulted" || report.info.Reason != "restart_limit" || report.info.Task == nil || report.info.Task.ID != id {
			t.Fatal(report)
		}
	})
}

// Native envelope guards: allowing the source's descriptive restart_limit
// error must not admit an unrelated reason or a mismatched receipt diagnostic.
func TestRestartLimit150ToolReceiptGuards(t *testing.T) {
	message := "Tool work was interrupted once in a row; it is not run again"
	outcome := TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: message, Detail: &TaskValue{Present: true, Value: JSON{"reason": "restart_limit"}}}}
	cp := toolCheckpoint{ErrorCode: "restart_limit", Result: &MessageReceipt{ErrorCode: "restart_limit", IsError: true, Diagnostics: []ToolDiagnostic{{Severity: "error", Code: "restart_limit", Message: message}}}}
	if status, err := toolReceiptRawStatus(outcome, cp); err != nil || status != "done" {
		t.Fatal(status, err)
	}
	for _, fault := range []string{"reason", "detail", "message", "diagnostic"} {
		t.Run(fault, func(t *testing.T) {
			bad := outcome
			detail := *outcome.Error
			bad.Error = &detail
			badCP := cp
			receipt := *cp.Result
			badCP.Result = &receipt
			switch fault {
			case "reason":
				bad.Error.Detail = &TaskValue{Present: true, Value: JSON{"reason": "other"}}
			case "detail":
				bad.Error.Detail = nil
			case "message":
				bad.Error.Message = "other"
			case "diagnostic":
				badCP.Result.Diagnostics = nil
			}
			if _, err := toolReceiptRawStatus(bad, badCP); err == nil {
				t.Fatal("invalid cutoff envelope admitted", fault)
			}
		})
	}
}
