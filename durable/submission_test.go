package durable

import (
	"bytes"
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestM1bRequestDedupQueueAndWrite(t *testing.T) {
	var calls atomic.Int64
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		n := calls.Add(1)
		ch := make(chan goai.Event)
		go func() {
			if n == 1 {
				entered <- struct{}{}
				<-release
			}
			ch <- terminal("answer")
			close(ch)
		}()
		return ch
	})
	s, _ := NewMemory()
	h := openHarness(t, s, options)
	cleanupTaskGates(t, release)
	r := root(t, h, ref)
	first, e := r.Submit(bg, Input{Content: "first", RequestID: "same"})
	if e != nil {
		t.Fatal(e)
	}
	<-entered
	duplicate, e := r.Submit(bg, Input{Content: "ignored", RequestID: "same"})
	if e != nil || duplicate.ID() != first.ID() {
		t.Fatal(e)
	}
	second, e := r.Submit(bg, Input{Content: "second", RequestID: "queued"})
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(bg, 20*time.Millisecond)
	defer cancel()
	if _, e = first.Wait(ctx); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("Wait canceled work", e)
	}
	if _, e = r.Submit(bg, Input{Content: "cross-type", RequestID: "same", Type: "write"}); e == nil {
		t.Fatal("cross-type reuse")
	}
	if _, e = r.Submit(bg, Input{Content: "invalid", Type: "unsupported"}); !errors.Is(e, ErrUnsupported) {
		t.Fatal(e)
	}
	if _, e = r.Commit(bg, func(*Tx) error { return nil }); e == nil {
		t.Fatal("busy passivecommit")
	}
	close(release)
	if waitSubmission(t, first).Submission.Status != "done" || waitSubmission(t, second).Submission.Status != "done" || calls.Load() != 2 {
		t.Fatal("queue discarded")
	}
	passive, e := r.Submit(bg, Input{Content: "passive", RequestID: "write", Type: "write"})
	if e != nil {
		t.Fatal(e)
	}
	if waitSubmission(t, passive).Submission.Status != "done" || calls.Load() != 2 {
		t.Fatal("write triggered model")
	}
	other, e := h.CreateConversation(bg, AgentChange{Model: ref})
	if e != nil {
		t.Fatal(e)
	}
	independent, e := other.Submit(bg, Input{Content: "other", RequestID: "same"})
	if e != nil {
		t.Fatal(e)
	}
	if independent.ID() == first.ID() || waitSubmission(t, independent).Submission.Status != "done" {
		t.Fatal("crossconversation ID collision")
	}
}

func TestM1bManySequentialAdmissionsNoStrandedQueue(t *testing.T) {
	var calls atomic.Int64
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		calls.Add(1)
		ch := make(chan goai.Event, 1)
		ch <- terminal("ok")
		close(ch)
		return ch
	})
	s, _ := NewMemory()
	h := openHarness(t, s, options)
	r := root(t, h, ref)
	for k := 0; k < 20; k++ {
		started := time.Now()
		sub, e := r.Submit(bg, Input{Content: "next"})
		if e != nil {
			t.Fatal(e)
		}
		admitted := time.Since(started)
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		settled, waitErr := sub.Wait(ctx)
		cancel()
		elapsed := time.Since(started)
		h.mu.Lock()
		worker := h.workers[r.ID()]
		h.mu.Unlock()
		t.Logf("ADMISSION index=%d id=%d submit_ms=%.3f total_ms=%.3f wait_ms=%.3f calls=%d worker=%v status=%s err=%v", k, sub.ID(), float64(admitted)/float64(time.Millisecond), float64(elapsed)/float64(time.Millisecond), float64(elapsed-admitted)/float64(time.Millisecond), calls.Load(), worker, settled.Submission.Status, waitErr)
		if waitErr != nil {
			state, stateErr := h.Snapshot(bg)
			t.Fatalf("admission=%d calls=%d worker=%v seq=%d entries=%d tasks=%d snapshotErr=%v: %v", k, calls.Load(), worker, state.Seq, len(state.Entries), len(state.Tasks), stateErr, waitErr)
		}
		if settled.Submission.Status != "done" || settled.Message == nil || len(settled.Message.Content) != 1 || settled.Message.Content[0].Text != "ok" || settled.Task.Status != "done" || calls.Load() != int64(k+1) {
			t.Fatal("wrong/stranded settlement", k, settled, calls.Load())
		}
	}
	if calls.Load() != 20 {
		t.Fatal(calls.Load())
	}
}

func TestSequentialAdmissionAtTerminalDrainAndEmptyWorker(t *testing.T) {
	var calls atomic.Int64
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		calls.Add(1)
		ch := make(chan goai.Event, 1)
		ch <- terminal("ok")
		close(ch)
		return ch
	})
	store, e := NewMemory()
	if e != nil {
		t.Fatal(e)
	}
	h := openHarness(t, store, options)
	r := root(t, h, ref)
	terminalEntered, release := make(chan struct{}), make(chan struct{})
	var gateOnce, releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	store.appendFrame = func(typ byte, ordinal, high uint64, payload []byte) error {
		if typ == 2 && bytes.Contains(payload, []byte(`"phase":"terminal"`)) {
			gateOnce.Do(func() { close(terminalEntered); <-release })
		}
		return nil
	}
	first, e := r.Submit(bg, Input{Content: "first"})
	if e != nil {
		t.Fatal(e)
	}
	<-terminalEntered
	h.mu.Lock()
	active := h.workers[r.ID()]
	h.mu.Unlock()
	if !active {
		t.Fatal("worker exited before terminal settlement")
	}
	secondResult := make(chan *SubmissionHandle, 1)
	secondError := make(chan error, 1)
	go func() {
		second, e := r.Submit(bg, Input{Content: "late-terminal"})
		if e != nil {
			secondError <- e
			return
		}
		secondResult <- second
	}()
	releaseOnce.Do(func() { close(release) })
	var second *SubmissionHandle
	select {
	case e := <-secondError:
		t.Fatal(e)
	case second = <-secondResult:
	case <-time.After(3 * time.Second):
		t.Fatal("late admission blocked")
	}
	if waitSubmission(t, first).Submission.Status != "done" || waitSubmission(t, second).Submission.Status != "done" {
		t.Fatal("late terminal admission stranded")
	}
	// Wait for the explicit worker exit signal under its same mutex; then admit a
	// new request into a known empty-worker state. No arbitrary sleep or seam in
	// the public Harness API is required.
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	for {
		h.mu.Lock()
		active = h.workers[r.ID()]
		signal := h.changed
		h.mu.Unlock()
		if !active {
			break
		}
		select {
		case <-signal:
		case <-ctx.Done():
			t.Fatal("worker never drained")
		}
	}
	third, e := r.Submit(bg, Input{Content: "after-empty"})
	if e != nil {
		t.Fatal(e)
	}
	if waitSubmission(t, third).Submission.Status != "done" || calls.Load() != 3 {
		t.Fatal("empty-worker admission stranded", calls.Load())
	}
}
