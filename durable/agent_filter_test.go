package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
)

func TestAgentFiltersPinnedLiveDefaultsAddRemoveAndToolControl(t *testing.T) {
	registry := NewRegistry()
	register := func(name string) {
		t.Helper()
		if err := registry.Install(&Extension{Name: name, Tools: []ToolRegistration{{Definition: goai.Tool{Name: name, Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "filter." + name, Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			return ToolResult{Content: name, Control: &ToolControl{AddTools: []string{"hidden"}}}, nil
		}}}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"visible", "hidden", "extra"} {
		register(name)
	}
	calls := 0
	ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		calls++
		names := []string{}
		for _, tool := range input.Tools {
			names = append(names, tool.Name)
		}
		joined := strings.Join(names, ",")
		ch := make(chan goai.Event, 1)
		if calls == 1 {
			if joined != "visible" {
				t.Error("remove filter", names)
			}
			ch <- toolAnswer("visible-call", "visible", JSON{})
		} else {
			if joined != "visible,hidden" {
				t.Error("addTools clears removal", names)
			}
			ch <- terminal("answer")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	store, _ := NewMemory()
	h := openHarness(t, store, options)
	c := root(t, h, ref)
	removed := []string{"hidden", "hidden"}
	if err := c.ConfigurePatch(bg, AgentPatch{ExtensionFilter: &ExtensionFilter{Remove: []string{"extra"}}, ToolsRemoved: &removed}); err != nil {
		t.Fatal(err)
	}
	sub, err := c.Submit(bg, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	waitSubmission(t, sub)
	snapshot, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := agentDocument(snapshot, c.ID())
	var agent agentState
	if err := fromObject(doc.Value, &agent, h.session.limits); err != nil {
		t.Fatal(err)
	}
	if agent.ToolsRemoved == nil || len(*agent.ToolsRemoved) != 0 || agent.ExtensionFilter == nil {
		t.Fatal("filter state", agent)
	}
	register("later")
	offers, _, _, _, err := registry.selectedGeneration(agent, h.session.limits)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, offer := range offers {
		if offer.Name == "later" {
			found = true
		}
	}
	if !found {
		t.Fatal("remove selection froze installation")
	}
	filter := &ExtensionFilter{Remove: []string{"hidden", "later"}, Add: []string{"extra", "visible", "extra"}}
	if err := c.ConfigurePatch(bg, AgentPatch{ExtensionFilter: filter, Clear: []string{"tools"}}); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = h.Snapshot(bg)
	doc, _ = agentDocument(snapshot, c.ID())
	agent = agentState{}
	fromObject(doc.Value, &agent, h.session.limits)
	selected := registry.selectedHooks(agent)
	if len(selected) != 2 {
		t.Fatal("add/remove selected count", len(selected))
	}
	registry.mu.RLock()
	extensions := registry.selectedExtensionsLocked(agent)
	registry.mu.RUnlock()
	if len(extensions) != 2 || extensions[0].name != "visible" || extensions[1].name != "extra" {
		t.Fatal("added extension order", extensions)
	}
}
