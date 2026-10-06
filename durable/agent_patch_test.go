package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
)

func TestAgentPatchPinnedPartialClearOrderingHistoricalForkAndReopen(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		order := []string{}
		for _, name := range []string{"first", "second"} {
			name := name
			ext := &Extension{Name: name, Tools: []ToolRegistration{{Definition: goai.Tool{Name: name, Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "patch." + name, Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) { return ToolResult{Content: "unused"}, nil }}}, Sections: []PromptSection{{Key: name, Render: func(context.Context, PromptInput) (*string, error) { value := name; return &value, nil }}}, Hooks: GenerationHooks{BeforeRequest: func(_ context.Context, messages []MessageReceipt, _ *HookAPI) ([]MessageReceipt, error) {
				order = append(order, name)
				return messages, nil
			}}}
			if err := registry.Install(ext); err != nil {
				t.Fatal(err)
			}
		}
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			if len(input.Tools) != 2 || input.Tools[0].Name != "second" || input.Tools[1].Name != "first" {
				t.Error("explicit tool order", input.Tools)
			}
			if strings.Index(input.SystemPrompt, "<second>") > strings.Index(input.SystemPrompt, "<first>") {
				t.Error("extension section order", input.SystemPrompt)
			}
			ch := make(chan goai.Event, 1)
			ch <- terminal("answer")
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		c, err := h.Root(bg, AgentChange{Model: ref, Cwd: "/old", SystemPrompt: "retained"})
		if err != nil {
			t.Fatal(err)
		}
		cwd := "/new"
		extensions := []string{"second", "first"}
		tools := []string{"second", "missing", "first"}
		if err := c.ConfigurePatch(bg, AgentPatch{Cwd: &cwd, Extensions: &extensions, Tools: &tools}); err != nil {
			t.Fatal(err)
		}
		extensions[0] = "mutated"
		tools[0] = "mutated"
		snapshot, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		doc, _ := agentDocument(snapshot, c.ID())
		var agent agentState
		fromObject(doc.Value, &agent, h.session.limits)
		if agent.Cwd != "/new" || agent.SystemPrompt != "retained" || agent.Model != ref || (*agent.Tools)[0] != "second" {
			t.Fatal("partial patch lost fields/detachment", agent)
		}
		sub, err := c.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		settled := waitSubmission(t, sub)
		if strings.Join(order, ",") != "second,first" {
			t.Fatal("explicit hook order", order)
		}
		view, err := c.ContextView(bg, 0)
		if err != nil {
			t.Fatal(err)
		}
		at := view.Entries[len(view.Entries)-1].ID
		if err := c.ConfigurePatch(bg, AgentPatch{Clear: []string{"cwd", "tools", "extensions"}}); err != nil {
			t.Fatal(err)
		}
		fork, err := c.Fork(bg, at)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err = h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		doc, _ = agentDocument(snapshot, fork.ID())
		fromObject(doc.Value, &agent, h.session.limits)
		if agent.Cwd != "/new" || agent.Tools == nil || (*agent.Tools)[0] != "second" {
			t.Fatal("historical patch state", agent)
		}
		before := snapshot
		if err := c.ConfigurePatch(bg, AgentPatch{Cwd: &cwd, Clear: []string{"cwd"}}); err == nil {
			t.Fatal("conflict accepted")
		}
		after, err := h.Snapshot(bg)
		if err != nil || after.Seq != before.Seq {
			t.Fatal("rejected patch wrote", err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		snapshot, err = second.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		doc, _ = agentDocument(snapshot, c.ID())
		agent = agentState{}
		if err := fromObject(doc.Value, &agent, second.session.limits); err != nil {
			t.Fatal(err)
		}
		if agent.Cwd != "" || agent.Tools != nil || agent.Extensions != nil || agent.SystemPrompt != "retained" {
			t.Fatal("clear/reopen", agent)
		}
		if settled.Submission.Status != "done" {
			t.Fatal(settled)
		}
	})
}

func TestAgentPatchReferenceConfigurationDoesNotResumePausedHarness(t *testing.T) {
	definition := taskDefinition(t, "task.configure-paused", func(context.Context, TaskRecord, *TaskRuntime) error { t.Error("configure resumed task"); return nil })
	h := taskTestHarness(t, mustMemory(t), definition)
	root, err := h.Root(bg, AgentChange{})
	if err != nil {
		t.Fatal(err)
	}
	createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	name := "changed"
	if err := root.ConfigurePatch(bg, AgentPatch{Name: &name}); err != nil {
		t.Fatal(err)
	}
	if err := root.Configure(bg, AgentChange{Name: "replacement"}); err != nil {
		t.Fatal(err)
	}
	view, err := h.Inspect(bg)
	if err != nil || view.Scheduling != "paused" || h.scheduler.enabled.Load() {
		t.Fatal("passive configure enabled scheduler", view, err)
	}
}
