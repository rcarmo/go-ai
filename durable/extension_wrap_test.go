package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func wrapRegistration(name string) ToolRegistration {
	return ToolRegistration{Definition: goai.Tool{Name: name, Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "wrap." + name, Version: 1, ReplaySafe: true, Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) { return ToolResult{Content: "base"}, nil }}
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
	// Source wrappers do not compile parameter schemas. A string declaration
	// remains selected; only throws, renames and invalid sections are dropped.
	if len(offers) != 2 || offers[0].Description != "winner first second" || offers[1].Name != "schema" || !equalJSONValue(offers[1].Schema, JSON{"type": "string"}) || calls != 2 || len(reports) != 5 || len(sections) != 1 {
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
	if err != nil || len(offers) != 0 || calls != 2 || len(reports) != 5 {
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
			reg.Execute = func(ctx context.Context, _ any, _ *ToolAPI) (ToolResult, error) {
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
					reg.Execute = func(ctx context.Context, rawToolArguments any, api *ToolAPI) (ToolResult, error) {
						args := rawToolArguments.(JSON)
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
		reg.Execute = func(context.Context, any, *ToolAPI) (ToolResult, error) {
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

// harness-tool-parks.test.ts:608-702. A ToolPark pointer represents the
// returned park identity; the shared park is initially returned by both bases.
func TestSourceV160ToolWrapperParks(t *testing.T) {
	for _, mode := range []string{"pass", "own", "shared", "stale"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(bg, 8*time.Second)
			defer cancel()
			native, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			storage := &toolParkTestStorage{Storage: native, parked: make(chan ID, 4)}
			token := testSignalAnswer(t)
			key, err := token.Key("done")
			if err != nil {
				t.Fatal(err)
			}
			shared := &ToolPark{Sleep: &ToolSleep{Signals: []SignalKey{key}}}
			var kept atomic.Pointer[ToolPark] // one call only in stale mode
			baseEntered, releaseBase := make(chan ID, 2), make(chan struct{})
			var requests, runs, staleReturns atomic.Int32
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				if requests.Add(1) == 1 {
					answer := toolAnswer("a", "ci", JSON{})
					if mode == "shared" {
						answer.Message.Content = append(answer.Message.Content, toolAnswer("b", "ci", JSON{}).Message.Content[0])
					}
					ch <- answer
				} else {
					ch <- terminal("done")
				}
				close(ch)
				return ch
			})
			options.Registry = NewRegistry()
			if err := options.Registry.Register(ToolRegistration{Definition: goai.Tool{Name: "ci", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "test.wrap.park", Version: 1,
				Execute: func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
					runs.Add(1)
					signal, err := api.ReadSignal(ctx, token, "done")
					if err != nil {
						return ToolResult{}, err
					}
					if signal == nil {
						if mode == "shared" {
							baseEntered <- api.TaskID()
							select {
							case <-releaseBase:
							case <-ctx.Done():
								return ToolResult{}, ctx.Err()
							}
						}
						return ToolResult{Park: shared}, nil
					}
					return ToolResult{Content: "ok"}, nil
				},
			}); err != nil {
				t.Fatal(err)
			}
			if err := options.Registry.Install(&Extension{Name: "decorator", Wraps: []ExtensionWrap{{Tool: "ci", WrapTool: func(inner ToolRegistration) (ToolRegistration, error) {
				execute := inner.Execute
				inner.Execute = func(ctx context.Context, args any, api *ToolAPI) (ToolResult, error) {
					if stale := kept.Load(); mode == "stale" && stale != nil {
						staleReturns.Add(1)
						return ToolResult{Park: stale}, nil
					}
					result, err := execute(ctx, args, api)
					if result.Park != nil {
						if mode == "own" {
							copied := *result.Park
							result.Park = &copied
						}
						if mode == "stale" {
							kept.Store(result.Park)
						}
					}
					return result, err
				}
				return inner, nil
			}}}}); err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, storage, options)
			defer releaseTaskGate(releaseBase)
			c := root(t, h, ref)
			sub, err := c.Submit(ctx, Input{Type: "input", Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			var baseTasks map[ID]bool
			if mode == "shared" {
				first, second := watchReceive(t, baseEntered), watchReceive(t, baseEntered)
				if first == 0 || second == 0 || first == second {
					t.Fatalf("shared park needs two simultaneous base tasks: %d and %d", first, second)
				}
				baseTasks = map[ID]bool{first: true, second: true}
				close(releaseBase)
			}
			var parked []ID
			if mode != "own" {
				count := 1
				if mode == "shared" {
					count = 2
				}
				for range count {
					select {
					case id := <-storage.parked:
						if mode == "shared" && !baseTasks[id] {
							t.Fatalf("unexpected or duplicate parked receipt for shared park, task %d", id)
						}
						delete(baseTasks, id)
						parked = append(parked, id)
					case <-ctx.Done():
						t.Fatal("missing park", ctx.Err())
					}
				}
				for _, id := range parked {
					task, found, err := h.Task(ctx, id)
					if err != nil || !found || task.State.Status != "sleeping" || !reflect.DeepEqual(task.State.Signals, []SignalKey{key}) {
						t.Fatalf("parked task %d has no independent wake claim: %+v, %v, %v", id, task.State, found, err)
					}
					if got, err := h.Signal(ctx, id, token, "done", JSON{"text": "wrapped"}); err != nil || got != SignalDelivered {
						t.Fatalf("signal = %q, %v", got, err)
					}
				}
			}
			// For the forged branch, a park is itself a failure, not a timeout.
			settled := make(chan error, 1)
			go func() {
				value, err := sub.Wait(ctx)
				if err == nil && value.Record.Status != "done" {
					err = fmt.Errorf("submission = %s", value.Record.Status)
				}
				settled <- err
			}()
		settling:
			for {
				select {
				case id := <-storage.parked:
					// Signal delivery can update the memo while the task is still sleeping.
					if mode == "own" {
						t.Fatalf("wrapper's forged park was saved: %d", id)
					}
				case err := <-settled:
					if err != nil {
						t.Fatal(err)
					}
					break settling
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			result := v160ToolResult(t, ctx, c, "a")
			if mode == "own" || mode == "stale" {
				if !result.IsError || !strings.Contains(v160ToolResultText(result), "a wrapper returned a park its base did not return") {
					t.Fatalf("invalid wrapper park result = %q", v160ToolResultText(result))
				}
				if runs.Load() != 1 || mode == "stale" && (kept.Load() != shared || staleReturns.Load() != 1) {
					t.Fatalf("expected one base approval, no fresh approval for copied or stale return: runs=%d stale=%d", runs.Load(), staleReturns.Load())
				}
				return
			}
			if result.IsError || v160ToolResultText(result) != "ok" || runs.Load() != int32(len(parked)*2) {
				t.Fatalf("result = %q; runs=%d", v160ToolResultText(result), runs.Load())
			}
			if mode == "shared" {
				if other := v160ToolResult(t, ctx, c, "b"); other.IsError || v160ToolResultText(other) != "ok" {
					t.Fatalf("second result = %+v", other)
				}
				page, err := c.Entries(ctx, EntryQuery{Order: Ascending}, 10, nil)
				if err != nil || page.Next != nil {
					t.Fatal(page, err)
				}
				var resultErrors []bool
				for _, entry := range page.Items {
					for _, message := range entry.Model {
						if message.Role == goai.RoleToolResult {
							resultErrors = append(resultErrors, message.IsError)
						}
					}
				}
				if !reflect.DeepEqual(resultErrors, []bool{false, false}) {
					t.Fatalf("shared park terminal errors = %v, want [false false]", resultErrors)
				}
				if !reflect.DeepEqual(shared, &ToolPark{Sleep: &ToolSleep{Signals: []SignalKey{key}}}) {
					t.Fatal("runtime mutated the shared park descriptor", shared)
				}
			}
		})
	}
}
