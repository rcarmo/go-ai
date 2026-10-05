package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"sync"
	"testing"
	"time"
)

func TestAgentEventsCommittedToolLifecycleAndReentrantListener(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "echo", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "events.echo", Version: 1, Execute: func(_ context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
			if err := api.Output("prefix"); err != nil {
				return ToolResult{}, err
			}
			return ToolResult{Content: "tail"}, nil
		}}); err != nil {
			t.Fatal(err)
		}
		calls := 0
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			calls++
			ch := make(chan goai.Event, 1)
			if calls == 1 {
				ch <- toolAnswer("event-call", "echo", JSON{})
			} else {
				ch <- terminal("answer")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		stream, err := h.WatchEvents(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Stop()
		initial, err := stream.Snapshot()
		if err != nil || len(initial.Tasks) != 0 {
			t.Fatal(initial, err)
		}
		initial.Documents["pi.agent"]["name"] = "mutated"
		fresh, err := stream.Snapshot()
		if err != nil || fresh.Documents["pi.agent"]["name"] == "mutated" {
			t.Fatal("event baseline escaped", err)
		}
		var mu sync.Mutex
		seen := map[string]int{}
		finished := make(chan struct{}, 1)
		if err := stream.Start(func(_ context.Context, events []AgentEvent) error {
			if _, err := h.TaskGraph(bg); err != nil {
				return err
			}
			mu.Lock()
			defer mu.Unlock()
			for _, event := range events {
				seen[event.Type]++
				if event.Type == "run_start" || event.Type == "run_end" {
					if len(event.Inputs) != 1 {
						t.Error("run inputs missing", event)
					}
				}
				if event.Type == "usage_changed" && len(event.Usage) == 0 {
					t.Error("usage payload missing", event)
				}
				if event.Type == "inbox_update" && event.Items == nil {
					t.Error("inbox payload missing", event)
				}
				if event.Usage != nil {
					event.Usage["caller"] = "mutation"
				}
				if event.Type == "run_end" {
					select {
					case finished <- struct{}{}:
					default:
					}
				}
				if event.Entry != nil {
					event.Entry.Value["caller"] = "mutation"
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
			end, _ := stream.End()
			t.Fatal("run events missing", end)
		}
		mu.Lock()
		for _, kind := range []string{"run_start", "run_end", "turn_start", "turn_end", "message_end", "entry_appended", "tool_execution_start", "tool_execution_update", "tool_execution_end", "submission", "usage_changed"} {
			if seen[kind] == 0 {
				t.Error("missing committed event", kind, seen)
			}
		}
		mu.Unlock()
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range state.Entries {
			if entry.Value["caller"] != nil {
				t.Fatal("event mutation reached storage")
			}
		}
	})
}

func TestEventSnapshotUsesActiveHeadAndBuiltinDocsOnly(t *testing.T) {
	store, _ := NewMemory()
	h := openHarness(t, store, Options{})
	_, err := h.session.Commit(bg, func(tx *Tx) error {
		if err := initializeBuiltins(tx, 1); err != nil {
			return err
		}
		id, err := tx.MintID()
		if err != nil {
			return err
		}
		if err = tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "app.old", Value: JSON{}}); err != nil {
			return err
		}
		if err = appendReset(tx, 1, "fresh"); err != nil {
			return err
		}
		id, err = tx.MintID()
		if err != nil {
			return err
		}
		_, err = tx.CreateDocument(Document{ID: id, Scope: "conversation", Owner: 1, Kind: "app.private", Family: true, Key: "named", Version: 1, Value: JSON{"not": "mounted"}, History: "latest", Fork: "initial"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	watch, err := h.WatchEvents(bg, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Stop()
	snapshot, err := watch.Snapshot()
	if err != nil || len(snapshot.Entries) != 1 || snapshot.Entries[0].Kind != "pi.reset" || snapshot.Documents["app.private"] != nil {
		t.Fatal(snapshot, err)
	}
}
