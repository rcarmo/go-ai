package durable

import (
	"math"
	"math/big"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

type mainContextTimerArm struct {
	delay    time.Duration
	callback func()
	canceled atomic.Int64
}
type mainContextTimers struct {
	mu         sync.Mutex
	calls      []*mainContextTimerArm
	decline    atomic.Bool
	onFirstArm func()
}

func (r *mainContextTimers) set(callback func(), delay time.Duration, options TimerOptions) func() {
	if !options.Background {
		panic("expected background context timer")
	}
	arm := &mainContextTimerArm{delay: delay, callback: callback}
	r.mu.Lock()
	r.calls = append(r.calls, arm)
	first := len(r.calls) == 1
	r.mu.Unlock()
	if first && r.onFirstArm != nil {
		r.onFirstArm()
	}
	if r.decline.Load() {
		return nil
	}
	return func() { arm.canceled.Add(1) }
}
func (r *mainContextTimers) arms() []*mainContextTimerArm {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*mainContextTimerArm(nil), r.calls...)
}

func TestMainContextTimerDelayArithmetic(t *testing.T) {
	random := rand.New(rand.NewPCG(7, 42))
	cases := [][3]int64{
		{1000, 1000, 500}, {math.MinInt64, math.MaxInt64, -2},
		{math.MaxInt64, math.MaxInt64, math.MinInt64},
		{math.MaxInt64 - 1000, 2000, math.MaxInt64 - 500},
		{0, math.MaxInt64 / int64(time.Millisecond), 0},
	}
	for range 1000 {
		cases = append(cases, [3]int64{int64(random.Uint64()), max(1, random.Int64()), int64(random.Uint64())})
	}
	for _, c := range cases {
		// Arbitrary precision is independent of the production unsigned-gap
		// arithmetic and catches overflow before duration saturation.
		left := new(big.Int).Add(big.NewInt(c[0]), big.NewInt(c[1]))
		left.Sub(left, big.NewInt(c[2]))
		want := time.Millisecond // already-due defensive fallback
		if left.Sign() > 0 {
			left.Mul(left, big.NewInt(int64(time.Millisecond)))
			if left.Cmp(big.NewInt(math.MaxInt64)) > 0 {
				want = time.Duration(math.MaxInt64)
			} else {
				want = time.Duration(left.Int64())
			}
		}
		if got := contextExpiryDelay(c[0], c[1], c[2]); got != want {
			t.Fatalf("idle/retention/now=%v: delay=%v want=%v", c, got, want)
		}
	}
}

// Mirrors scheduler.ts:991-1005 at 81f3cd41. Overflow, capped delays and
// already-admitted stale callbacks are native safety controls.
func TestMainContextTimerDeadlines(t *testing.T) {
	for _, mode := range []string{"unchanged", "rollback-fire", "same-deadline", "settings-fire", "decline-retry", "overflow", "stale-admitted", "reentrant-arm"} {
		t.Run(mode, func(t *testing.T) {
			utf16Backends(t, func(t *testing.T, b backend) {
				other, entry, otherEntry := mint(t, b.store), mint(t, b.store), mint(t, b.store)
				a := contextEntry(entry, 1, goai.RoleUser, "first")
				z := contextEntry(otherEntry, other, goai.RoleUser, "second")
				apply(t, b.store, Write{Op: "create-conversation", Conversation: &Conversation{ID: other}}, Write{Op: "append-entry", Entry: &a}, Write{Op: "append-entry", Entry: &z})
				var clock atomic.Int64
				clock.Store(1000)
				if mode == "overflow" {
					clock.Store(math.MaxInt64 - 1000)
				}
				timers := &mainContextTimers{}
				timers.decline.Store(mode == "decline-retry")
				retention := int64(1000)
				h := taskTestHarnessOptions(t, b.store, Options{Now: clock.Load, Settings: &HarnessSettings{ContextRetentionMs: &retention}, SetTimer: timers.set})
				read := func(id ID) {
					t.Helper()
					if _, err := h.session.ContextView(bg, id, 0); err != nil {
						t.Fatal(err)
					}
				}
				settle := func() {
					t.Helper()
					if err := h.scheduler.reconcile(); err != nil {
						t.Fatal(err)
					}
				}
				settings := func(ms int64) {
					t.Helper()
					if err := h.SetSettings(bg, HarnessSettings{ContextRetentionMs: &ms}); err != nil {
						t.Fatal(err)
					}
				}
				expect := func(n int, delay time.Duration) []*mainContextTimerArm {
					t.Helper()
					arms := timers.arms()
					if len(arms) != n {
						t.Fatalf("host arms=%d want=%d", len(arms), n)
					}
					if arms[n-1].delay != delay {
						t.Fatalf("last delay=%v want=%v", arms[n-1].delay, delay)
					}
					return arms
				}
				if mode == "reentrant-arm" {
					timers.onFirstArm = func() { read(1) }
				}
				read(1)
				expect(1, time.Second)
				switch mode {
				case "unchanged":
					clock.Store(1500)
					read(1)
					settle()
					expect(1, time.Second)
				case "rollback-fire":
					clock.Store(500)
					settle()
					arms := expect(1, time.Second)
					arms[0].callback()
					expect(2, 1500*time.Millisecond)
					h.session.taskBookkeeping(func() {
						if h.session.contextRanges[1] == nil {
							t.Fatal("early timer evicted range")
						}
					})
				case "same-deadline":
					clock.Store(1500)
					read(other)
					settings(500)
					expect(1, time.Second)
					h.session.taskBookkeeping(func() {
						if h.session.contextRanges[1] != nil || h.session.contextRanges[other] == nil {
							t.Fatal("wrong retained ranges at equal deadline")
						}
					})
				case "settings-fire":
					clock.Store(1200)
					settings(2000)
					arms := expect(2, 1800*time.Millisecond)
					arms[0].callback()
					expect(2, 1800*time.Millisecond)
					clock.Store(3000)
					arms[1].callback()
					arms[1].callback()
					expect(2, 1800*time.Millisecond)
					h.session.taskBookkeeping(func() {
						if len(h.session.contextRanges) != 0 || h.session.contextExpiry != nil {
							t.Fatal("fired alarm was retained")
						}
					})
				case "decline-retry":
					settle()
					expect(2, time.Second)
					timers.decline.Store(false)
					settle()
					expect(3, time.Second)
					settle()
					expect(3, time.Second)
				case "overflow":
					settings(2000)
					arms := expect(2, 2*time.Second)
					clock.Store(math.MaxInt64 - 500)
					settle()
					expect(2, 2*time.Second)
					arms[1].callback()
					arms = expect(3, 1500*time.Millisecond)
					clock.Store(math.MinInt64)
					arms[2].callback()
					arms = expect(4, time.Duration(math.MaxInt64))
					arms[3].callback()
					expect(5, time.Duration(math.MaxInt64))
				case "stale-admitted":
					var admitted func()
					h.session.taskBookkeeping(func() { admitted = h.session.contextExpiry.callback })
					settings(2000)
					arms := expect(2, 2*time.Second)
					// Models a callback that passed its off-line liveness check before a
					// replacement acquired the Session line. The on-line witness must reject it.
					admitted()
					expect(2, 2*time.Second)
					if arms[1].canceled.Load() != 0 {
						t.Fatal("stale callback canceled replacement")
					}
					if err := h.Close(bg); err != nil {
						t.Fatal(err)
					}
					admitted()
					arms[1].callback()
					expect(2, 2*time.Second)
				}
				if err := h.Close(bg); err != nil {
					t.Fatal(err)
				}
				arms := timers.arms()
				for i, arm := range arms {
					want := int64(1)
					if mode == "decline-retry" && i < 2 {
						want = 0
					}
					if arm.canceled.Load() != want {
						t.Fatalf("arm %d cancellations=%d want=%d", i, arm.canceled.Load(), want)
					}
					arm.callback() // Late host callbacks after Close remain inert.
				}
				if len(timers.arms()) != len(arms) {
					t.Fatal("late callback rearmed closed session")
				}
			})
		})
	}
}
