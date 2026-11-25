package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

// Source: pi v1.6.0 codemode.test.ts:504–520. The echo waits for the hold
// callback to enter, so the assertion cannot pass on a call never admitted.
func TestSourceV160CodemodePublishedWorkerAbortsRunningNestedCall(t *testing.T) {
	entered := make(chan struct{})
	heldID := make(chan ID, 1)
	joined := make(chan error, 1)
	var holds atomic.Int32
	hold := wrapRegistration("hold")
	hold.Execute = func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
		if holds.Add(1) != 1 {
			return ToolResult{}, errors.New("hold executed more than once")
		}
		heldID <- api.TaskID()
		close(entered)
		<-ctx.Done()
		joined <- ctx.Err()
		return ToolResult{}, ctx.Err()
	}
	echo := wrapRegistration("echo")
	echo.Execute = func(ctx context.Context, args any, _ *ToolAPI) (ToolResult, error) {
		select {
		case <-entered:
			return ToolResult{Content: args.(JSON)["text"].(string)}, nil
		case <-ctx.Done():
			return ToolResult{}, ctx.Err()
		}
	}
	const script = `tools.hold({}); await tools.echo({ text: "a" }); return 1;`
	h, _, result := runV160PublishedWorkerOutput(t, script, nil, 1, hold, echo)
	if result.IsError || len(result.Content) != 2 || result.Content[1].Text != "1" {
		t.Errorf("script result = %+v; want one successful value of 1", result)
	}
	if got := holds.Load(); got != 1 {
		t.Fatalf("hold runs = %d, want one", got)
	}
	select {
	case err := <-joined:
		if err != context.Canceled {
			t.Errorf("held callback ended with %v, want cancellation", err)
		}
	default:
		t.Fatal("result returned before the held callback observed cancellation")
	}
	id := <-heldID
	record, found, err := h.Task(t.Context(), id)
	if err != nil || !found || record.State.Status != "terminal" || record.State.Outcome == nil || record.State.Outcome.Status != "aborted" {
		t.Fatalf("held nested task = %+v, found=%v err=%v; want terminal and aborted", record, found, err)
	}
	var input canonicalToolInput
	if record.Input == nil {
		t.Fatal("held task lost its original input")
	}
	if err := fromObject(taskCheckpointMap(record.Input.Value), &input, DefaultLimits()); err != nil || input.Kind != "nested" || input.Call.Name != "hold" {
		t.Errorf("held task input = %+v, err=%v; want the nested hold", input, err)
	}
}
