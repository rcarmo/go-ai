package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
)

func TestExtensionAtomicReplacementTaskAndToolSnapshots(t *testing.T) {
	registry := NewRegistry()
	tool := ToolRegistration{Definition: goai.Tool{Name: "extension_tool", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "extension.tool", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) { return ToolResult{Content: "ok"}, nil }}
	task := taskDefinition(t, "task.extension", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("extension"), nil })
	})
	section := PromptSection{Key: "extension", Render: func(context.Context, PromptInput) (*string, error) { return promptString("extension"), nil }}
	first := &Extension{Name: "example", Tools: []ToolRegistration{tool}, Tasks: []*TaskDefinition{task}, Sections: []PromptSection{section}}
	if err := registry.Install(first); err != nil {
		t.Fatal(err)
	}
	snapshot, err := registry.taskSnapshot(DefaultLimits())
	if err != nil || snapshot.Task(task.Kind()) != task {
		t.Fatal(snapshot, err)
	}
	first.Tools[0].Definition.Parameters[0] = 'x'
	first.Sections[0].Key = "mutated"
	tools, err := snapshot.Tools()
	if err != nil || len(tools) != 1 {
		t.Fatal(tools, err)
	}
	replacement := &Extension{Name: "example", Sections: []PromptSection{section}}
	if err := registry.Install(replacement); err != nil {
		t.Fatal(err)
	}
	registry.Uninstall(first)
	if len(registry.Installed()) != 0 {
		t.Fatal("name-based uninstall retained replacement")
	}
	if err := registry.Install(replacement); err != nil {
		t.Fatal(err)
	}
	if snapshot.Task(task.Kind()) != task {
		t.Fatal("old snapshot changed")
	}
	current, err := registry.taskSnapshot(DefaultLimits())
	if err != nil || current.Task(task.Kind()) != nil {
		t.Fatal("old task remains installed", err)
	}
	if got, err := current.Tools(); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	bad := &Extension{Name: "bad", Tasks: []*TaskDefinition{task}, Sections: []PromptSection{{Key: "instructions", Render: section.Render}}}
	if err := registry.Install(bad); err == nil {
		t.Fatal("reserved section accepted")
	}
	if len(registry.Installed()) != 1 {
		t.Fatal("failed publication not atomic")
	}
	registry.Uninstall(replacement)
	if len(registry.Installed()) != 0 {
		t.Fatal("uninstall failed")
	}
}
func TestExtensionTaskActuallyRunsAndPromptParticipates(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		task := taskDefinition(t, "task.extension.actual", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("extension"), nil })
		})
		registry := NewRegistry()
		extension := &Extension{Name: "actual", Tasks: []*TaskDefinition{task}}
		if err := registry.Install(extension); err != nil {
			t.Fatal(err)
		}
		h, err := Open(bg, b.store, Options{Registry: registry})
		if err != nil {
			t.Fatal(err)
		}
		defer h.Close(bg)
		id := createPublicTask(t, h, task, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Result.Value != "extension" {
			t.Fatal(record)
		}
	})
}

func TestRegistryReferenceAgentSelectionDeduplicatesFirstPositionsAndMissingNames(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"first", "second"} {
		if err := registry.Install(&Extension{Name: name, Tools: []ToolRegistration{wrapRegistration(name)}}); err != nil {
			t.Fatal(err)
		}
	}
	h := openHarness(t, mustMemory(t), Options{Registry: registry})
	extensions := []string{"second", "missing", "second", "first"}
	tools := []string{"first", "first", "second", "absent"}
	conversation, err := h.CreateConversation(bg, AgentChange{Extensions: &extensions, Tools: &tools})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := conversation.Agent(bg)
	if err != nil || strings.Join(agent.Extensions, ",") != "second,first" || len(agent.Tools) != 2 || agent.Tools[0].Name != "first" || agent.Tools[1].Name != "second" {
		t.Fatal(agent, err)
	}
	if strings.Join(*agent.Configuration.Extensions, ",") != "second,missing,first" || strings.Join(*agent.Configuration.Tools, ",") != "first,second,absent" {
		t.Fatal("stored order/dedup", agent.Configuration)
	}
	extensions[0] = "mutated"
	tools[0] = "mutated"
	agent, err = conversation.Agent(bg)
	if err != nil || agent.Extensions[0] != "second" || agent.Tools[0].Name != "first" {
		t.Fatal("selection aliases input", agent, err)
	}
}
