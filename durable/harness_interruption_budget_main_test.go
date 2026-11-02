package durable

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Selected source204 exhaustion and source596 charge-once assertions; sleeping
// is a native phase guard control, not the complete source275 tools scenario.
func TestMainInterruptedBudgetAndCloseAccounting(t *testing.T) {
	for _, mode := range []string{"exhaust", "charge-once", "sleeping", "clean-resend"} {
		t.Run(mode, func(t *testing.T) {
			utf16Backends(t, func(t *testing.T, b backend) {
				crashes, maximum, delay := 1, 3, int64(1)
				if mode == "exhaust" || mode == "clean-resend" {
					crashes, maximum = 2, 1
				}
				if mode == "sleeping" {
					maximum, delay = 1, 60000
				}
				var calls atomic.Int64
				streams := make(chan *mainRecoveryStream, 4)
				ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					if calls.Add(1) > int64(crashes) {
						events := make(chan goai.Event, 1)
						events <- terminal("answer")
						close(events)
						return events
					}
					stream := &mainRecoveryStream{events: make(chan goai.Event, 1)}
					streams <- stream
					go func() { <-ctx.Done(); stream.close() }()
					return stream.events
				})
				var clock atomic.Int64
				clock.Store(1000)
				enabled := true
				policy := &RetrySettings{Enabled: &enabled, MaxRetries: &maximum, BaseDelayMs: &delay}
				f := mainOpenRecoveryWithRetry(t, b.store, ref, options, &clock, 0, policy)
				sub, err := f.c.Submit(bg, Input{Content: "hi"})
				if err != nil {
					t.Fatal(err)
				}
				var taskID ID
				for round := 0; round < crashes; round++ {
					stream := mainRecoveryRetryStream(t, f, streams)
					text := strings.Repeat("z", 64*(round+1))
					f.partial(stream, text)
					now := clock.Add(100)
					var jobs []func()
					f.timerMu.Lock()
					for _, timer := range f.timers {
						if !timer.cancelled && timer.due <= now {
							timer.cancelled = true
							jobs = append(jobs, timer.fire)
						}
					}
					f.timerMu.Unlock()
					for _, fire := range jobs {
						fire()
					}
					if _, err := f.h.Snapshot(bg); err != nil {
						t.Fatal(err)
					}
					if stored, ok := mainRecoveryText(f.live(f.stored())); !ok || stored != text {
						t.Fatal("fixture lost its stored prefix", stored)
					}
					task, _, ok, err := f.h.nextTask(f.c.ID())
					if err != nil || !ok {
						t.Fatal(err)
					}
					if taskID == 0 {
						taskID = task.ID
					} else if task.ID != taskID {
						t.Fatal("retry changed task identity")
					}
					var store Storage
					if mode == "clean-resend" && round == 0 {
						if err := f.h.Close(bg); err != nil {
							t.Fatal(err)
						}
						store = utf16ReopenStore(t, f.store)
					} else {
						store = f.crash()
					}
					f = mainOpenRecoveryWithRetry(t, store, ref, options, &clock, 0, policy)
					if mode == "charge-once" {
						if err := f.h.Close(bg); err != nil {
							t.Fatal(err)
						}
						f = mainOpenRecoveryWithRetry(t, utf16ReopenStore(t, store), ref, options, &clock, 0, policy)
					}
					if err := f.h.Resume(bg); err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				if err := mainInterruptionQuiescent(ctx, f.h); err != nil {
					t.Fatal(err)
				}
				if mode != "exhaust" {
					record, exists, err := f.h.Task(bg, taskID)
					if err != nil || !exists || record.State.Status != "sleeping" || record.State.Until == nil || record.Restarts != 0 {
						t.Fatal("charge not durably sleeping", record, err)
					}
					until := *record.State.Until
					if until != clock.Load()+delay {
						t.Fatal("wrong retry deadline", until, clock.Load())
					}
					if mode == "sleeping" {
						store := f.crash()
						f = mainOpenRecoveryWithRetry(t, store, ref, options, &clock, 0, policy)
						if err := f.h.Resume(bg); err != nil {
							t.Fatal(err)
						}
						if err := mainInterruptionQuiescent(ctx, f.h); err != nil {
							t.Fatal(err)
						}
						again, _, err := f.h.Task(bg, taskID)
						if err != nil || again.Restarts != 0 || again.State.Until == nil || *again.State.Until != until {
							t.Fatal("sleep charged again", again, err)
						}
					}
					if calls.Load() != int64(crashes) {
						t.Fatal("dispatch before retry deadline", calls.Load())
					}
					clock.Store(until)
					if err := f.h.Resume(bg); err != nil {
						t.Fatal(err)
					}
					if err := mainInterruptionQuiescent(ctx, f.h); err != nil {
						t.Fatal(err)
					}
				}
				handle, err := f.h.Submission(bg, sub.ID())
				if err != nil {
					t.Fatal(err)
				}
				status, err := handle.Status(bg)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "exhaust" {
					text := "The host stopped while the model was answering, on attempt 2"
					if status.Status != "unanswered" || status.Reason != "interrupted" || status.Detail == nil || status.Detail.Value != text || calls.Load() != 2 {
						t.Errorf("exhaustion: %+v calls=%d", status, calls.Load())
					}
					record, _, err := f.h.Task(bg, taskID)
					if err != nil {
						t.Fatal(err)
					}
					got, err := dtoObject(record.State.Outcome, f.h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					want, err := dtoObject(TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: text, Detail: &TaskValue{Present: true, Value: JSON{"reason": "interrupted"}}}}, f.h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					if !equalJSONValue(got, want) {
						t.Errorf("exhaustion outcome: got %v; want %v", got, want)
					}
				} else if status.Status != "done" || calls.Load() != int64(crashes+1) {
					t.Errorf("one charged retry: %+v calls=%d", status, calls.Load())
				}
				state, err := f.h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				var roles, aborted []string
				for _, id := range ids(state.Entries) {
					entry := state.Entries[id]
					if entry.Conversation != f.c.ID() || entry.Value["role"] == nil {
						continue
					}
					roles = append(roles, fmt.Sprint(entry.Value["role"]))
					if entry.Value["stopReason"] == "aborted" {
						var message MessageReceipt
						if err := fromObject(entry.Value, &message, f.h.session.limits); err != nil || len(message.Content) != 1 {
							t.Fatal("aborted receipt", message, err)
						}
						aborted = append(aborted, message.Content[0].Text)
					}
				}
				wantRoles := []string{"user"}
				for i := 0; i < crashes; i++ {
					wantRoles = append(wantRoles, "assistant")
				}
				if mode != "exhaust" {
					wantRoles = append(wantRoles, "assistant")
				}
				if !equalJSONValue(roles, wantRoles) {
					t.Error("duplicate input or missing assistant", roles)
				}
				if len(aborted) != crashes {
					t.Fatal("wrong conversion count", aborted)
				}
				for i, text := range aborted {
					if text != strings.Repeat("z", 64*(i+1)) {
						t.Error("converted prefix changed", i, text)
					}
				}
			})
		})
	}
}
