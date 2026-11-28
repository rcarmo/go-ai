package durable

import (
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestToolCallerBoundariesRemainDetachedAndPersisted(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	for _, spec := range []struct {
		name    string
		callers []string
	}{
		{name: "model_only", callers: []string{"model"}},
		{name: "tool_only", callers: []string{"tools"}},
		{name: "either"},
		{name: "probe"},
	} {
		registration := wrapRegistration(spec.name)
		registration.Callers = spec.callers
		if err := registry.Register(registration); err != nil {
			t.Fatal(err)
		}
	}
	h := openHarness(t, store, Options{Registry: registry})
	root := root(t, h, ModelRef{})

	agent, err := root.Agent(bg)
	if err != nil {
		t.Fatal(err)
	}
	toolNames := func(tools []goai.Tool) []string {
		names := make([]string, len(tools))
		for i := range tools {
			names[i] = tools[i].Name
		}
		return names
	}
	if got, want := toolNames(agent.Tools), []string{"model_only", "either", "probe"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("model tools = %v; want %v", got, want)
	}
	if got, want := toolNames(agent.Callable), []string{"tool_only", "either", "probe"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("callable tools = %v; want %v", got, want)
	}

	modelTools := &ExtensionFilter{Remove: []string{"either"}, Add: []string{"probe"}}
	if err := root.ConfigurePatch(bg, AgentPatch{ModelTools: modelTools}); err != nil {
		t.Fatal(err)
	}
	agent, err = root.Agent(bg)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := toolNames(agent.Tools), []string{"model_only", "probe"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered model tools = %v; want %v", got, want)
	}
	if got, want := toolNames(agent.Callable), []string{"tool_only", "either", "probe"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("nested callable tools changed = %v; want %v", got, want)
	}
	if agent.Configuration.ModelTools == nil || !reflect.DeepEqual(agent.Configuration.ModelTools.Remove, []string{"either"}) {
		t.Fatalf("modelTools record = %+v", agent.Configuration.ModelTools)
	}

	// Mutating a detached public view cannot alter the committed agent record.
	agent.Tools[0].Name = "mutated"
	again, err := root.Agent(bg)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Tools[0].Name; got != "model_only" {
		t.Fatalf("detached model tool alias leaked: %q", got)
	}
}
