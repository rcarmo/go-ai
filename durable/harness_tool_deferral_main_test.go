package durable

import (
	"context"
	"math"
	"reflect"
	"strings"
	"testing"
)

// Source-derived from f9a26365 tool.ts:609-662 PROGRESS_COMMIT and the deferred
// progress test file's storage/failed-Harness rules. This is not another claim
// of executing a complete upstream test body.
func TestMainToolProgressDeferredMemoryAndPersistence(t *testing.T) {
	for _, mode := range []string{"timer", "dependent", "close"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				setup := mainToolRecoverySetup(t, false)
				window := math.Inf(1)
				if mode == "timer" {
					window = 50
				}
				f := mainOpenRecovery(t, b.store, setup.ref, setup.options, setup.clock, window)
				if _, err := f.c.Submit(bg, Input{Content: "go"}); err != nil {
					t.Fatal(err)
				}
				run := watchReceive(t, setup.runs)
				before := f.stored()
				text := strings.Repeat("held tool output ", 4)
				mainWriteToolOutput(t, f, run, text, text)
				held := f.stored()
				if got, ok := mainToolOutput(f.live(held)); ok {
					t.Errorf("memory output already stored: %q", got)
				}
				for id, task := range before.Tasks {
					if task.Kind == "pi.tool" && !equalJSONValue(task.Checkpoint, held.Tasks[id].Checkpoint) {
						t.Error("presentation changed durable task checkpoint")
					}
				}
				switch mode {
				case "timer":
					mainRecoveryAdvance(f)
					for {
						state := f.stored()
						if got, ok := mainToolOutput(f.live(state)); ok && got == text {
							f.replicaMatches(state)
							break
						}
						watchReceive(t, f.replicated)
					}
				case "dependent":
					if _, err := f.h.Commit(bg, func(tx *Tx) (any, error) {
						live, err := builtin(tx, f.c.ID(), "pi.live")
						if err != nil {
							return nil, err
						}
						value, err := live.Get()
						if err != nil {
							return nil, err
						}
						got, ok := mainToolOutput(value)
						if !ok || got != text {
							t.Error("dependent read missed memory", got)
						}
						id, err := tx.MintID()
						if err != nil {
							return nil, err
						}
						return nil, tx.appendEntryRecord(Entry{ID: id, Conversation: f.c.ID(), Kind: "test.tool-dependent", Value: JSON{"read": got}})
					}); err != nil {
						t.Fatal(err)
					}
					state := f.stored()
					if got, ok := mainToolOutput(f.live(state)); !ok || got != text {
						t.Error("dependent read did not persist held output", got)
					}
					f.replicaMatches(state)
				case "close":
					if err := f.h.Close(bg); err != nil {
						t.Fatal(err)
					}
					store := utf16ReopenStore(t, f.store)
					state, err := store.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					if got, ok := mainToolOutput(f.live(state)); !ok || got != text {
						t.Error("close lost held output", got)
					}
				}
				if err := f.h.Close(bg); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestMainToolProgressCrashBeforeFirstStore(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		setup := mainToolRecoverySetup(t, false)
		f := mainOpenRecovery(t, b.store, setup.ref, setup.options, setup.clock, math.Inf(1))
		sub, err := f.c.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		run := watchReceive(t, setup.runs)
		text := "not stored before the process disappears"
		mainWriteToolOutput(t, f, run, text, text)
		store := f.crash()
		state, err := store.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := mainToolOutput(f.live(state)); ok {
			t.Error("unstored output survived crash", got)
		}
		f = mainOpenRecovery(t, store, setup.ref, setup.options, setup.clock, math.Inf(1))
		mainFinishToolSubmission(t, f, sub.ID())
		want := []string{"<harness>\n[error] Tool work was interrupted and may have partially run\n</harness>"}
		if got := mainToolResults(t, f); !reflect.DeepEqual(got, want) {
			t.Errorf("results=%q want=%q", got, want)
		}
		if setup.count.Load() != 1 {
			t.Error("unsafe tool replayed", setup.count.Load())
		}
		if err := f.h.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}

func TestMainToolProgressMemoryEventsNotStorageReplay(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		setup := mainToolRecoverySetup(t, false)
		f := mainOpenRecovery(t, b.store, setup.ref, setup.options, setup.clock, math.Inf(1))
		events := make(chan string, 16)
		watch, err := f.h.WatchEvents(bg, f.c.ID())
		if err != nil {
			t.Fatal(err)
		}
		if err := watch.Start(func(_ context.Context, batch []AgentEvent) error {
			for _, e := range batch {
				if e.Type == "tool_execution_update" && e.OutputChange != nil {
					events <- e.Output
				}
				if e.Type == "entry_appended" && e.Entry != nil && e.Entry.Kind == "test.tool-marker" {
					events <- "marker"
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.c.Submit(bg, Input{Content: "go"}); err != nil {
			t.Fatal(err)
		}
		run := watchReceive(t, setup.runs)
		mainWriteToolOutput(t, f, run, "one", "one")
		mainWriteToolOutput(t, f, run, "two", "onetwo")
		if got := watchReceive(t, events); got != "one" {
			t.Fatal("first memory event", got)
		}
		if got := watchReceive(t, events); got != "onetwo" {
			t.Fatal("second memory event", got)
		}
		if got, ok := mainToolOutput(f.live(f.stored())); ok {
			t.Fatal("event waited for storage", got)
		}
		if _, err := f.h.Commit(bg, func(tx *Tx) (any, error) {
			live, err := builtin(tx, f.c.ID(), "pi.live")
			if err != nil {
				return nil, err
			}
			if _, err := live.Get(); err != nil {
				return nil, err
			}
			id, err := tx.MintID()
			if err != nil {
				return nil, err
			}
			return nil, tx.appendEntryRecord(Entry{ID: id, Conversation: f.c.ID(), Kind: "test.tool-marker", Value: JSON{}})
		}); err != nil {
			t.Fatal(err)
		}
		if got := watchReceive(t, events); got != "marker" {
			t.Error("stored flush replayed output", got)
		}
		watch.Stop()
		if err := f.h.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}
