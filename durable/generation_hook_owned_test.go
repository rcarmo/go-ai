package durable

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestGenerationAfterToolsHookOwnsWorkAndLateEventsDoNotRepeatTurnEnd(t *testing.T) {
	for _, terminate := range []bool{false, true} {
		t.Run(map[bool]string{false: "successor", true: "terminate"}[terminate], func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				entered, release := make(chan struct{}), make(chan struct{})
				child := taskDefinition(t, "task.after-tools.owned", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					close(entered)
					<-release
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("child"), nil })
				})
				registry := NewRegistry()
				if _, err := registry.RegisterTask(child); err != nil {
					t.Fatal(err)
				}
				tool := wrapRegistration("noop")
				tool.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
					return ToolResult{Content: "done", Control: &ToolControl{Terminate: terminate}}, nil
				}
				if err := registry.Register(tool); err != nil {
					t.Fatal(err)
				}
				var captured *HookAPI
				var ownerID, childID ID
				var hookCalls atomic.Int64
				if err := registry.Install(&Extension{Name: "owned.after-tools", Hooks: GenerationHooks{AfterTools: func(ctx context.Context, _ []MessageReceipt, api *HookAPI) error {
					hookCalls.Add(1)
					captured = api
					ownerID = api.TaskID()
					if err := api.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
						record, err := tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: api.TaskID()}})
						childID = record
						return nil, err
					}); err != nil {
						return err
					}
					awaitTaskSignal(t, entered)
					return nil
				}}}); err != nil {
					t.Fatal(err)
				}
				var modelCalls atomic.Int64
				ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
					ch := make(chan goai.Event, 1)
					if modelCalls.Add(1) == 1 {
						ch <- toolAnswer("noop-call", "noop", JSON{})
					} else {
						ch <- terminal("answer")
					}
					close(ch)
					return ch
				})
				options.Registry = registry
				h := openHarness(t, b.store, options)
				cleanupTaskGates(t, release)
				conversation := root(t, h, ref)
				var mu sync.Mutex
				earlyEvents, lateEvents := []AgentEvent{}, []AgentEvent{}
				earlyBarrier, lateBarrier := make(chan struct{}), make(chan struct{})
				early, err := h.WatchEvents(bg, conversation.ID())
				if err != nil {
					t.Fatal(err)
				}
				defer early.Stop()
				if err := early.Start(func(_ context.Context, events []AgentEvent) error {
					mu.Lock()
					earlyEvents = append(earlyEvents, events...)
					mu.Unlock()
					for _, event := range events {
						if event.Type == "entry_appended" && event.Entry.Kind == "app.events.barrier" {
							releaseTaskGate(earlyBarrier)
						}
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				submission, err := conversation.Submit(bg, Input{Content: "go"})
				if err != nil {
					t.Fatal(err)
				}
				waitSubmission(t, submission)
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				owner, err := CanonicalTask(state.Tasks[ownerID], h.session.limits)
				if err != nil || owner.State.Status != "completing" || owner.State.Outcome.Status != "completed" || state.Tasks[childID].Owner != ownerID {
					t.Fatal("hook-owned generation did not hold", owner, err)
				}
				wantTurns := 2
				if terminate {
					wantTurns = 1
				}
				if hookCalls.Load() != 1 || modelCalls.Load() != int64(wantTurns) {
					t.Fatal("unexpected hooks/requests", hookCalls.Load(), modelCalls.Load())
				}
				late, err := h.WatchEvents(bg, conversation.ID())
				if err != nil {
					t.Fatal(err)
				}
				defer late.Stop()
				if err := late.Start(func(_ context.Context, events []AgentEvent) error {
					mu.Lock()
					lateEvents = append(lateEvents, events...)
					mu.Unlock()
					for _, event := range events {
						if event.Type == "entry_appended" && event.Entry.Kind == "app.events.barrier" {
							releaseTaskGate(lateBarrier)
						}
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				releaseTaskGate(release)
				waitPublicTask(t, h, ownerID)
				// A committed marker witnesses both serial observers after final cleanup.
				if _, err := h.Commit(bg, func(tx *Tx) error {
					id, err := tx.MintID()
					if err != nil {
						return err
					}
					return tx.AppendEntry(Entry{ID: id, Conversation: conversation.ID(), Kind: "app.events.barrier", Value: JSON{}})
				}); err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, lateBarrier)
				awaitTaskSignal(t, earlyBarrier)
				mu.Lock()
				earlyTurns, lateTurns := 0, 0
				for _, event := range earlyEvents {
					if event.Type == "turn_end" {
						earlyTurns++
					}
				}
				for _, event := range lateEvents {
					if event.Type == "turn_end" {
						lateTurns++
					}
				}
				mu.Unlock()
				if earlyTurns != wantTurns || lateTurns != 0 {
					t.Fatalf("turn_end repeated at held finalisation: early=%d late=%d", earlyTurns, lateTurns)
				}
				var effects atomic.Int64
				if err := captured.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) { effects.Add(1); return nil, nil }); !errors.Is(err, ErrSealed) || effects.Load() != 0 {
					t.Fatal("escaped hook retained commit authority", err, effects.Load())
				}
			})
		})
	}
}
