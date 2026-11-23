package durable

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

type capturedCodemodeExecutionV160 struct {
	*RemoteCodemodeSandbox
	started chan CodemodeExecution
}

func (s *capturedCodemodeExecutionV160) Start(ctx context.Context, code string, start CodemodeStart) (CodemodeExecution, error) {
	vm, err := s.RemoteCodemodeSandbox.Start(ctx, code, start)
	if err == nil {
		select {
		case s.started <- vm:
		case <-ctx.Done():
			_ = vm.Close()
			return nil, ctx.Err()
		}
	}
	return vm, err
}

// codemode.test.ts:474–502. The host closes the live portable guest before the
// Harness. Go's remote sandbox has no pool Close, so close its sole active
// execution. A host diagnostic or settled submission provides the observable
// boundary where the source waits for a timer.
func TestSourceV160CodemodePublishedWorkerHostCutoff(t *testing.T) {
	base, first := newJournalWorkerSandboxV160(t)
	sandbox := &capturedCodemodeExecutionV160{RemoteCodemodeSandbox: base, started: make(chan CodemodeExecution, 1)}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var echoes, requests atomic.Int32
	var blocking atomic.Bool
	blocking.Store(true)
	holdEntered := make(chan struct{})
	registry := NewRegistry()
	install := func(s CodemodeSandbox) {
		t.Helper()
		extension, err := NewCodemodeExtension(s)
		if err != nil {
			t.Fatal(err)
		}
		if err := registry.Install(&extension); err != nil {
			t.Fatal(err)
		}
	}
	install(sandbox)
	echo := wrapRegistration("echo")
	echo.ReplaySafe = false
	echo.Execute = func(_ context.Context, args any, _ *ToolAPI) (ToolResult, error) {
		if args.(JSON)["text"] != "a" {
			t.Errorf("echo args = %+v; want text a", args)
		}
		echoes.Add(1)
		return ToolResult{Content: "echo a"}, nil
	}
	hold := wrapRegistration("hold")
	hold.ReplaySafe = true
	hold.Execute = func(ctx context.Context, _ any, _ *ToolAPI) (ToolResult, error) {
		if blocking.Load() {
			close(holdEntered)
			<-ctx.Done()
			return ToolResult{}, ctx.Err()
		}
		return ToolResult{Content: "held "}, nil
	}
	for _, tool := range []ToolRegistration{echo, hold} {
		if err := registry.Register(tool); err != nil {
			t.Fatal(err)
		}
	}
	const source = `await tools.echo({ text: "a" }); await tools.hold({}); return "done";`
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			ch <- toolAnswer("s1", "codemode", JSON{"code": source})
		} else {
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	cutoffReported := make(chan struct{}, 1)
	options.OnReport = func(err error) {
		if errors.Is(err, errCodemodeHostCutoff) {
			select {
			case cutoffReported <- struct{}{}:
			default:
			}
		}
	}
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, options)
	conversation := root(t, h, ref)
	sub, err := conversation.Submit(ctx, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-holdEntered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case vm := <-sandbox.started:
		if err := vm.Close(); err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	settled := make(chan error, 1)
	go func() { _, err := sub.Wait(ctx); settled <- err }()
	select {
	case <-cutoffReported:
	case err := <-settled:
		if err != nil {
			t.Fatalf("wait after guest cutoff: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("guest cutoff did not reach the host: %v", ctx.Err())
	}
	state, err := h.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runs := 0
	for _, doc := range state.Documents {
		if doc.Kind != codemodeRunKind {
			continue
		}
		runs++
		var run codemodeRunState
		if err := decodeDocumentValue(doc, DefaultLimits(), &run); err != nil {
			t.Fatal(err)
		}
		if run.Result != nil {
			t.Errorf("run result before restart = %+v; want no outcome", run.Result)
		}
	}
	if runs != 1 {
		t.Fatalf("run documents = %d; want 1", runs)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("provider requests before Harness close = %d; want 1", got)
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	blocking.Store(false)
	replacement, second := newJournalWorkerSandboxV160(t)
	install(replacement)
	next := openHarness(t, reopenStoreAfterHarnessClose(t, store), options)
	if err := next.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := next.Submission(ctx, sub.ID())
	if err != nil || reopened == nil {
		t.Fatalf("reopen submission: %v, %v", reopened, err)
	}
	if result, err := reopened.Wait(ctx); err != nil || result.Submission.Status != "done" {
		t.Fatalf("replayed submission: %+v, %v", result, err)
	}
	conversation, err = next.Conversation(ctx, conversation.ID())
	if err != nil {
		t.Fatal(err)
	}
	result := v160ToolResult(t, ctx, conversation, "s1")
	_, text, found := strings.Cut(v160ToolResultText(result), "\nOutput:\n")
	if result.IsError || !found || text != "done" {
		t.Errorf("published result = %q, error=%t; want done", v160ToolResultText(result), result.IsError)
	}
	if got := echoes.Load(); got != 1 {
		t.Errorf("echo effects = %d; want 1 call with text a", got)
	}
	if first.starts.Load() != 1 || second.starts.Load() != 1 || requests.Load() != 2 {
		t.Errorf("first and second guest starts = %d, %d; provider requests = %d; want 1,1,2", first.starts.Load(), second.starts.Load(), requests.Load())
	}
}

// Supplemental cause check: an upstream caller cancel must not masquerade as
// an explicit host close, even when Next uses a live context of its own.
func TestV160CodemodePublishedWorkerCallerCancelIsNotHostCutoff(t *testing.T) {
	sandbox, _ := newJournalWorkerSandboxV160(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	parent, cancelParent := context.WithCancel(ctx)
	defer cancelParent()
	vm, err := sandbox.Start(parent, `await tools.hold({});`, CodemodeStart{
		ParallelCalls: true, DrainAcknowledgements: true, StartedAt: 100,
		MemoryLimitBytes: DefaultCodemodeMemoryLimitBytes, InterruptBudget: DefaultCodemodeInterruptBudget,
		Tools: []CodemodeToolDescription{{Name: "hold", Identifier: "hold"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	if event, err := vm.Next(ctx); err != nil || event.Call == nil || event.Call.Name != "hold" {
		t.Fatalf("before parent cancel: %+v, %v; want hold", event, err)
	}
	cancelParent()
	if err := vm.Close(); err != nil {
		t.Fatal(err)
	}
	// A drain may already have been buffered before the parent canceled.
	for {
		if _, err := vm.Next(ctx); err != nil {
			if !errors.Is(err, context.Canceled) || errors.Is(err, errCodemodeHostCutoff) {
				t.Fatalf("caller cancellation = %v; want canceled, not host cutoff", err)
			}
			break
		}
	}
}
