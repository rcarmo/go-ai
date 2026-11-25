package durable

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Source: pi v1.6.0 codemode.test.ts:913–946. The final value is compared
// exactly. The observer measures the native commit count without requiring
// the existing per-delivery protocol to match the upstream batch limit.
func TestSourceV160CodemodePublishedWorkerConcurrentResults(t *testing.T) {
	const count = 50
	const script = `return (await Promise.all(Array.from({ length: 50 }, () => wait()))).length`
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	sandbox, remote := newJournalWorkerSandboxV160(t)
	registry := NewRegistry()
	ext, err := NewCodemodeExtension(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Install(&ext); err != nil {
		t.Fatal(err)
	}
	var callbacks, modelRequests, publications atomic.Int32
	release := make(chan struct{})
	ref, opts := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		out := make(chan goai.Event, 1)
		if modelRequests.Add(1) == 1 {
			out <- toolAnswer("s1", "codemode", JSON{"code": script})
		} else {
			out <- terminal("done")
		}
		close(out)
		return out
	})
	opts.Registry = registry
	opts.CodemodeGlobals = []CodemodeGlobal{{Name: "wait", Version: 1, Execute: func(ctx context.Context, _ any) (any, error) {
		if callbacks.Add(1) == count {
			close(release)
		}
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}}
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, opts)
	conversation := root(t, h, ref)
	publishedDone := make(chan struct{})
	notifyDone := sync.OnceFunc(func() { close(publishedDone) })
	observation, err := h.ObserveCommits(CommitObserver{Advance: func(_ context.Context, p CommitPublication) error {
		for i := range p.Changes.Len() {
			change, _ := p.Changes.Index(i)
			kind, _ := change.Get("type")
			switch kind.String() {
			case "document":
				record, _ := change.Get("record")
				if name, ok := record.Get("kind"); ok && name.String() == codemodeJournalKind {
					publications.Add(1)
				}
			case "submission":
				value, _ := change.Get("value")
				if status, ok := value.Get("status"); ok && status.String() == "done" {
					notifyDone()
				}
			}
		}
		return nil
	}, End: func(WatchEnd) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer observation.Detach()
	sub, err := conversation.Submit(ctx, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if settled, err := sub.Wait(ctx); err != nil || settled.Submission.Status != "done" {
		t.Fatalf("submission=%+v err=%v (callbacks=%d deliveries=%d journal publications=%d)", settled, err, callbacks.Load(), remote.deliveries.Load(), publications.Load())
	}
	select {
	case <-publishedDone:
	case <-ctx.Done():
		t.Fatalf("terminal submission was not observed: %v", ctx.Err())
	}
	result := v160ToolResult(t, ctx, conversation, "s1")
	if result.IsError || len(result.Content) != 2 || result.Content[1].Text != "50" {
		t.Fatalf("script result=%q isError=%v; want 50", v160ToolResultText(result), result.IsError)
	}
	if callbacks.Load() != count || remote.deliveries.Load() != count || modelRequests.Load() != 2 {
		t.Fatalf("callbacks=%d deliveries=%d model requests=%d; want 50, 50, 2", callbacks.Load(), remote.deliveries.Load(), modelRequests.Load())
	}
	if publications.Load() == 0 {
		t.Fatal("no journal publications observed")
	}
	t.Logf("native journal publications = %d for %d finished deliveries (source requires fewer than 10)", publications.Load(), remote.deliveries.Load())
}
