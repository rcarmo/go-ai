package durable

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// Signal actual evaluation of the admission select (or Close's final select),
// without sleeps or a goroutine-start-only witness. The underlying context's
// cancellation/Err contracts are unchanged.
type upstreamAdmissionContext struct {
	context.Context
	once      sync.Once
	evaluated chan struct{}
}

func (c *upstreamAdmissionContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.evaluated) })
	return c.Context.Done()
}

func holdUpstreamSession(t *testing.T, s *Session) (func(), <-chan error) {
	t.Helper()
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	go func() { _, err := s.Commit(bg, func(*Tx) error { close(entered); <-release; return nil }); done <- err }()
	watchReceive(t, entered)
	return unblock, done
}

// Pinned harness-view.test.ts:295-328. Native end/error tags are explicitly
// adapted below; the source's literal ended/diagnostic assertions remain open.
func TestMainViewUpstreamAcquisitionOracles(t *testing.T) {
	for _, mode := range []string{"close-late", "queued-cancel-close"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				h, c := transcriptFixture(t, b)
				if mode == "close-late" {
					w, err := detachedWatch(c, bg)
					if err != nil {
						t.Fatal(err)
					}
					if err = h.Close(bg); err != nil {
						t.Fatal(err)
					}
					watchReceive(t, w.Closed())
					end, closed := w.End()
					// Source {kind:ended} has a distinct native Session-origin tag.
					if !closed || end.Reason != "session_closed" || end.Error != nil {
						t.Fatal("native closed end", end, closed)
					}
					value, err := w.Value()
					if err != nil {
						t.Fatal(err)
					}
					if value.Entries == nil || len(value.Entries) != 0 {
						t.Fatal("source retained empty entries array", value.Entries)
					}
					late, err := detachedWatch(c, bg)
					if late != nil || !errors.Is(err, ErrClosed) {
						t.Fatal("late acquisition", late, err)
					}
					return
				}
				type result struct {
					w   *detachedConversationWatch
					err error
				}
				acquire := func(ctx context.Context) <-chan result {
					ch := make(chan result, 1)
					go func() {
						w, err := detachedWatch(c, ctx)
						if w != nil {
							w.Stop()
						}
						ch <- result{w, err}
					}()
					return ch
				}
				release, blocking := holdUpstreamSession(t, h.session)
				cancelCtx, cancel := context.WithCancelCause(bg)
				defer cancel(nil)
				queued := &upstreamAdmissionContext{Context: cancelCtx, evaluated: make(chan struct{})}
				cancelled := acquire(queued)
				watchReceive(t, queued.evaluated)
				cause := errors.New("cancelled")
				cancel(cause)
				release()
				if err := watchReceive(t, blocking); err != nil {
					t.Fatal(err)
				}
				got := watchReceive(t, cancelled)
				// Source harness-view:319 checks the supplied "cancelled" error,
				// not a replacement generic cancellation sentinel.
				if got.w != nil || got.err != cause {
					t.Fatal("queued cancellation", got.w, got.err)
				}

				release, blocking = holdUpstreamSession(t, h.session)
				queued = &upstreamAdmissionContext{Context: bg, evaluated: make(chan struct{})}
				closedWhileQueued := acquire(queued)
				watchReceive(t, queued.evaluated)
				closeCtx := &upstreamAdmissionContext{Context: bg, evaluated: make(chan struct{})}
				closing := make(chan error, 1)
				go func() { closing <- h.Close(closeCtx) }()
				// Close evaluates its final select after synchronously setting closing.
				watchReceive(t, closeCtx.evaluated)
				release()
				if err := watchReceive(t, blocking); err != nil {
					t.Fatal(err)
				}
				got = watchReceive(t, closedWhileQueued)
				if got.w != nil || !errors.Is(got.err, ErrClosed) {
					t.Fatal("queued close", got.w, got.err)
				}
				if err := watchReceive(t, closing); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}
