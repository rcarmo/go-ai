package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAgentHostDefaultsFilterExplicitOverrideAndLateInstall(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"coding", "skills", "reviewer"} {
		if err := registry.Install(&Extension{Name: name, Tools: []ToolRegistration{wrapRegistration(name)}}); err != nil {
			t.Fatal(err)
		}
	}
	var requests atomic.Int64
	expected := "coding,skills"
	ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		requests.Add(1)
		names := []string{}
		for _, tool := range input.Tools {
			names = append(names, tool.Name)
		}
		if strings.Join(names, ",") != expected {
			t.Error("host defaults offers", names, expected)
		}
		ch := make(chan goai.Event, 1)
		ch <- terminal("done")
		close(ch)
		return ch
	})
	defaults := []string{"coding", "skills", "missing"}
	options.Registry = registry
	options.Extensions = &defaults
	store, dir := newJournal(t)
	h := openHarness(t, store, options)
	defaults[0] = "reviewer"
	conv := root(t, h, ref)
	submit := func() {
		t.Helper()
		sub, err := conv.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, sub)
	}
	submit()
	filter := &ExtensionFilter{Add: []string{"reviewer", "coding"}, Remove: []string{"skills"}}
	if err := conv.ConfigurePatch(bg, AgentPatch{ExtensionFilter: filter}); err != nil {
		t.Fatal(err)
	}
	expected = "coding,reviewer"
	submit()
	explicit := []string{"reviewer", "skills"}
	if err := conv.ConfigurePatch(bg, AgentPatch{Extensions: &explicit}); err != nil {
		t.Fatal(err)
	}
	expected = "reviewer,skills"
	submit()
	if err := conv.ConfigurePatch(bg, AgentPatch{Clear: []string{"extensions"}}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Install(&Extension{Name: "missing", Tools: []ToolRegistration{wrapRegistration("missing")}}); err != nil {
		t.Fatal(err)
	}
	expected = "coding,skills,missing"
	submit()
	snapshot, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := agentDocument(snapshot, conv.ID())
	if _, ok := doc.Value["defaultExtensions"]; ok {
		t.Fatal("host defaults persisted")
	}
	if err := h.Close(bg); err != nil {
		t.Fatal(err)
	}
	// Reopening changes host defaults without changing the stored agent.
	defaults = []string{"reviewer"}
	options.Extensions = &defaults
	store, err = OpenJournal(dir, JournalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h = openHarness(t, store, options)
	conv, err = h.Conversation(bg, conv.ID())
	if err != nil {
		t.Fatal(err)
	}
	expected = "reviewer"
	submit()
	if requests.Load() != 5 {
		t.Fatal(requests.Load())
	}
}
