package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestSystemToolPlanningReplayOrderSchemaChangeAndHeadBaseline(t *testing.T) {
	tool := func(name, description string) goai.Tool {
		return goai.Tool{Name: name, Description: description, Parameters: json.RawMessage(`{"type":"object"}`)}
	}
	first, err := PlanSystemEntries(ContextView{}, nil, []goai.Tool{tool("a", "old"), tool("b", "kept")}, 1, DefaultLimits())
	if err != nil || len(first) != 1 || len(first[0].Model[0].ToolsAdded) != 2 {
		t.Fatal(first, err)
	}
	view := ContextView{Messages: first[0].Model}
	unchanged, err := PlanSystemEntries(view, nil, []goai.Tool{tool("a", "old"), tool("b", "kept")}, 2, DefaultLimits())
	if err != nil || len(unchanged) != 0 {
		t.Fatal("unchanged declarations written", unchanged, err)
	}
	reordered, err := PlanSystemEntries(view, nil, []goai.Tool{tool("b", "kept"), tool("a", "new")}, 2, DefaultLimits())
	if err != nil || len(reordered) != 1 {
		t.Fatal(reordered, err)
	}
	shown := ReplayToolDeclarations(append(append([]MessageReceipt{}, view.Messages...), reordered[0].Model...))
	if len(shown) != 2 || shown[0].Name != "b" || shown[1].Name != "a" || shown[1].Description != "new" {
		t.Fatal("loadout replay", shown)
	}
	view.Head = &Entry{ID: 5, Head: 3}
	view.Entries = []Entry{{ID: 2, Kind: "pi.system"}, *view.Head}
	baseline, err := PlanSystemEntries(view, nil, []goai.Tool{tool("b", "kept")}, 3, DefaultLimits())
	if err != nil || len(baseline) != 1 || len(baseline[0].Edits) != 1 || baseline[0].Edits[0].Target != 2 || len(baseline[0].Model[0].ToolsAdded) != 1 {
		t.Fatal("tool baseline", baseline, err)
	}
}
func TestToolLoadoutHistoryPersistsSelectedChangesAndReopens(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		register := func(description string) {
			t.Helper()
			if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "echo", Description: description, Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "prompt.echo", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) { return ToolResult{Content: "echo"}, nil }}); err != nil {
				t.Fatal(err)
			}
		}
		register("old")
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			ch <- terminal("answer")
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		run := func(c *ConversationHandle) {
			t.Helper()
			sub, err := c.Submit(bg, Input{Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			waitSubmission(t, sub)
		}
		run(conversation)
		register("new")
		run(conversation)
		view, err := conversation.ContextView(bg, 0)
		if err != nil {
			t.Fatal(err)
		}
		declarations := ReplayToolDeclarations(view.Messages)
		if len(declarations) != 1 || declarations[0].Description != "new" {
			t.Fatal(declarations)
		}
		systems := 0
		for _, entry := range view.Entries {
			if entry.Kind == "pi.system" {
				systems++
			}
		}
		if systems != 2 {
			t.Fatal("loadout positional patches", systems)
		}
		none := []string{}
		if err := conversation.Configure(bg, AgentChange{Model: ref, Tools: &none}); err != nil {
			t.Fatal(err)
		}
		run(conversation)
		view, err = conversation.ContextView(bg, 0)
		if err != nil || len(ReplayToolDeclarations(view.Messages)) != 0 {
			t.Fatal("tool removal lost", view, err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		recovered, err := second.Conversation(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		before, err := recovered.ContextView(bg, 0)
		if err != nil {
			t.Fatal(err)
		}
		run(recovered)
		after, err := recovered.ContextView(bg, 0)
		if err != nil {
			t.Fatal(err)
		}
		count := func(view ContextView) int {
			n := 0
			for _, entry := range view.Entries {
				if entry.Kind == "pi.system" {
					n++
				}
			}
			return n
		}
		if count(before) != count(after) || len(ReplayToolDeclarations(after.Messages)) != 0 {
			t.Fatal("loadout replay duplicated patch")
		}
	})
}
