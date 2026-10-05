package durable

import (
	"context"
	"encoding/json"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestToolProgressDetailsWindowAndSealedWrites(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		var escaped *ToolAPI
		if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "progress", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "progress.test", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) { return ToolResult{}, nil }}); err != nil {
			t.Fatal(err)
		}
		// Replace the same implementation with the real callback before dispatch.
		if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "progress", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "progress.test", Version: 1, Execute: func(_ context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
			escaped = api
			details := JSON{"child": JSON{"id": 12}}
			if err := api.Details(details); err != nil {
				return ToolResult{}, err
			}
			details["child"].(JSON)["id"] = 99
			if err := api.Output("abcdef"); err != nil {
				return ToolResult{}, err
			}
			if err := api.SetOutput("defXYZ"); err != nil {
				return ToolResult{}, err
			}
			if err := api.Details(nil); err != nil {
				return ToolResult{}, err
			}
			if err := api.Details(JSON{"retained": true}); err != nil {
				return ToolResult{}, err
			}
			return ToolResult{Content: " final"}, nil
		}}); err != nil {
			t.Fatal(err)
		}
		calls := 0
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			calls++
			ch := make(chan goai.Event, 1)
			if calls == 1 {
				ch <- toolAnswer("progress-call", "progress", JSON{})
			} else {
				ch <- terminal("done")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		watch, err := h.WatchEvents(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		var mu sync.Mutex
		detailsSeen, cleared, window := false, false, false
		finished := make(chan struct{}, 1)
		if err := watch.Start(func(_ context.Context, events []AgentEvent) error {
			mu.Lock()
			defer mu.Unlock()
			for _, event := range events {
				if event.Type == "tool_execution_update" {
					if event.HasDetails {
						if event.Details == nil {
							cleared = true
						} else {
							object := event.Details.(map[string]any)
							if child, ok := object["child"].(map[string]any); ok {
								number, _ := exactNumber(child["id"])
								if number.Num().Int64() != 12 {
									t.Error("metadata alias", event)
								}
								detailsSeen = true
							} else if object["retained"] != true {
								t.Error("unexpected metadata", event)
							}
							object["changed"] = true
						}
					}
					if event.OutputChange != nil && event.OutputChange.TrimStart == 3 && event.OutputChange.Append == "XYZ" {
						window = true
					}
				}
				if event.Type == "run_end" {
					select {
					case finished <- struct{}{}:
					default:
					}
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		sub, err := conversation.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, sub)
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			end, _ := watch.End()
			t.Fatal(end)
		}
		mu.Lock()
		if !detailsSeen || !cleared || !window {
			t.Error("progress changes missing", detailsSeen, cleared, window)
		}
		mu.Unlock()
		for _, write := range []func() error{func() error { return escaped.Details(JSON{"late": true}) }, func() error { return escaped.SetOutput("late") }} {
			if err := write(); !errors.Is(err, ErrSealed) {
				t.Fatal("escaped progress write", err)
			}
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range state.Tasks {
			if task.Kind == "pi.tool" {
				var cp toolCheckpoint
				if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
					t.Fatal(err)
				}
				if cp.Result.Content[0].Text != "defXYZ final" || cp.Result.Details["retained"] != true {
					t.Fatal("progress checkpoint", cp)
				}
			}
		}
	})
}

func TestSafeToolReplayClearsInterruptedOutputAndDetails(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, replayed, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		registry := NewRegistry()
		var effects atomic.Int64
		if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "safe", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "safe.progress", Version: 1, ReplaySafe: true, Execute: func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
			if effects.Add(1) == 1 {
				if err := api.Output("stale prefix"); err != nil {
					return ToolResult{}, err
				}
				if err := api.Details(JSON{"stale": true}); err != nil {
					return ToolResult{}, err
				}
				close(entered)
				<-ctx.Done()
				return ToolResult{}, ctx.Err()
			}
			close(replayed)
			select {
			case <-release:
			case <-ctx.Done():
				return ToolResult{}, ctx.Err()
			}
			return ToolResult{Content: "fresh"}, nil
		}}); err != nil {
			t.Fatal(err)
		}
		calls := 0
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			calls++
			ch := make(chan goai.Event, 1)
			if calls == 1 {
				ch <- toolAnswer("safe-call", "safe", JSON{})
			} else {
				ch <- terminal("done")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		first := openHarness(t, b.store, options)
		conversation := root(t, first, ref)
		sub, err := conversation.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		if err := first.Close(bg); err != nil {
			t.Fatal(err)
		}
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		cleanupTaskGates(t, release)
		watch, err := second.WatchEvents(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		cleared := make(chan struct{}, 1)
		if err := watch.Start(func(_ context.Context, events []AgentEvent) error {
			for _, event := range events {
				if event.Type == "tool_execution_update" && event.HasDetails && event.Details == nil && event.OutputChange != nil && event.OutputChange.Set != nil && *event.OutputChange.Set == "" {
					select {
					case cleared <- struct{}{}:
					default:
					}
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := second.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, replayed)
		state, err := second.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range state.Tasks {
			if task.Kind == "pi.tool" {
				var cp toolCheckpoint
				if err := fromObject(task.Checkpoint, &cp, second.session.limits); err != nil {
					t.Fatal(err)
				}
				if cp.Output != "" || cp.HasDetails || cp.Details != nil {
					t.Fatal("replay retained stale UI", cp)
				}
			}
		}
		releaseTaskGate(release)
		handle, err := second.Submission(bg, sub.ID())
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, handle)
		select {
		case <-cleared:
		case <-time.After(3 * time.Second):
			end, _ := watch.End()
			t.Fatal("replay reset not published", end)
		}
		state, err = second.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range state.Tasks {
			if task.Kind == "pi.tool" {
				var cp toolCheckpoint
				if err := fromObject(task.Checkpoint, &cp, second.session.limits); err != nil {
					t.Fatal(err)
				}
				if cp.Result.Content[0].Text != "fresh" {
					t.Fatal("replay receipt reused prefix", cp)
				}
			}
		}
	})
}
