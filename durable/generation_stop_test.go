package durable

import (
	"context"
	"errors"
	"testing"
)

func TestGenerationStopReportingRequiresControlErrorAndCancellationIntent(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		definition := taskDefinition(t, "task.stop.reporting", func(context.Context, TaskRecord, *TaskRuntime) error { return nil })
		h := taskTestHarness(t, b.store, definition)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		runtime := &TaskRuntime{taskID: id, context: ctx}
		for _, err := range []error{context.Canceled, ErrSealed, reject("generation aborted")} {
			if h.scheduler.expectedGenerationStop(runtime, err) {
				t.Fatalf("live task suppressed control-shaped failure: %v", err)
			}
		}
		cancel()
		for _, err := range []error{context.Canceled, ErrSealed, reject("generation aborted")} {
			if !h.scheduler.expectedGenerationStop(runtime, err) {
				t.Fatalf("expected cancellation reported: %v", err)
			}
		}
		for _, err := range []error{errors.New("provider failure"), ErrPoisoned, ErrCorrupt, reject("invalid checkpoint"), context.DeadlineExceeded} {
			if h.scheduler.expectedGenerationStop(runtime, err) {
				t.Fatalf("genuine failure suppressed after cancellation: %v", err)
			}
		}
	})
}
