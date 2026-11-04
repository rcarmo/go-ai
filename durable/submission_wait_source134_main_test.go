package durable

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Source harness-submissions.test.ts:134: cancelling one wait leaves the
// placed submission unchanged, while an independently pending wait rejects on
// close. The Go model stream is held and released after the close signal so this
// test does not depend on the separately paused invocation-close contract.
func TestMainSubmissionWaitSource134CancellationAndClose(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	started, release, streamDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event)
		close(started)
		go func() {
			defer close(streamDone)
			defer close(ch)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}()
		return ch
	})
	h := openHarness(t, store, options)
	t.Cleanup(unblock)
	conversation := root(t, h, ref)
	submission, err := conversation.Submit(bg, Input{Type: "input", Content: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		unblock()
		t.Fatal("model request did not start")
	}
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	cancelled, pending := make(chan error, 1), make(chan error, 1)
	go func() { _, err := submission.Wait(ctx); cancelled <- err }()
	go func() { _, err := submission.Wait(bg); pending <- err }()
	deadline := time.After(3 * time.Second)
	for {
		registered := false
		h.session.taskBookkeeping(func() { registered = len(h.submissionWaiters[submission.ID()]) == 2 })
		if registered {
			break
		}
		select {
		case <-deadline:
			unblock()
			t.Fatal("both waits were not registered")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	select {
	case err := <-cancelled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled wait: %v", err)
		}
	case <-time.After(3 * time.Second):
		unblock()
		t.Fatal("cancelled wait did not return")
	}
	status, err := submission.Status(bg)
	if err != nil || status.Status != "placed" {
		unblock()
		t.Fatalf("status after cancellation: %+v, %v", status, err)
	}
	closed := make(chan error, 1)
	go func() { closed <- h.Close(bg) }()
	select {
	case err := <-pending:
		if !errors.Is(err, ErrClosed) {
			unblock()
			t.Fatalf("pending wait: %v", err)
		}
	case <-time.After(3 * time.Second):
		unblock()
		t.Fatal("close did not reject pending wait")
	}
	unblock()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("harness close did not finish")
	}
	select {
	case <-streamDone:
	case <-time.After(3 * time.Second):
		t.Fatal("model stream did not finish")
	}
	h.session.taskBookkeeping(func() {
		if len(h.submissionWaiters) != 0 {
			t.Error("submission waiters retained after close")
		}
	})
}
