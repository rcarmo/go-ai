package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
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
	if _, e = r.Submit(bg, Input{Content: "steer", Type: "steer"}); !errors.Is(e, ErrUnsupported) {
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
		sub, e := r.Submit(bg, Input{Content: "next"})
		if e != nil {
			t.Fatal(e)
		}
		if waitSubmission(t, sub).Submission.Status != "done" {
			t.Fatal("queue stranded")
		}
	}
	if calls.Load() != 20 {
		t.Fatal(calls.Load())
	}
}
