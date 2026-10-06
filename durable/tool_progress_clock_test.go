package durable

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type progressTestClock struct {
	mu       sync.Mutex
	time     time.Time
	requests chan time.Duration
	ticks    chan time.Time
}

func (c *progressTestClock) now() time.Time                         { c.mu.Lock(); defer c.mu.Unlock(); return c.time }
func (c *progressTestClock) after(d time.Duration) <-chan time.Time { c.requests <- d; return c.ticks }
func (c *progressTestClock) advance(d time.Duration) {
	c.mu.Lock()
	c.time = c.time.Add(d)
	now := c.time
	c.mu.Unlock()
	c.ticks <- now
}
func TestToolProgressPinnedAdaptiveScheduleCoalescenceStopAndErrors(t *testing.T) {
	clock := &progressTestClock{time: time.Unix(0, 0), requests: make(chan time.Duration, 8), ticks: make(chan time.Time, 8)}
	started, release := make(chan int, 4), make(chan struct{})
	calls := 0
	p := newToolProgressClock(func() (int, error) {
		calls++
		started <- calls
		if calls == 1 {
			<-release
			return 50 * 1024, nil
		}
		return 10, nil
	}, nil, clock.now, clock.after)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		p.stop()
	})
	first := p.mark(true)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("first idle commit not immediate")
	}
	second := p.mark(true)
	p.mark(false)
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	select {
	case delay := <-clock.requests:
		if delay != 500*time.Millisecond {
			t.Fatal("size-based delay", delay)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("delay absent")
	}
	select {
	case <-second:
		t.Fatal("coalesced waiter settled early")
	default:
	}
	clock.advance(500 * time.Millisecond)
	select {
	case n := <-started:
		if n != 2 {
			t.Fatal(n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("coalesced write absent")
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	third := p.mark(true)
	select {
	case delay := <-clock.requests:
		if delay != 100*time.Millisecond {
			t.Fatal("minimum delay", delay)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("minimum delay absent")
	}
	pending := p.stop()
	if len(pending) != 1 {
		t.Fatal("stop pending waiters", len(pending))
	}
	settleProgress(pending, nil)
	if err := <-third; err != nil {
		t.Fatal(err)
	}
	failure := errors.New("write rejected")
	reported := make(chan error, 1)
	bad := newToolProgress(func() (int, error) { return 0, failure }, func(err error) { reported <- err })
	defer bad.stop()
	waiter := bad.mark(true)
	if err := <-waiter; !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if err := <-reported; !errors.Is(err, failure) {
		t.Fatal(err)
	}
}
