package quickjs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func receiveQuickJSV160[T any](t *testing.T, ctx context.Context, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		var zero T
		return zero
	}
}

// Source: pi v1.6.0 packages/codemode/test/sandbox.test.ts:599–638.
func TestSourceV160QuickJSAbortSignals(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	entered := make(chan context.Context, 3)
	opts := Options{Tools: []Tool{{Name: "hang", Execute: func(c context.Context, _ json.RawMessage) (json.RawMessage, error) {
		entered <- c
		<-c.Done()
		return nil, c.Err()
	}}}}
	const wait = `await tools.hang(); return 'never'`
	start := func(ctx context.Context) <-chan Result {
		out := make(chan Result, 1)
		go func() { out <- engine.Execute(ctx, wait, opts) }()
		return out
	}
	retainedCtx, releaseRetained := context.WithCancel(ctx)
	defer releaseRetained()
	retained := start(retainedCtx)
	first := receiveQuickJSV160(t, ctx, entered)

	aborted, abort := context.WithCancelCause(ctx)
	abort(errors.New("user cancelled"))
	if first.Err() != nil {
		t.Fatalf("unrelated callback signal was canceled: %v", first.Err())
	}
	pre := engine.Execute(aborted, `await tools.hang()`, opts)
	if pre.OK || pre.Error == nil || pre.Error.Kind != ErrorAborted || pre.Error.Message != "user cancelled" {
		t.Errorf("pre-cancel error=%+v; want aborted: user cancelled", pre.Error)
	}
	select {
	case <-entered:
		t.Error("pre-canceled execute dispatched a callback")
	default:
	}

	pendingCtx, cancelPending := context.WithCancel(ctx)
	defer cancelPending()
	pending := start(pendingCtx)
	second := receiveQuickJSV160(t, ctx, entered)
	cancelPending()
	got := receiveQuickJSV160(t, ctx, pending)
	if got.OK || got.Error == nil || got.Error.Kind != ErrorAborted {
		t.Errorf("pending error=%+v; want aborted", got.Error)
	}
	if len(got.Calls) != 1 || got.Calls[0].Name != "hang" || got.Calls[0].Status != "cancelled" {
		t.Errorf("pending calls=%+v; want one cancelled hang", got.Calls)
	}
	if second.Err() == nil || first.Err() != nil {
		t.Errorf("signals after independent cancel: first=%v second=%v", first.Err(), second.Err())
	}

	if err := engine.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if first.Err() == nil {
		t.Error("Close returned with the first callback signal still active")
		releaseRetained() // Join the baseline even when Close does not stop it.
	}
	closed := receiveQuickJSV160(t, ctx, retained)
	if closed.OK || closed.Error == nil || closed.Error.Kind != ErrorAborted || closed.Error.Message != "Sandbox closed" {
		t.Errorf("closed result error=%+v; want aborted: Sandbox closed", closed.Error)
	}
}
