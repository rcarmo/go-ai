package durable

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Source harness-submissions.test.ts:284. This temporary private-guard oracle
// retains the observer-before-wait assertion through a real Harness; the
// public observeCommits API and JS microtask ordering are separate gaps.
func TestPendingHarnessStoredObserverPrecedesSubmissionWait(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		close(entered)
		go func() {
			defer close(ch)
			select {
			case <-release:
				ch <- terminal("hi")
			case <-ctx.Done():
			}
		}()
		return ch
	})
	h := openHarness(t, store, options)
	t.Cleanup(unblock)
	var settled atomic.Uint64
	observation, err := h.session.observeStoredCommits(sessionCommitObserver{
		advance: func(_ context.Context, publication CommitPublication) error {
			for _, write := range publication.Tables {
				if sub := write.Submission; sub != nil && sub.Status == "done" {
					settled.Store(uint64(sub.ID))
				}
			}
			return nil
		},
		end: func(WatchEnd) error { return nil },
	})
	if err != nil {
		t.Fatalf("scheduler-backed commit observer unavailable: %v", err)
	}
	defer observation.detach()
	c := root(t, h, ref)
	submission, err := c.Submit(bg, Input{Type: "input", Content: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		unblock()
		t.Fatal("provider did not start")
	}
	type result struct {
		settlement Settlement
		err        error
		observed   uint64
	}
	waited := make(chan result, 1)
	go func() { value, err := submission.Wait(bg); waited <- result{value, err, settled.Load()} }()
	deadline := time.After(3 * time.Second)
	for {
		registered := false
		h.session.taskBookkeeping(func() { registered = len(h.submissionWaiters[submission.ID()]) == 1 })
		if registered {
			break
		}
		select {
		case <-deadline:
			unblock()
			<-waited
			t.Fatal("wait was not registered")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	unblock()
	select {
	case got := <-waited:
		if got.err != nil || got.settlement.Record.Status != "done" || got.observed != uint64(submission.ID()) {
			t.Fatalf("wait resolved before observer: status=%q observed=%d want=%d err=%v", got.settlement.Record.Status, got.observed, submission.ID(), got.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("submission did not settle")
	}
}
