package durable

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

func nestedKeyToolTasks(t *testing.T, c *ConversationHandle) []TaskRecord {
	t.Helper()
	page, err := c.Commit(bg, func(tx *Tx) (any, error) {
		return tx.ScanTasks(TaskQuery{Conversation: c.ID(), Kind: "pi.tool", Order: Ascending}, 10, nil)
	})
	if err != nil || page.(Page[TaskRecord]).Next != nil {
		t.Fatal(page, err)
	}
	return page.(Page[TaskRecord]).Items
}

func completeNestedKeyTurn(t *testing.T, c *ConversationHandle) {
	t.Helper()
	sub, err := c.Submit(bg, Input{Type: "input", Content: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if got := waitSubmission(t, sub).Record; got.Status != "done" {
		t.Fatal("nested tool turn", got)
	}
}

// harness-nested-tools.test.ts:301-322. A key returns the same result;
// a conflicting call reaches the model as the caller's error result.
func TestSourceV160NestedKeyReuseAndConflict(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var runs atomic.Int64
		echo := wrapRegistration("echo")
		echo.Execute = func(_ context.Context, args any, _ *ToolAPI) (ToolResult, error) {
			runs.Add(1)
			return ToolResult{Content: "echo " + generationObject(args)["text"].(string)}, nil
		}
		batch := wrapRegistration("batch")
		reuse := make(chan ToolResult, 1)
		batch.Execute = func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
			if _, err := api.CallTool(ctx, "k", "echo", JSON{"text": "a"}); err != nil {
				return ToolResult{}, err
			}
			result, err := api.CallTool(ctx, "k", "echo", JSON{"text": "a"})
			if err != nil {
				return ToolResult{}, err
			}
			reuse <- result
			return api.CallTool(ctx, "k", "echo", JSON{"text": "b"})
		}
		_, c := openNestedResultHarness(t, b.store, echo, batch)
		completeNestedKeyTurn(t, c)
		if got := runs.Load(); got != 1 {
			t.Fatalf("echo ran %d times, want one", got)
		}
		if got := watchReceive(t, reuse); got.StructuredOutput != "echo a" {
			t.Fatal("reused echo", got)
		}
		result := v160ToolResult(t, bg, c, "c1")
		if message := v160ToolResultText(result); !strings.Contains(message, "Nested call c1/k was already made with another tool or other arguments") {
			t.Fatalf("tool result missing expected key conflict: %q", message)
		}
	})
}

// harness-nested-tools.test.ts:324-351. The two source JSON objects differ
// in field order; Go's public JSON map has no insertion-order representation.
func TestSourceV160NestedKeyMatchesEquivalentArguments(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var args [2]JSON
		for i, raw := range []string{`{"a":"x","b":"y"}`, `{"b":"y","a":"x"}`} {
			if err := json.Unmarshal([]byte(raw), &args[i]); err != nil {
				t.Fatal(err)
			}
		}
		var runs atomic.Int64
		pair := wrapRegistration("pair")
		pair.Execute = func(_ context.Context, value any, _ *ToolAPI) (ToolResult, error) {
			runs.Add(1)
			args := generationObject(value)
			return ToolResult{Content: args["a"].(string) + args["b"].(string)}, nil
		}
		batch := wrapRegistration("batch")
		outputs := make(chan [2]any, 1)
		batch.Execute = func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
			var values [2]any
			for i, arg := range args {
				result, err := api.CallTool(ctx, "k", "pair", arg)
				if err != nil {
					return ToolResult{}, err
				}
				values[i] = result.StructuredOutput
			}
			outputs <- values
			return ToolResult{}, nil
		}
		_, c := openNestedResultHarness(t, b.store, pair, batch)
		completeNestedKeyTurn(t, c)
		if got := runs.Load(); got != 1 {
			t.Fatalf("pair ran %d times, want one", got)
		}
		if got := watchReceive(t, outputs); got != [2]any{"xy", "xy"} {
			t.Fatal("key reused against equivalent arguments", got)
		}
	})
}

// harness-nested-tools.test.ts:597-629. Cancellation belongs to the first
// caller wait, not the admitted child. The child's actual start cancels it.
func TestSourceV160NestedWaitCancellationKeepsKeyedChild(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		release := make(chan struct{})
		started := make(chan ID, 1)
		cancelFirstWait := make(chan context.CancelFunc, 1)
		var runs atomic.Int64
		slow := wrapRegistration("slow")
		slow.Execute = func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
			if runs.Add(1) == 1 {
				started <- api.TaskID()
				(<-cancelFirstWait)()
			}
			select {
			case <-release:
				return ToolResult{Content: "slow done"}, nil
			case <-ctx.Done():
				return ToolResult{}, ctx.Err()
			}
		}
		type witness struct {
			firstErr error
			retryID  ID
			result   ToolResult
		}
		observed := make(chan witness, 1)
		batch := wrapRegistration("batch")
		batch.Execute = func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
			wait, stop := context.WithCancel(ctx)
			cancelFirstWait <- stop
			defer stop()
			defer releaseTaskGate(release)
			_, firstErr := api.CallTool(wait, "same", "slow", JSON{})
			close(release)
			retry, err := api.StartTool(ctx, "same", "slow", JSON{})
			if err != nil {
				return ToolResult{}, err
			}
			result, err := retry.Wait(ctx)
			if err != nil {
				return ToolResult{}, err
			}
			observed <- witness{firstErr, retry.ID(), result}
			return ToolResult{}, nil
		}
		_, c := openNestedResultHarness(t, b.store, slow, batch)
		completeNestedKeyTurn(t, c)
		got := watchReceive(t, observed)
		if !errors.Is(got.firstErr, context.Canceled) {
			t.Fatalf("first wait did not cancel: %v", got.firstErr)
		}
		if got.result.IsError || !got.result.HasStructuredOutput || got.result.StructuredOutput != "slow done" {
			t.Fatal("retry of active child", got.result)
		}
		firstID := watchReceive(t, started)
		tasks := nestedKeyToolTasks(t, c)
		if len(tasks) != 2 || firstID != got.retryID || tasks[1].ID != firstID || runs.Load() != 1 {
			t.Fatalf("retry started another call: initial=%d retry=%d tasks=%v runs=%d", firstID, got.retryID, tasks, runs.Load())
		}
	})
}

// harness-nested-tools.test.ts:631-651. Start both admissions from the
// same barrier and hold execution until both obtained their public handles.
func TestSourceV160ConcurrentNestedKeyAdmitsOneChild(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		release := make(chan struct{})
		var runs atomic.Int64
		echo := wrapRegistration("echo")
		echo.Execute = func(ctx context.Context, args any, _ *ToolAPI) (ToolResult, error) {
			runs.Add(1)
			select {
			case <-release:
				return ToolResult{Content: "echo " + generationObject(args)["text"].(string)}, nil
			case <-ctx.Done():
				return ToolResult{}, ctx.Err()
			}
		}
		type admission struct {
			handle *NestedToolHandle
			err    error
		}
		type completion struct {
			text any
			err  error
		}
		type witness struct {
			ids  [2]ID
			text [2]any
		}
		observed := make(chan witness, 1)
		batch := wrapRegistration("batch")
		batch.Execute = func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
			defer releaseTaskGate(release)
			ready, start := make(chan struct{}, 2), make(chan struct{})
			admitted := make(chan admission, 2)
			completed := make(chan completion, 2)
			for range 2 {
				go func() {
					ready <- struct{}{}
					<-start
					handle, err := api.StartTool(ctx, "k", "echo", JSON{"text": "a"})
					admitted <- admission{handle, err}
					if err == nil {
						result, err := handle.Wait(ctx)
						completed <- completion{result.StructuredOutput, err}
					}
				}()
			}
			<-ready
			<-ready
			close(start)
			var result witness
			for i := range result.ids {
				outcome := <-admitted
				if outcome.err != nil {
					return ToolResult{}, outcome.err
				}
				result.ids[i] = outcome.handle.ID()
			}
			close(release)
			for i := range result.text {
				got := <-completed
				if got.err != nil {
					return ToolResult{}, got.err
				}
				result.text[i] = got.text
			}
			observed <- result
			return ToolResult{}, nil
		}
		_, c := openNestedResultHarness(t, b.store, echo, batch)
		completeNestedKeyTurn(t, c)
		if got := runs.Load(); got != 1 {
			t.Fatalf("concurrent echo ran %d times, want one", got)
		}
		got := watchReceive(t, observed)
		if got.text != [2]any{"echo a", "echo a"} {
			t.Fatal("concurrent result values", got)
		}
		tasks := nestedKeyToolTasks(t, c)
		if len(tasks) != 2 || tasks[1].ID != got.ids[0] || got.ids[0] != got.ids[1] {
			t.Fatal("concurrent admissions did not share one child", got.ids, tasks)
		}
	})
}
