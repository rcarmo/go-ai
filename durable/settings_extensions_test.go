package durable

import (
	"fmt"
	"sync"
	"testing"
)

func TestHostSettingsExtensionDefaultsRefreshAndExplicitAgentOverride(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"first", "second"} {
		if err := registry.Install(&Extension{Name: name, Tools: []ToolRegistration{wrapRegistration(name)}}); err != nil {
			t.Fatal(err)
		}
	}
	store, _ := NewMemory()
	names := []string{"first"}
	h := openHarness(t, store, Options{Registry: registry, Settings: &HarnessSettings{Extensions: &names}})
	conversation, err := h.CreateConversation(bg, AgentChange{})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := conversation.Agent(bg)
	if err != nil || len(agent.Extensions) != 1 || agent.Extensions[0] != "first" {
		t.Fatal(agent, err)
	}
	names[0] = "mutated"
	agent, err = conversation.Agent(bg)
	if err != nil || len(agent.Extensions) != 1 || agent.Extensions[0] != "first" {
		t.Fatal("initial settings were not detached", agent, err)
	}
	selected := []string{"second"}
	if err := h.SetSettings(bg, HarnessSettings{Extensions: &selected}); err != nil {
		t.Fatal(err)
	}
	selected[0] = "mutated"
	agent, err = conversation.Agent(bg)
	if err != nil || len(agent.Extensions) != 1 || agent.Extensions[0] != "second" {
		t.Fatal(agent, err)
	}
	explicit := []string{"first"}
	if err := conversation.ConfigurePatch(bg, AgentPatch{Extensions: &explicit}); err != nil {
		t.Fatal(err)
	}
	invalid := []string{""}
	if err := h.SetSettings(bg, HarnessSettings{Extensions: &invalid}); err == nil {
		t.Fatal("invalid default selection accepted")
	}
	if err := h.SetSettings(bg, HarnessSettings{}); err != nil {
		t.Fatal(err)
	}
	agent, err = conversation.Agent(bg)
	if err != nil || len(agent.Extensions) != 1 || agent.Extensions[0] != "first" {
		t.Fatal(agent, err)
	}
	if err := conversation.ConfigurePatch(bg, AgentPatch{Clear: []string{"extensions"}}); err != nil {
		t.Fatal(err)
	}
	agent, err = conversation.Agent(bg)
	if err != nil || len(agent.Extensions) != 2 {
		t.Fatal(agent, err)
	}
}

func TestHostSettingsExtensionDefaultsConcurrentDetachedSelections(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"first", "second"} {
		if err := registry.Install(&Extension{Name: name, Tools: []ToolRegistration{wrapRegistration(name)}}); err != nil {
			t.Fatal(err)
		}
	}
	store, err := NewMemory()
	if err != nil {
		t.Fatal(err)
	}
	legacy := []string{"first"}
	h := openHarness(t, store, Options{Registry: registry, Extensions: &legacy})
	conversation, err := h.CreateConversation(bg, AgentChange{})
	if err != nil {
		t.Fatal(err)
	}
	before, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	failures := make(chan error, 4)
	var joined sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		joined.Add(1)
		go func(worker int) {
			defer joined.Done()
			<-start
			for i := 0; i < 30; i++ {
				if worker == 0 {
					selected := []string{"first"}
					if i%2 == 0 {
						selected[0] = "second"
					}
					if err := h.SetSettings(bg, HarnessSettings{Extensions: &selected}); err != nil {
						failures <- err
						return
					}
					selected[0] = "mutated"
					continue
				}
				agent, err := conversation.Agent(bg)
				if err != nil {
					failures <- err
					return
				}
				if len(agent.Extensions) != 1 || (agent.Extensions[0] != "first" && agent.Extensions[0] != "second") {
					failures <- fmt.Errorf("torn/default selection: %#v", agent.Extensions)
					return
				}
				agent.Extensions[0] = "mutated"
			}
		}(worker)
	}
	close(start)
	joined.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	after, err := h.Snapshot(bg)
	if err != nil || before.Seq != after.Seq {
		t.Fatal("host settings persisted", before.Seq, after.Seq, err)
	}
	empty := []string{}
	if err := h.SetSettings(bg, HarnessSettings{Extensions: &empty}); err != nil {
		t.Fatal(err)
	}
	agent, err := conversation.Agent(bg)
	if err != nil || len(agent.Extensions) != 0 {
		t.Fatal("explicit empty defaults", agent, err)
	}
	if err := h.SetSettings(bg, HarnessSettings{}); err != nil {
		t.Fatal(err)
	}
	agent, err = conversation.Agent(bg)
	if err != nil || len(agent.Extensions) != 2 {
		t.Fatal("reset retained legacy options defaults", agent, err)
	}
}
