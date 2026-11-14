package durable

import (
	"context"
	"encoding/json"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
)

func wrapRegistration(name string) ToolRegistration {
	return ToolRegistration{Definition: goai.Tool{Name: name, Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "wrap." + name, Version: 1, ReplaySafe: true, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) { return ToolResult{Content: "base"}, nil }}
}
func TestExtensionWrappersPinnedWinnerOrderingDropReportAndDetachment(t *testing.T) {
	registry := NewRegistry()
	calls := 0
	reports := []error{}
	report := func(err error) { reports = append(reports, err); registry.Installed() }
	section := PromptSection{Key: "same", Render: func(context.Context, PromptInput) (*string, error) { return promptString("base"), nil }}
	if err := registry.Install(&Extension{Name: "base", Tools: []ToolRegistration{wrapRegistration("keep"), wrapRegistration("throw"), wrapRegistration("rename"), wrapRegistration("panic"), wrapRegistration("schema")}, Sections: []PromptSection{section, {Key: "bad", Render: section.Render}, {Key: "rename", Render: section.Render}}}); err != nil {
		t.Fatal(err)
	}
	wraps := []ExtensionWrap{
		{Tool: "keep", WrapTool: func(reg ToolRegistration) (ToolRegistration, error) {
			registry.Installed()
			calls++
			reg.Definition.Description += " first"
			return reg, nil
		}},
		{Tool: "missing", WrapTool: func(ToolRegistration) (ToolRegistration, error) {
			t.Error("missing wrapper ran")
			return ToolRegistration{}, nil
		}},
		{Tool: "throw", WrapTool: func(ToolRegistration) (ToolRegistration, error) { return ToolRegistration{}, errors.New("throw") }},
		{Tool: "rename", WrapTool: func(reg ToolRegistration) (ToolRegistration, error) { reg.Definition.Name = "other"; return reg, nil }},
		{Tool: "panic", WrapTool: func(ToolRegistration) (ToolRegistration, error) { panic("host") }},
		{Tool: "schema", WrapTool: func(reg ToolRegistration) (ToolRegistration, error) {
			reg.Definition.Parameters = json.RawMessage(`{"type":"string"}`)
			return reg, nil
		}},
		{Section: "same", WrapSection: func(value PromptSection) (PromptSection, error) {
			value.Render = func(context.Context, PromptInput) (*string, error) { return promptString("wrapped"), nil }
			return value, nil
		}},
		{Section: "bad", WrapSection: func(PromptSection) (PromptSection, error) { panic("section") }},
		{Section: "rename", WrapSection: func(value PromptSection) (PromptSection, error) { value.Key = "other"; return value, nil }},
		{Section: "instructions", WrapSection: func(PromptSection) (PromptSection, error) {
			t.Error("instructions wrapped")
			return PromptSection{}, nil
		}},
	}
	if err := registry.Install(&Extension{Name: "wraps", Wraps: wraps}); err != nil {
		t.Fatal(err)
	}
	winner := wrapRegistration("keep")
	winner.Definition.Description = "winner"
	if err := registry.Install(&Extension{Name: "winner", Tools: []ToolRegistration{winner}, Wraps: []ExtensionWrap{{Tool: "keep", WrapTool: func(reg ToolRegistration) (ToolRegistration, error) {
		calls++
		reg.Definition.Description += " second"
		return reg, nil
	}}}}); err != nil {
		t.Fatal(err)
	}
	// The caller's wrapper slice cannot change the installed snapshot.
	wraps[0] = ExtensionWrap{Tool: "missing", WrapTool: func(reg ToolRegistration) (ToolRegistration, error) { return reg, nil }}
	offers, _, sections, _, err := registry.selectedGeneration(agentState{}, DefaultLimits(), report)
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) != 1 || offers[0].Description != "winner first second" || calls != 2 || len(reports) != 6 || len(sections) != 1 {
		t.Fatal(offers, calls, reports, sections)
	}
	text, err := sections[0].Render(bg, PromptInput{})
	if err != nil || *text != "wrapped" {
		t.Fatal(text, err)
	}
	filtered := []string{}
	calls = 0
	reports = nil
	offers, _, _, _, err = registry.selectedGeneration(agentState{Tools: &filtered}, DefaultLimits(), report)
	if err != nil || len(offers) != 0 || calls != 2 || len(reports) != 6 {
		t.Fatal("wrapping must precede tool filter", offers, calls, reports, err)
	}
	extensions := []string{"base"}
	reports = nil
	offers, _, _, _, err = registry.selectedGeneration(agentState{Extensions: &extensions}, DefaultLimits(), report)
	if err != nil || len(offers) != 5 || len(reports) != 0 || offers[0].Description != "" {
		t.Fatal("unselected wrapper leaked", offers, reports, err)
	}
	for _, wrap := range []ExtensionWrap{{}, {Tool: "keep", Section: "same", WrapTool: wraps[0].WrapTool}, {Section: "same", WrapTool: wraps[0].WrapTool}} {
		if err := registry.Install(&Extension{Name: "invalid", Wraps: []ExtensionWrap{wrap}}); err == nil {
			t.Fatal("invalid wrapper accepted")
		}
	}
}

func TestExtensionWrapperExecuteAndRecoveryIdentity(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "same", true: "changed"}[changed], func(t *testing.T) {
			registry := NewRegistry()
			var effects atomic.Int64
			entered, release := make(chan struct{}), make(chan struct{})
			first := true
			reg := wrapRegistration("compute")
			reg.Execute = func(ctx context.Context, _ JSON, _ *ToolAPI) (ToolResult, error) {
				effects.Add(1)
				if first {
					first = false
					close(entered)
					<-release
					<-ctx.Done()
					return ToolResult{}, ctx.Err()
				}
				return ToolResult{Content: "base"}, nil
			}
			if err := registry.Register(reg); err != nil {
				t.Fatal(err)
			}
			version := uint64(1)
			install := func() {
				t.Helper()
				if err := registry.Install(&Extension{Name: "decorator", Wraps: []ExtensionWrap{{Tool: "compute", WrapTool: func(reg ToolRegistration) (ToolRegistration, error) {
					reg.Version = version
					execute := reg.Execute
					reg.Execute = func(ctx context.Context, args JSON, api *ToolAPI) (ToolResult, error) {
						value, err := execute(ctx, args, api)
						value.Content = "wrapped:" + value.Content
						return value, err
					}
					return reg, nil
				}}}}); err != nil {
					t.Fatal(err)
				}
			}
			install()
			var modelCalls atomic.Int64
			ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				if modelCalls.Add(1) == 1 {
					ch <- toolAnswer("call", "compute", JSON{})
				} else {
					if !changed {
						found := false
						for _, message := range input.Messages {
							if message.Role == goai.RoleToolResult {
								for _, block := range message.Content {
									if strings.Contains(block.Text, "wrapped:base") {
										found = true
									}
								}
							}
						}
						if !found {
							t.Error("wrapped executor did not produce result", input.Messages)
						}
					}
					ch <- terminal("done")
				}
				close(ch)
				return ch
			})
			options.Registry = registry
			store, dir := newSQLiteTestStore(t)
			h := openHarness(t, store, options)
			sub, err := root(t, h, ref).Submit(bg, Input{Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			<-entered
			closed := make(chan error, 1)
			go func() { closed <- h.Close(bg) }()
			<-h.life.Done()
			close(release)
			if err := <-closed; err != nil {
				t.Fatal(err)
			}
			if changed {
				version = 2
				install()
			}
			store, err = openSQLiteTestDirectory(dir, SQLiteOptions{})
			if err != nil {
				t.Fatal(err)
			}
			h = openHarness(t, store, options)
			if effects.Load() != 1 {
				t.Fatal("open executed wrapper")
			}
			resumed, err := h.Submission(bg, sub.ID())
			if err != nil {
				t.Fatal(err)
			}
			waitSubmission(t, resumed)
			// Stored+current replay-safe policies govern replay; implementation
			// version changes do not override the reference replay contract.
			want := int64(2)
			if effects.Load() != want {
				t.Fatal("wrapper replay identity", effects.Load(), want)
			}
			snapshot, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			for _, task := range snapshot.Tasks {
				if task.Kind == "pi.tool" {
					var cp toolCheckpoint
					if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
						t.Fatal(err)
					}
					if cp.ErrorCode != "" {
						t.Fatal(cp.ErrorCode)
					}
				}
			}
		})
	}
}

// Source harness/define.ts:22-26,50-52 and registry.ts:95-102 use tool names
// as string map keys; section/document identifier grammars do not apply.
func TestSourceToolNamesAreStringsAcrossRegistrationWrappingAndNestedCalls(t *testing.T) {
	names := []string{"", "9tool", "_tool", " Mixed case: note/v1", "工具/Δ", strings.Repeat("x", 129), "nul\x00name"}
	registry := NewRegistry()
	var effects atomic.Int64
	tools := make([]ToolRegistration, 0, len(names))
	wraps := make([]ExtensionWrap, 0, len(names))
	for _, name := range names {
		reg := wrapRegistration(name)
		reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: name}, nil
		}
		tools = append(tools, reg)
		wraps = append(wraps, ExtensionWrap{Tool: name, WrapTool: func(reg ToolRegistration) (ToolRegistration, error) {
			reg.Definition.Description = "wrapped " + name
			return reg, nil
		}})
	}
	if err := registry.Install(&Extension{Name: "names", Tools: tools, Wraps: wraps}); err != nil {
		t.Fatal("source string tool names rejected", err)
	}
	offers, _, _, _, err := registry.selectedGeneration(agentState{}, DefaultLimits(), func(err error) { t.Error(err) })
	if err != nil || len(offers) != len(names) {
		t.Fatal(offers, err)
	}
	for i, name := range names {
		if offers[i].Name != name || offers[i].Description != "wrapped "+name {
			t.Error("source tool name or wrapper target changed", i, offers[i])
		}
		if err := validateToolControl(&ToolControl{AddTools: []string{name}}, DefaultLimits()); err != nil {
			t.Error("source addTools string rejected", name, err)
		}
		for _, message := range []goai.Message{{Role: goai.RoleSystem, ToolsAdded: []goai.Tool{{Name: name, Parameters: json.RawMessage(`{"type":"object"}`)}}}, {Role: goai.RoleSystem, ToolsRemoved: []goai.ToolReference{{Name: name}}}} {
			if _, err := contributionReceipt(message, DefaultLimits()); err != nil {
				t.Error("source tool contribution string rejected", name, err)
			}
		}
	}
	if err := registry.Install(&Extension{Name: "duplicate", Tools: []ToolRegistration{tools[0], tools[0]}}); err == nil {
		t.Fatal("duplicate empty name accepted")
	}
	for _, invalid := range []string{string([]byte{0xff}), strings.Repeat("x", DefaultLimits().MaxStringBytes+1)} {
		if err := registry.Register(wrapRegistration(invalid)); err == nil {
			t.Error("tool name bypassed JSON string budget")
		}
		if _, err := agentSelectionNames([]string{invalid}); err == nil {
			t.Error("selection name bypassed JSON string budget")
		}
		if err := validateToolControl(&ToolControl{AddTools: []string{invalid}}, DefaultLimits()); err == nil {
			t.Error("control name bypassed JSON string budget")
		}
	}
	if validKind("") || validKind(" Mixed case: note/v1") {
		t.Fatal("tool-name acceptance changed document/task kind validation")
	}
	definition := taskDefinition(t, "source.tool-names", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		for i, name := range names {
			for repeat := 0; repeat < 2; repeat++ {
				result, err := r.CallTool(ctx, strings.Repeat("k", i+1), name, JSON{})
				if err != nil {
					return err
				}
				if result.Content != name || result.IsError {
					t.Error("source nested name result changed", name, result)
				}
			}
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	if _, err := registry.RegisterTask(definition); err != nil {
		t.Fatal(err)
	}
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, Options{Registry: registry})
	conversation := root(t, h, ModelRef{Provider: "source", ID: "model"})
	if err := conversation.ConfigurePatch(bg, AgentPatch{Tools: &names}); err != nil {
		t.Fatal("source tool selection strings rejected", err)
	}
	record := waitPublicTask(t, h, createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
	if record.State.Outcome.Status != "completed" || effects.Load() != int64(len(names)) {
		t.Error("source name execution/replay changed", record.State.Outcome, effects.Load())
	}
}
