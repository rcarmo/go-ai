package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestCommittedLiveGenerationPartialAndToolProgressSlots(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		providerEntered, providerRelease, toolEntered, toolRelease := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
		cleanupTaskGates(t, providerRelease, toolRelease)
		registry := NewRegistry()
		registration := wrapRegistration("live-tool")
		registration.Execute = func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
			if err := api.Output("committed tool output"); err != nil {
				return ToolResult{}, err
			}
			if err := api.Details(JSON{"step": 1}); err != nil {
				return ToolResult{}, err
			}
			close(toolEntered)
			<-toolRelease
			return ToolResult{Content: "done"}, nil
		}
		if err := registry.Register(registration); err != nil {
			t.Fatal(err)
		}
		calls := 0
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			calls++
			ch := make(chan goai.Event, 1)
			if calls == 1 {
				close(providerEntered)
				go func() { <-providerRelease; ch <- toolAnswer("live-call", "live-tool", JSON{}); close(ch) }()
			} else {
				ch <- terminal("answer")
				close(ch)
			}
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		sub, err := conversation.Submit(bg, Input{Content: "question"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, providerEntered)
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		var generation ID
		for _, task := range state.Tasks {
			if task.Kind == "pi.generation" {
				generation = task.ID
			}
		}
		if err := h.commitPartial(generation, MessageReceipt{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "committed partial"}}, Api: goai.Api("test")}); err != nil {
			t.Fatal(err)
		}
		live := readLiveStatus(t, h, conversation.ID())
		present := live["generation"].(map[string]any)
		if !equalJSONValue(present["attempt"], 1) || present["message"].(map[string]any)["role"] != string(goai.RoleAssistant) {
			t.Fatal(live)
		}
		releaseTaskGate(providerRelease)
		awaitTaskSignal(t, toolEntered)
		live = readLiveStatus(t, h, conversation.ID())
		if live["generation"] != nil {
			t.Fatal("generation retained in tool round", live)
		}
		slots := live["tools"].([]any)
		if len(slots) != 1 {
			t.Fatal(slots)
		}
		slot := slots[0].(map[string]any)
		if slot["callId"] != "live-call" || slot["status"] != "running" || slot["output"] != "committed tool output" || slot["details"].(map[string]any)["step"] == nil {
			t.Fatal(slot)
		}
		releaseTaskGate(toolRelease)
		waitSubmission(t, sub)
		live = readLiveStatus(t, h, conversation.ID())
		if live["generation"] != nil || live["tools"] != nil || live["run"] != nil {
			t.Fatal("settled run presentation retained", live)
		}
	})
}
func readLiveStatus(t *testing.T, h *Harness, conversation ID) JSON {
	t.Helper()
	state, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range state.Documents {
		if document.Kind == "pi.live" && document.Owner == conversation {
			return document.Value
		}
	}
	t.Fatal("live document missing")
	return nil
}
