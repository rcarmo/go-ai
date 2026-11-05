package durable

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"testing"
)

// Source session-watches.test.ts acquisition/start, blocked callbacks, 101
// repeated sets and retirement. The blocked call must never enter the fold.
func TestMainWatchPendingDelivery(t *testing.T) {
	for _, mode := range []string{"before-start", "blocked", "retired"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				s, e := OpenSession(b.store)
				if e != nil {
					t.Fatal(e)
				}
				defer s.Close(bg)
				def := watchDoc(t, s)
				w := watchAcquire(t, s, def)
				defer w.Stop()
				entered, release := make(chan struct{}), make(chan struct{})
				defer func() {
					select {
					case <-release:
					default:
						close(release)
					}
				}()
				calls := make(chan WatchFrame, 4)
				listener := func(_ context.Context, f WatchFrame) error {
					calls <- f
					if mode == "blocked" && f.Value["n"].(json.Number) == "1" {
						close(entered)
						<-release
					}
					return nil
				}
				if mode == "blocked" {
					if e = w.StartFrame(listener); e != nil {
						t.Fatal(e)
					}
					watchSet(t, s, def, 1)
					watchReceive(t, entered)
					if f := watchReceive(t, calls); f.Value["n"].(json.Number) != "1" {
						t.Fatal(f)
					}
				}
				from := 1
				if mode == "blocked" {
					from = 2
				}
				type contextKey struct{}
				producer, cancel := context.WithCancel(context.WithValue(bg, contextKey{}, "latest"))
				defer cancel()
				// The active callback above owns 1; only this pending batch can fold.
				for n := from; n < 102; n++ {
					watchSet(t, s, def, n)
				}
				_, e = s.Commit(producer, func(tx *Tx) error {
					h, err := tx.AcquireDocument(def, 0, nil, nil)
					if err != nil {
						return err
					}
					return h.ApplyOperations([]Operation{{"s", []any{"n"}, 102}})
				})
				if e != nil {
					t.Fatal(e)
				}
				if mode == "retired" {
					if _, e = s.Commit(producer, func(tx *Tx) error { return tx.RetireDefinition(def, 0, nil) }); e != nil {
						t.Fatal(e)
					}
				}
				cancel()
				if mode == "blocked" {
					close(release)
				} else {
					v, e := w.Value()
					if e != nil || v["n"].(json.Number) != "0" {
						t.Fatal("advanced before start", v, e)
					}
					if e = w.StartFrame(listener); e != nil {
						t.Fatal(e)
					}
				}
				f := watchReceive(t, calls)
				if f.Context.Value(contextKey{}) != "latest" || f.Context.Err() != nil {
					t.Fatal("fold lost latest context or inherited cancellation")
				}
				if f.Kind != "value" || f.Value["n"].(json.Number) != "102" || len(f.Ops) != 1 || f.Ops[0][0] != "s" || f.Retired != (mode == "retired") {
					t.Fatal("pending fold", f)
				}
				w.mu.Lock()
				pending := len(w.pending)
				w.mu.Unlock()
				if pending != 0 {
					t.Fatal("pending frames delivered separately", pending)
				}
				if mode == "retired" {
					checkpointCommit(t, s, func(tx *Tx) error { _, e := tx.AcquireDocument(def, 0, nil, nil); return e })
					f = watchReceive(t, calls)
					if f.Retired || len(f.Ops) != 1 || f.Ops[0][0] != "r" {
						t.Fatal("recreation after folded retirement", f)
					}
				}
				watchSet(t, s, def, 103)
				f = watchReceive(t, calls)
				if f.Retired || f.Value["n"].(json.Number) != "103" {
					t.Fatal("subsequent frame", f)
				}
			})
		})
	}
}

func TestMainWatchPendingOperationFolding(t *testing.T) {
	cases := []struct {
		name    string
		base    JSON
		batches [][]Operation
		want    []Operation
	}{
		{"string-append", JSON{"s": ""}, [][]Operation{{{"s", []any{"s"}, "a"}}, {{"a", []any{"s"}, "b"}}, {{"a", []any{"s"}, "c"}}}, []Operation{{"s", []any{"s"}, "abc"}}},
		{"append-append", JSON{"s": "a"}, [][]Operation{{{"a", []any{"s"}, "b"}}, {{"a", []any{"s"}, "c"}}}, []Operation{{"a", []any{"s"}, "bc"}}},
		{"paths-not-joined", JSON{"a.b": 0, "a": map[string]any{"b": 0}}, [][]Operation{{{"s", []any{"a.b"}, 1}}, {{"s", []any{"a", "b"}, 2}}, {{"s", []any{"a.b"}, 3}}}, []Operation{{"s", []any{"a.b"}, 3}, {"s", []any{"a", "b"}, 2}}},
		{"structural-barrier", JSON{"s": "a"}, [][]Operation{{{"s", []any{"s"}, "b"}}, {{"s", []any{"s"}, map[string]any{"x": 0}}}, {{"s", []any{"s", "x"}, 1}}}, []Operation{{"s", []any{"s"}, "b"}, {"s", []any{"s"}, map[string]any{"x": 0}}, {"s", []any{"s", "x"}, 1}}},
		{"root-discards-prefix", JSON{"s": 0}, [][]Operation{{{"s", []any{"s"}, 1}}, {{"r", map[string]any{"s": 2}}}, {{"s", []any{"s"}, 3}}}, []Operation{{"r", map[string]any{"s": 2}}, {"s", []any{"s"}, 3}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limits := DefaultLimits()
			var frames []WatchFrame
			var state any = map[string]any(tc.base)
			for _, batch := range tc.batches {
				owned, e := ownOperations(batch, limits)
				if e != nil {
					t.Fatal(e)
				}
				state, e = ApplyOperations(state, owned, limits)
				if e != nil {
					t.Fatal(e)
				}
				value, e := copyObject(JSON(state.(map[string]any)), limits)
				if e != nil {
					t.Fatal(e)
				}
				frames = append(frames, WatchFrame{Kind: "value", Value: value, Ops: owned, ownedValue: value})
			}
			before, e := json.Marshal(frames)
			if e != nil {
				t.Fatal(e)
			}
			f, e := coalesceWatchFrames(frames, limits, limits.MaxRetainedBytes)
			if e != nil {
				t.Fatal(e)
			}
			want, e := ownOperations(tc.want, limits)
			if e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(f.Ops, want) {
				t.Fatal(f.Ops, want)
			}
			replay, e := ApplyOperations(map[string]any(tc.base), f.Ops, limits)
			if e != nil || !equalDeltaJSON(replay, state) {
				t.Fatal("fold replay", replay, state, e)
			}
			after, e := json.Marshal(frames)
			if e != nil || string(before) != string(after) {
				t.Fatal("input operations mutated", e)
			}
		})
	}
}

func TestMainWatchPendingNativeBounds(t *testing.T) {
	limits := DefaultLimits()
	value := JSON{"n": 0}
	var frames []WatchFrame
	for batch := 0; batch < 3; batch++ {
		count := maxPendingWatchOperations / 2
		if batch == 2 {
			count = 1
		}
		ops := make([]Operation, count)
		for i := range ops {
			ops[i] = Operation{"s", []any{"n"}, map[string]any{"v": strconv.Itoa(batch*count + i)}}
		}
		frames = append(frames, WatchFrame{Kind: "value", Value: value, Ops: ops, ownedValue: value})
	}
	folded, e := coalesceWatchFrames(frames, limits, limits.MaxRetainedBytes)
	if e != nil || len(folded.Ops) != 1 || folded.Ops[0][0] != "r" {
		t.Fatal("native operation limit must replace", folded, e)
	}
	frames = append(frames, WatchFrame{Kind: "value", Value: value, Ops: []Operation{{"r", map[string]any{"n": 0}}}, ownedValue: value})
	folded, e = coalesceWatchFrames(frames, limits, limits.MaxRetainedBytes)
	if e != nil || len(folded.Ops) != 1 || folded.Ops[0][0] != "r" {
		t.Fatal("root must recover overflow", folded, e)
	}
	if _, e = coalesceWatchFrames(frames, limits, 1); e == nil {
		t.Fatal("quota bypass")
	}
	// A kept-up listener gets an exact frame, without operation folding.
	lone := WatchFrame{Kind: "value", Value: value, Ops: []Operation{{"s", []any{"n"}, 0}, {"s", []any{"n"}, 0}}}
	folded, e = coalesceWatchFrames([]WatchFrame{lone}, limits, limits.MaxRetainedBytes)
	if e != nil || !reflect.DeepEqual(folded.Ops, lone.Ops) {
		t.Fatal("single frame folded", folded, e)
	}
}

func TestMainWatchPendingRecreation(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s, e := OpenSession(b.store)
		if e != nil {
			t.Fatal(e)
		}
		defer s.Close(bg)
		def := watchDoc(t, s)
		w := watchAcquire(t, s, def)
		defer w.Stop()
		watchSet(t, s, def, 1)
		checkpointCommit(t, s, func(tx *Tx) error { return tx.RetireDefinition(def, 0, nil) })
		watchSet(t, s, def, 2)
		watchSet(t, s, def, 3)
		frames := make(chan WatchFrame, 2)
		if e = w.StartFrame(func(_ context.Context, f WatchFrame) error { frames <- f; return nil }); e != nil {
			t.Fatal(e)
		}
		f := watchReceive(t, frames)
		if !f.Retired || f.Value["n"].(json.Number) != "3" || len(f.Ops) != 2 || f.Ops[0][0] != "r" || f.Ops[1][0] != "s" {
			t.Fatal("recreation must discard old ops and keep retirement", f)
		}
		replay, e := ApplyOperations(map[string]any{"n": 0, "xs": []any{"a", "b"}}, f.Ops, s.limits)
		if e != nil || !equalDeltaJSON(replay, map[string]any(f.Value)) {
			t.Fatal("recreation replay", replay, e)
		}
	})
}
