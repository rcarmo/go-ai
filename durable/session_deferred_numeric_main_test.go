package durable

import (
	"errors"
	"math"
	"strconv"
	"testing"
	"time"
)

// Official v1.5.0 f9a26365 session.ts:173-187,517-525,1223,1297-1323:
// nonnegative Number windows, earliest clock+window deadline, and nonnegative
// remaining delay. These are source-derived edge controls, not additional full
// upstream test bodies. Full-width clocks extend the source Number domain to
// the existing native int64 clock contract; they must not lose small windows.
func TestMainSessionDeferredNumericWindow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		window float64
		delay  time.Duration
	}{
		{"zero", 0, 0},
		{"negative-zero", math.Copysign(0, -1), 0},
		{"quarter", .25, 250 * time.Microsecond},
		{"tenth", .1, 100 * time.Microsecond},
		{"sub-nanosecond", math.SmallestNonzeroFloat64, 0},
		// The last whole millisecond below the duration ceiling is exact.
		{"ceiling-whole-ms", 9223372036854, 9223372036854000000},
		{"above-ceiling", 9223372036855, time.Duration(math.MaxInt64)},
		{"largest-finite", math.MaxFloat64, time.Duration(math.MaxInt64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				f := openMainDeadlineSession(t, b, nil)
				f.clock.Store(0)
				before := mainDeferredSnapshot(t, b.store).Seq
				if err := f.append(f.doc, "a", tc.window); err != nil {
					t.Fatal(err)
				}
				f.assertArmed(t, tc.delay)
				if mainDeferredSnapshot(t, b.store).Seq != before {
					t.Fatal("window caused immediate storage")
				}
				f.fire(t) // A host fire stores holds even before the clock advances.
				after := mainDeferredSnapshot(t, b.store)
				if after.Seq != before+1 || !equalJSONValue(after.Documents[f.doc].Value, JSON{"text": "a"}) {
					t.Fatal("numeric timer did not store exactly one held value", after)
				}
			})
		})
	}
}

func TestMainSessionDeferredNumericClock(t *testing.T) {
	for _, clock := range []int64{1<<53 + 1, math.MaxInt64 - 1, math.MinInt64} {
		t.Run(strconv.FormatInt(clock, 10), func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				f := openMainDeadlineSession(t, b, nil)
				f.clock.Store(clock)
				if err := f.append(f.doc, "a", 1.25); err != nil {
					t.Fatal(err)
				}
				f.assertArmed(t, 1250*time.Microsecond)
				first := f.active()[0]
				f.clock.Add(1)
				if err := f.append(f.doc, "b", 100); err != nil {
					t.Fatal(err)
				}
				if f.active()[0] != first {
					t.Fatal("later deadline rearmed earliest hold")
				}
				if err := f.append(f.doc, "c", .125); err != nil {
					t.Fatal(err)
				}
				f.assertArmed(t, 125*time.Microsecond)
				f.fire(t)
				if value := mainDeferredSnapshot(t, b.store).Documents[f.doc].Value; !equalJSONValue(value, JSON{"text": "abc"}) {
					t.Fatal("full-width deadline lost held changes", value)
				}
			})
		})
	}
}

// Preserve Source Number addition rounding in its safe-integer clock domain;
// extending native wide clocks must not silently change normal JS arithmetic.
func TestMainSessionDeferredNumericSourceNumber(t *testing.T) {
	for _, tc := range []struct {
		clock  int64
		window float64
		delay  time.Duration
	}{
		{1700000000000, .1, 100097},
		{1000, math.SmallestNonzeroFloat64, 0},
	} {
		t.Run(strconv.FormatInt(tc.clock, 10), func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				f := openMainDeadlineSession(t, b, nil)
				f.clock.Store(tc.clock)
				if err := f.append(f.doc, "a", tc.window); err != nil {
					t.Fatal(err)
				}
				f.assertArmed(t, tc.delay)
			})
		})
	}
}

func TestMainSessionDeferredNumericPolicy(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		window := .125
		f := openMainDeadlineSession(t, b, &window)
		if err := f.append(f.doc, "a"); err != nil {
			t.Fatal(err)
		}
		f.assertArmed(t, 125*time.Microsecond)
		for _, invalid := range []float64{math.Inf(-1), -math.SmallestNonzeroFloat64} {
			called := false
			_, err := f.session.commitPersistence(bg, func(*Tx) error { called = true; return nil }, true, 0, true, invalid)
			var rejected *StorageRejected
			if !errors.As(err, &rejected) || called {
				t.Fatal("invalid explicit window reached callback or wrong error", invalid, err, called)
			}
		}
		// Invalid configured defaults fall back to 1000ms. Store the previous
		// hold first so its earlier deadline cannot win the next minimum.
		for _, invalid := range []float64{math.Inf(-1), math.NaN()} {
			if _, err := f.session.Commit(bg, func(tx *Tx) (any, error) { return nil, mainDeferredAppend(f.doc, "stored")(tx) }); err != nil {
				t.Fatal(err)
			}
			window = invalid
			if err := f.append(f.doc, "b"); err != nil {
				t.Fatal(err)
			}
			f.assertArmed(t, time.Second)
		}
	})
}
