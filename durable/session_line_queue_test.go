package durable

import (
	"context"
	"errors"
	"testing"
)

// queued starts `name` waiting on the line and returns once it is actually in the
// queue, so arrival order is the order the line must grant in.
func queued(t *testing.T, l *sessionLine, name string, grants chan<- string) {
	t.Helper()
	before := l.pending()
	go func() {
		if err := l.acquire(bg); err != nil {
			t.Errorf("%s acquire: %v", name, err)
			return
		}
		grants <- name
		l.release()
	}()
	l.waitPending(before + 1)
}

func TestSessionLineGrantsInAdmissionOrder(t *testing.T) {
	l := newSessionLine()
	if err := l.acquire(bg); err != nil {
		t.Fatal(err)
	}

	grants := make(chan string, 4)
	for _, name := range []string{"first", "second", "third"} {
		queued(t, l, name, grants)
	}

	// One release grants exactly the head of the queue, so the grants must arrive in
	// admission order rather than in whatever order three goroutines race.
	l.release()
	for _, want := range []string{"first", "second", "third"} {
		if got := <-grants; got != want {
			t.Fatalf("line admitted out of order: got %q, want %q", got, want)
		}
	}
	if !l.free {
		t.Fatal("line must be free after every job released")
	}
}

// The failure mode a one-slot token has: a job that gives the line up can be
// overtaken by a job admitted later. Granting must follow the queue, not the race.
func TestSessionLineHeadWaiterIsNotJumped(t *testing.T) {
	l := newSessionLine()
	if err := l.acquire(bg); err != nil {
		t.Fatal(err)
	}
	grants := make(chan string, 2)
	queued(t, l, "B", grants)
	queued(t, l, "C", grants)

	l.release()
	if got := <-grants; got != "B" {
		t.Fatalf("head waiter must be granted first, got %q", got)
	}
	if got := <-grants; got != "C" {
		t.Fatalf("queue order lost: second grant was %q", got)
	}
}

func TestSessionLineAcquireHonoursCancellation(t *testing.T) {
	l := newSessionLine()
	if err := l.acquire(bg); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(bg)
	failed := make(chan error, 1)
	go func() { failed <- l.acquire(ctx) }()
	l.waitPending(1)
	cancel()
	if err := <-failed; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled acquire: %v", err)
	}
	if l.pending() != 0 {
		t.Fatal("canceled waiter must leave the queue")
	}

	l.release()
	if err := l.acquire(bg); err != nil {
		t.Fatalf("line stranded after cancellation: %v", err)
	}
	l.release()
}

// A cancellation racing the handoff must still return the token, otherwise the line
// is stranded forever with nobody holding it.
func TestSessionLineCancellationDuringHandoff(t *testing.T) {
	for i := 0; i < 500; i++ {
		l := newSessionLine()
		if err := l.acquire(bg); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(bg)
		got := make(chan error, 1)
		go func() { got <- l.acquire(ctx) }()
		for l.pending() == 0 {
			continue
		}
		// Release and cancel concurrently: exactly one of them wins the race, and
		// either way the token must survive.
		go l.release()
		cancel()
		// Either acquire won (the goroutine now owns the line and must return it) or
		// cancellation won. Both are legitimate; losing the token is not.
		if err := <-got; err == nil {
			l.release()
		} else if !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected acquire error: %v", err)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			if err := l.acquire(bg); err != nil {
				t.Errorf("round %d: %v", i, err)
				return
			}
			l.release()
		}()
		<-done
		if !l.free {
			t.Fatal("line not free after the handoff race")
		}
	}
}
