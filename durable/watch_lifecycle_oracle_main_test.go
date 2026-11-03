package durable

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// Object-valued inputs from chord/test/watch.test.ts:91-124,154-180 at
// 81f3cd412758ebdd92324c7d63b669a189408d0a. Source gates are preserved;
// Promise/microtask timing and JS identity are NOT replaced with Go barriers.
func TestMainWatchUpstreamValueOracles(t *testing.T) {
	for _, mode := range []string{"acquisition", "inflight"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				s, err := OpenSession(b.store)
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close(bg)
				initial := JSON{"text": ""}
				if mode == "acquisition" {
					initial["n"] = 0
				}
				def := upstreamWatchDefinition(t, s, initial)
				w := watchAcquire(t, s, def)
				defer w.Stop()
				calls := make(chan WatchFrame, 4)
				gate := make(chan struct{})
				var release sync.Once
				defer release.Do(func() { close(gate) })
				ended := make(chan struct{}, 1)
				listener := func(_ context.Context, f WatchFrame) error {
					calls <- f
					if mode == "inflight" && f.Value["text"] == "a" {
						<-gate
						ended <- struct{}{}
					}
					return nil
				}
				if mode == "acquisition" {
					upstreamWatchApply(t, s, def, []Operation{{"s", []any{"n"}, 1}})
					upstreamWatchApply(t, s, def, []Operation{{"s", []any{"n"}, 2}, {"a", []any{"text"}, "a"}})
					upstreamWatchApply(t, s, def, []Operation{{"a", []any{"text"}, "b"}})
					value, err := w.Value()
					if err != nil || !upstreamWatchEqual(t, value, initial) {
						t.Fatal("acquisition value", value, err)
					}
					if err = w.StartFrame(listener); err != nil {
						t.Fatal(err)
					}
					f := watchReceive(t, calls)
					if !upstreamWatchEqual(t, f.Value, JSON{"n": 2, "text": "ab"}) {
						t.Fatal(f)
					}
					assertConversationOperations(t, f.Ops, []Operation{{"s", []any{"n"}, 2}, {"a", []any{"text"}, "ab"}}, s.limits)
				} else {
					if err := w.StartFrame(listener); err != nil {
						t.Fatal(err)
					}
					upstreamWatchApply(t, s, def, []Operation{{"a", []any{"text"}, "a"}})
					first := watchReceive(t, calls)
					if !upstreamWatchEqual(t, first.Value, JSON{"text": "a"}) {
						t.Fatal(first)
					}
					upstreamWatchApply(t, s, def, []Operation{{"a", []any{"text"}, "b"}})
					upstreamWatchApply(t, s, def, []Operation{{"a", []any{"text"}, "c"}})
					select {
					case f := <-calls:
						t.Fatal("concurrent callback", f)
					default:
					}
					release.Do(func() { close(gate) })
					second := watchReceive(t, calls)
					// The next call cannot precede the first callback's completion.
					select {
					case <-ended:
					default:
						t.Fatal("callback order")
					}
					if !upstreamWatchEqual(t, second.Value, JSON{"text": "abc"}) {
						t.Fatal(second)
					}
					assertConversationOperations(t, second.Ops, []Operation{{"a", []any{"text"}, "bc"}}, s.limits)
				}
				value, err := w.Value()
				want := JSON{"text": "abc"}
				if mode == "acquisition" {
					want = JSON{"n": 2, "text": "ab"}
				}
				if err != nil || !upstreamWatchEqual(t, value, want) {
					t.Fatal("delivered value", value, err)
				}
				w.mu.Lock()
				pending := len(w.pending)
				w.mu.Unlock()
				if pending != 0 || len(calls) != 0 {
					t.Fatal("unexpected extra frames", pending, len(calls))
				}
			})
		})
	}
}

// chord/test/watch.test.ts:708-720,737-756,776-798. Scalar 0/1/2 inputs
// use the native object envelope {n:0/1/2}. Error text and terminal assertions
// are retained; Go has no JS Promise identity or catchable non-Error throws.
func TestMainWatchUpstreamLifecycleOracles(t *testing.T) {
	for _, mode := range []string{"stop", "listener-error"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				reports := make(chan error, 4)
				h := taskTestHarnessOptions(t, b.store, Options{OnReport: func(err error) { reports <- err }})
				s := h.session
				def := upstreamWatchDefinition(t, s, JSON{"n": 0})
				w := watchAcquire(t, s, def)
				defer w.Stop()
				failure := errors.New("listener failed")
				gate := make(chan struct{})
				var release sync.Once
				defer release.Do(func() { close(gate) })
				calls := make(chan WatchFrame, 4)
				returned := make(chan struct{})
				if err := w.StartFrame(func(ctx context.Context, f WatchFrame) error {
					calls <- f
					<-gate
					if ctx.Err() != nil {
						t.Error("stop aborted callback context", ctx.Err())
					}
					close(returned)
					if mode == "listener-error" {
						return failure
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				if err := w.StartFrame(func(context.Context, WatchFrame) error { return nil }); err == nil {
					t.Fatal("repeated start accepted")
				}
				upstreamWatchApply(t, s, def, []Operation{{"r", map[string]any{"n": 1}}})
				f := watchReceive(t, calls)
				if !upstreamWatchEqual(t, f.Value, JSON{"n": 1}) {
					t.Fatal(f)
				}
				upstreamWatchApply(t, s, def, []Operation{{"r", map[string]any{"n": 2}}})
				if mode == "stop" {
					first, second := w.Stop(), w.Stop()
					if first.Reason != "stopped" || second != first {
						t.Fatal(first, second)
					}
					watchReceive(t, w.Closed()) // Stop must complete with the callback still blocked.
					select {
					case <-returned:
						t.Fatal("callback unexpectedly finished")
					default:
					}
				}
				release.Do(func() { close(gate) })
				watchReceive(t, returned)
				watchReceive(t, w.Closed())
				end, closed := w.End()
				if !closed {
					t.Fatal("not ended")
				}
				if stopped := w.Stop(); stopped != end {
					t.Fatal("terminal cause changed", end, stopped)
				}
				if mode == "listener-error" {
					if end.Reason != "listener_error" || end.Error != failure {
						t.Fatal(end)
					}
					if got := watchReceive(t, reports); got != failure {
						t.Fatal("reported error", got)
					}
				}
				if len(calls) != 0 || len(reports) != 0 {
					t.Fatal("extra delivery/report")
				}
				s.observerMu.Lock()
				_, attached := s.watches[w]
				s.observerMu.Unlock()
				if attached {
					t.Fatal("watch not detached")
				}
				if err := w.StartFrame(func(context.Context, WatchFrame) error { return nil }); !errors.Is(err, ErrClosed) {
					t.Fatal("late start", err)
				}
				late := watchAcquire(t, s, def)
				late.Stop()
				if err := late.StartFrame(func(context.Context, WatchFrame) error { return nil }); !errors.Is(err, ErrClosed) {
					t.Fatal("unstarted late start", err)
				}
			})
		})
	}
}

func upstreamWatchDefinition(t *testing.T, s *Session, initial JSON) *DocumentDefinition {
	t.Helper()
	def := checkpointDef(t, DefinitionOptions{Kind: "watch.oracle", Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) { return initial, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
	checkpointCommit(t, s, func(tx *Tx) error { _, err := tx.AcquireDocument(def, 0, nil, nil); return err })
	return def
}
func upstreamWatchApply(t *testing.T, s *Session, def *DocumentDefinition, ops []Operation) {
	t.Helper()
	checkpointCommit(t, s, func(tx *Tx) error {
		doc, err := tx.AcquireDocument(def, 0, nil, nil)
		if err != nil {
			return err
		}
		return doc.ApplyOperations(ops)
	})
}

func upstreamWatchEqual(t *testing.T, got, want JSON) bool {
	t.Helper()
	a, err := ownJSONValue(got, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	b, err := ownJSONValue(want, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return equalDeltaJSON(a, b)
}
