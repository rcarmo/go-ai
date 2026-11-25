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
// exactly. The observer checks the upstream publication limit for a new
// run on the default published worker policy.
func TestSourceV160CodemodePublishedWorkerConcurrentResults(t *testing.T) {
	checkWorkerConcurrentResults(t, officialWorkerPackageRootV160(t), true)
}

func TestV160CodemodeLegacyWorkerConcurrentResults(t *testing.T) {
	checkWorkerConcurrentResults(t, officialWorkerPackageRootV160(t), false)
}

// The standalone v1.6.1 published worker retains the pinned fingerprint.
// v1.6.1 codemode.test.ts:913–946 checks the default, with a legacy control.
func TestSourceV161CodemodePublishedWorkerConcurrentResults(t *testing.T) {
	checkWorkerConcurrentResults(t, officialWorkerPackageRoot(t, "GOAI_CODEMODE_V161_PACKAGE_ROOT", "1.6.1"), true)
}

func TestV161CodemodeLegacyWorkerConcurrentResults(t *testing.T) {
	checkWorkerConcurrentResults(t, officialWorkerPackageRoot(t, "GOAI_CODEMODE_V161_PACKAGE_ROOT", "1.6.1"), false)
}

func checkWorkerConcurrentResults(t *testing.T, packageRoot string, grouped bool) {
	t.Helper()
	const count = 50
	const script = `return (await Promise.all(Array.from({ length: 50 }, () => wait()))).length`
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	sandbox, remote := newJournalWorkerSandbox(packageRoot)
	sandbox.legacyJournal = !grouped
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
	if grouped && publications.Load() >= 10 {
		t.Fatalf("grouped run used %d journal publications; want fewer than 10", publications.Load())
	}
	if publications.Load() == 0 {
		t.Fatal("no journal publications observed")
	}
	if !grouped && publications.Load() < 10 {
		t.Fatalf("legacy control used %d journal publications; want at least 10", publications.Load())
	}
	t.Logf("native journal publications = %d for %d finished deliveries (source requires fewer than 10)", publications.Load(), remote.deliveries.Load())
}
