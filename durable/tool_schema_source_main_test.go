package durable

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source validateToolArguments compiles the complete parameters schema at use,
// coerces a detached call, and never restricts registration to a native subset.
func TestSourceToolParameterSchemasAndCoercion(t *testing.T) {
	for _, tc := range []struct {
		name, schema string
		input, want  JSON
	}{
		{"empty", `{}`, JSON{"anything": true}, JSON{"anything": true}},
		{"unknown-keyword", `{"type":"object","unknown":true}`, JSON{"x": "kept"}, JSON{"x": "kept"}},
		{"non-array-required", `{"type":"object","required":true,"unknown":true}`, JSON{}, JSON{}},
		{"integer-coercion", `{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`, JSON{"n": "2"}, JSON{"n": 2}},
		{"local-ref", `{"type":"object","properties":{"n":{"$ref":"#/$defs/int"}},"required":["n"],"$defs":{"int":{"type":"integer","minimum":1}}}`, JSON{"n": "3"}, JSON{"n": 3}},
		{"pattern-properties", `{"type":"object","patternProperties":{"^x":{"type":"string"}},"additionalProperties":false}`, JSON{"x": 4}, JSON{"x": "4"}},
		{"any-of", `{"anyOf":[{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]},{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}]}`, JSON{"n": "5"}, JSON{"n": 5}},
		{"nullable-union", `{"type":"object","properties":{"n":{"type":["integer","null"]}},"required":["n"]}`, JSON{"n": nil}, JSON{"n": nil}},
		{"required-at-use", `{"type":"object","required":["absent"]}`, JSON{}, nil},
		{"invalid-pattern-at-use", `{"type":"object","properties":{"x":{"type":"string","pattern":"["}}}`, JSON{"x": "value"}, nil},
		{"unsatisfiable-bounds-at-use", `{"type":"object","properties":{"n":{"type":"number","minimum":10,"maximum":1}}}`, JSON{"n": 2}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				registry := NewRegistry()
				var effects, requests atomic.Int64
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "schema", Description: "schema", Parameters: json.RawMessage(tc.schema)}, Implementation: "source.schema", Version: 1, Execute: func(ctx context.Context, rawToolArguments any, api *ToolAPI) (ToolResult, error) {
					args := rawToolArguments.(JSON)
					effects.Add(1)
					if !equalJSONValue(args, tc.want) {
						t.Error("source coerced arguments", args, tc.want)
					}
					record, found, err := api.Task(ctx, api.TaskID())
					if err != nil || !found || !equalJSONValue(record.State.Checkpoint, JSON{"phase": "execute", "arguments": tc.want, "replay": "unsafe"}) {
						t.Error("coerced execute intent", record, err)
					}
					args["host-mutation"] = true
					return ToolResult{Content: "done"}, nil
				}}); err != nil {
					t.Fatal("source parameter declaration rejected", err)
				}
				ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					events := make(chan goai.Event, 1)
					if requests.Add(1) == 1 {
						events <- toolAnswer("schema-call", "schema", tc.input)
					} else {
						results := 0
						for _, message := range input.Messages {
							if message.Role == goai.RoleToolResult {
								results++
								if message.IsError != (tc.want == nil) {
									t.Error("schema result error arm", message)
								}
							}
						}
						if results != 1 {
							t.Error("schema result missing", results)
						}
						calls := 0
						for _, message := range input.Messages {
							for _, block := range message.Content {
								if block.Type == "toolCall" {
									calls++
									if !equalJSONValue(block.Arguments, tc.input) {
										t.Error("coercion mutated original transcript call", block.Arguments, tc.input)
									}
								}
							}
						}
						if calls != 1 {
							t.Error("original call missing", calls)
						}
						events <- terminal("answer")
					}
					close(events)
					return events
				})
				options.Registry = registry
				h := openHarness(t, b.store, options)
				conversation := root(t, h, ref)
				submission, err := conversation.Submit(bg, Input{Content: "go"})
				if err != nil {
					t.Fatal(err)
				}
				settled := waitSubmission(t, submission)
				wantEffects := int64(1)
				if tc.want == nil {
					wantEffects = 0
				}
				if settled.Record.Status != "done" || effects.Load() != wantEffects || requests.Load() != 2 {
					t.Fatal("source schema effect/answer", settled, effects.Load(), requests.Load())
				}
			})
		})
	}
}

// The source schema result is a value union, even though model calls begin with
// object arguments. Hooks, execute intents and callbacks must retain that value.
func TestSourceToolArrayArgumentsThroughPublicExecution(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		registry := NewRegistry()
		var effects, requests, before, after atomic.Int64
		want := []any{JSON{}}
		if err := registry.Install(&Extension{Name: "array", Tools: []ToolRegistration{{Definition: goai.Tool{Name: "array", Parameters: json.RawMessage(`{"type":"array"}`)}, Implementation: "source.array", Version: 1, Execute: func(ctx context.Context, args any, api *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			if !equalJSONValue(args, want) {
				t.Error("source execute array", args)
			}
			record, found, err := api.Task(ctx, api.TaskID())
			if err != nil || !found || !equalJSONValue(record.State.Checkpoint, JSON{"phase": "execute", "arguments": want, "replay": "unsafe"}) {
				t.Error("source array execute checkpoint", record, err)
			}
			checkpointJSON, err := json.Marshal(record.State.Checkpoint)
			if err != nil || !strings.Contains(string(checkpointJSON), `"arguments":[{}]`) {
				t.Error("source array checkpoint value JSON", string(checkpointJSON), err)
			}
			any(args).([]any)[0] = "host mutation"
			return ToolResult{Content: "done"}, nil
		}}}, ToolHooks: ToolHooks{BeforeTool: func(_ context.Context, call goai.ToolCall, _ *HookAPI) (*BeforeToolResult, error) {
			before.Add(1)
			if !equalJSONValue(call.Arguments, want) {
				t.Error("source beforeTool array", call.Arguments)
			}
			return nil, nil
		}, AfterTool: func(_ context.Context, call goai.ToolCall, result ToolResult, _ *HookAPI) (*ToolResult, error) {
			after.Add(1)
			if !equalJSONValue(call.Arguments, JSON{}) {
				t.Error("source afterTool original object", call.Arguments)
			}
			return &result, nil
		}}}); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			events := make(chan goai.Event, 1)
			if requests.Add(1) == 1 {
				events <- toolAnswer("array-call", "array", JSON{})
			} else {
				results, calls := 0, 0
				for _, message := range input.Messages {
					if message.Role == goai.RoleToolResult {
						results++
						if message.IsError {
							t.Error("source array call rejected", message)
						}
					}
					for _, block := range message.Content {
						if block.Type == "toolCall" {
							calls++
							if !equalJSONValue(block.Arguments, JSON{}) {
								t.Error("source array rewrote original model call", block.Arguments)
							}
						}
					}
				}
				if results != 1 || calls != 1 {
					t.Error("source array call/result identity", calls, results)
				}
				events <- terminal("answer")
			}
			close(events)
			return events
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		submission, err := conversation.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		settled := waitSubmission(t, submission)
		if settled.Record.Status != "done" || effects.Load() != 1 || requests.Load() != 2 || before.Load() != 1 || after.Load() != 1 {
			t.Fatal("source array execution/hook counts", settled, effects.Load(), requests.Load(), before.Load(), after.Load())
		}
	})
}

func TestSourceToolArrayArgumentsUTF16SafeReplay(t *testing.T) {
	for _, throwing := range []bool{false, true} {
		t.Run(map[bool]string{false: "returns", true: "throws"}[throwing], func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				original := toolUTF16Scalar(t, `"\ud800"`)
				prepared := toolUTF16Scalar(t, `"\udfff"`)
				rewritten := toolUTF16Scalar(t, `"\ud801"`)
				key := original.WTF8()
				var effects, requests, prepares, before, after atomic.Int64
				entered := make(chan struct{})
				var taskID ID
				checkArray := func(value any, unit uint16) {
					array, ok := value.([]any)
					if !ok || len(array) != 1 {
						t.Error("source UTF-16 array shape", value)
						return
					}
					object, ok := array[0].(map[string]any)
					if !ok || len(object) != 1 {
						t.Error("source UTF-16 array object", array[0])
						return
					}
					requireToolUTF16(t, object[key], unit)
				}
				registry := NewRegistry()
				if err := registry.Install(&Extension{Name: "array-replay", Tools: []ToolRegistration{{Definition: goai.Tool{Name: "array_replay", Parameters: json.RawMessage(`{"type":"array","items":{"type":"object","additionalProperties":{"type":"string","minLength":1,"maxLength":1,"pattern":"^.$"}}}`)}, Implementation: "source.array-replay", Version: 1, ReplaySafe: true,
					PrepareArguments: func(_ context.Context, value any) (any, error) {
						prepares.Add(1)
						requireToolUTF16(t, value.(JSON)[key], 0xd800)
						return JSON{key: prepared}, nil
					},
					Execute: func(ctx context.Context, args any, api *ToolAPI) (ToolResult, error) {
						run := effects.Add(1)
						checkArray(args, 0xd801)
						record, found, err := api.Task(ctx, api.TaskID())
						if err != nil || !found {
							t.Error("source array task", record, err)
						} else {
							wire, err := json.Marshal(record)
							if err != nil || !strings.Contains(string(wire), `"arguments":[{"\ud800":"\ud801"}]`) {
								t.Error("source array checkpoint JSON", string(wire), err)
							}
							checkpointJSON, err := json.Marshal(record.State.Checkpoint)
							if err != nil || !strings.Contains(string(checkpointJSON), `"arguments":[{"\ud800":"\ud801"}]`) {
								t.Error("source UTF-16 array checkpoint value JSON", string(checkpointJSON), err)
							}
							view, err := NewImmutableValue(record.State.Checkpoint, DefaultLimits())
							if err != nil {
								t.Error("source array checkpoint readonly", err)
							} else if arguments, found := view.Get("arguments"); !found || !arguments.IsArray() || arguments.Len() != 1 {
								t.Error("source array checkpoint readonly shape", arguments)
							}
						}
						args.([]any)[0] = "host mutation"
						if run == 1 {
							taskID = api.TaskID()
							close(entered)
							<-ctx.Done()
							if throwing {
								return ToolResult{}, ctx.Err()
							}
						}
						return ToolResult{Content: "done"}, nil
					},
				}}, ToolHooks: ToolHooks{BeforeTool: func(_ context.Context, call goai.ToolCall, _ *HookAPI) (*BeforeToolResult, error) {
					before.Add(1)
					checkArray(call.Arguments, 0xdfff)
					return &BeforeToolResult{Arguments: []any{JSON{key: rewritten}}}, nil
				}, AfterTool: func(_ context.Context, call goai.ToolCall, result ToolResult, _ *HookAPI) (*ToolResult, error) {
					after.Add(1)
					requireToolUTF16(t, call.Arguments.(map[string]any)[key], 0xd800)
					return &result, nil
				}}}); err != nil {
					t.Fatal(err)
				}
				ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					events := make(chan goai.Event, 1)
					if requests.Add(1) == 1 {
						event := toolAnswer("array-replay", "array_replay", JSON{key: original})
						event.Message.Content[0].SetStreamingArguments(map[string]any{key: original}, "")
						events <- event
					} else {
						calls, results := 0, 0
						for _, message := range input.Messages {
							if message.Role == goai.RoleToolResult {
								results++
								if message.IsError {
									t.Error("source array replay result", message)
								}
							}
							for _, block := range message.Content {
								if block.Type == "toolCall" {
									calls++
									requireToolUTF16(t, block.Arguments[key], 0xd800)
								}
							}
						}
						if calls != 1 || results != 1 {
							t.Error("source array replay pairing", calls, results)
						}
						events <- terminal("answer")
					}
					close(events)
					return events
				})
				options.Registry = registry
				first := openHarness(t, b.store, options)
				conversation := root(t, first, ref)
				submission, err := conversation.Submit(bg, Input{Content: "go"})
				if err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, entered)
				record, found, err := first.Task(bg, taskID)
				if err != nil || !found {
					t.Fatal(record, err)
				}
				wire, err := json.Marshal(record)
				if err != nil || !strings.Contains(string(wire), `"arguments":[{"\ud800":"\ud801"}]`) {
					t.Fatal("host mutated persisted array", string(wire), err)
				}
				if err := first.Close(bg); err != nil {
					t.Fatal(err)
				}
				second := openHarness(t, utf16ReopenStore(t, b.store), options)
				handle, err := second.Submission(bg, submission.ID())
				if err != nil {
					t.Fatal(err)
				}
				settled := waitSubmission(t, handle)
				wantAfter := int64(2)
				if throwing {
					wantAfter = 1
				}
				if settled.Record.Status != "done" || effects.Load() != 2 || prepares.Load() != 1 || before.Load() != 1 || after.Load() != wantAfter || requests.Load() != 2 {
					t.Fatal("source array replay counts", settled, effects.Load(), prepares.Load(), before.Load(), after.Load(), requests.Load())
				}
			})
		})
	}
}

// Port of pinned harness-tools.test.ts: repairs arguments before validation,
// preserves the throwing repair's text, and never rewrites the stored call.
func TestSourceToolPrepareArgumentsErrorKeepsSourceMessage(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var prepares, effects, requests atomic.Int64
		registry := NewRegistry()
		if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "echo", Parameters: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)}, Implementation: "source.repair", Version: 1,
			PrepareArguments: func(_ context.Context, value any) (any, error) {
				prepares.Add(1)
				args := value.(JSON)
				if args["text"] == "throw" {
					return nil, errors.New("cannot repair")
				}
				return JSON{"text": "#7"}, nil
			},
			Execute: func(ctx context.Context, value any, api *ToolAPI) (ToolResult, error) {
				effects.Add(1)
				if !equalJSONValue(value, JSON{"text": "#7"}) {
					t.Error("source repaired execution", value)
				}
				record, found, err := api.Task(ctx, api.TaskID())
				if err != nil || !found || !equalJSONValue(record.State.Checkpoint, JSON{"phase": "execute", "arguments": JSON{"text": "#7"}, "replay": "unsafe"}) {
					t.Error("source repaired intent", record, err)
				}
				return ToolResult{Blocks: []goai.ContentBlock{}}, nil
			},
		}); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			events := make(chan goai.Event, 1)
			if requests.Add(1) == 1 {
				event := toolAnswer("fixed", "echo", JSON{"text": 7})
				event.Message.Content = append(event.Message.Content, goai.ContentBlock{Type: "toolCall", ID: "broken", Name: "echo", Arguments: map[string]any{"text": "throw"}})
				events <- event
			} else {
				results := map[string]goai.Message{}
				calls := map[string]map[string]any{}
				for _, message := range input.Messages {
					if message.Role == goai.RoleToolResult {
						results[message.ToolCallID] = message
					}
					for _, block := range message.Content {
						if block.Type == "toolCall" {
							calls[block.ID] = block.Arguments
						}
					}
				}
				if len(results) != 2 || results["fixed"].IsError || goai.ContentText(results["fixed"].Content) != "" || !results["broken"].IsError || goai.ContentText(results["broken"].Content) != "<harness>\n[error] cannot repair\n</harness>" {
					t.Error("source throwing repair result", results)
				}
				if len(calls) != 2 || !equalJSONValue(calls["fixed"], JSON{"text": 7}) || !equalJSONValue(calls["broken"], JSON{"text": "throw"}) {
					t.Error("source stored original repair calls", calls)
				}
				events <- terminal("answer")
			}
			close(events)
			return events
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		submission, err := conversation.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		if result := waitSubmission(t, submission); result.Record.Status != "done" || prepares.Load() != 2 || effects.Load() != 1 || requests.Load() != 2 {
			t.Fatal("source repair effects/answer", result.Record, prepares.Load(), effects.Load(), requests.Load())
		}
	})
}

func TestSourceToolPrepareErrorPreservesIsolatedUTF16(t *testing.T) {
	for _, decorated := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "decorated"}[decorated], func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				scalar := toolUTF16Scalar(t, `"\ud800"`)
				text := scalar.WTF8()
				var effects, requests atomic.Int64
				registry := NewRegistry()
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "unicode_error", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "source.unicode-error", Version: 1, PrepareArguments: func(context.Context, any) (any, error) { return nil, errors.New(text) }, Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) { effects.Add(1); return ToolResult{}, nil }}); err != nil {
					t.Fatal(err)
				}
				ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					events := make(chan goai.Event, 1)
					if requests.Add(1) == 1 {
						events <- toolAnswer("unicode-error-call", "unicode_error", JSON{})
					} else {
						results := 0
						for _, message := range input.Messages {
							if message.Role == goai.RoleToolResult {
								results++
								if !message.IsError || goai.ContentText(message.Content) != "<harness>\n[error] "+text+"\n</harness>" {
									t.Error("source UTF-16 preparation error", message)
								}
							}
						}
						if results != 1 {
							t.Error("missing UTF-16 preparation result", results)
						}
						events <- terminal("answer")
					}
					close(events)
					return events
				})
				options.Registry = registry
				store := b.store
				if decorated {
					store = &sourceStorageDecorator{Storage: b.store}
				}
				h := openHarness(t, store, options)
				conversation := root(t, h, ref)
				published := make(chan ReadonlyValue, 1)
				observer, err := h.ObserveCommits(CommitObserver{Advance: func(_ context.Context, frame CommitPublication) error {
					for index := range frame.Changes.Len() {
						change, _ := frame.Changes.Index(index)
						kind, _ := change.Get("type")
						if kind.String() != "entry" {
							continue
						}
						value, _ := change.Get("value")
						entryKind, _ := value.Get("kind")
						if entryKind.String() == "pi.tool-result" {
							published <- value
						}
					}
					return nil
				}, End: func(WatchEnd) error { return nil }})
				if err != nil {
					t.Fatal(err)
				}
				defer observer.Detach()
				submission, err := conversation.Submit(bg, Input{Content: "go"})
				if err != nil {
					t.Fatal(err)
				}
				if result := waitSubmission(t, submission); result.Record.Status != "done" || effects.Load() != 0 || requests.Load() != 2 {
					t.Fatal("UTF-16 preparation settlement", result.Record, effects.Load(), requests.Load())
				}
				entries, err := conversation.Entries(bg, EntryQuery{}, 20, nil)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, entry := range entries.Items {
					if entry.Kind != "pi.tool-result" {
						continue
					}
					found = true
					diagnostics, ok := generationObject(entry.Data)["diagnostics"].([]any)
					if !ok || len(diagnostics) != 1 {
						t.Fatal("UTF-16 stored diagnostic absent", entry.Data)
					}
					diagnostic := generationObject(diagnostics[0])
					if diagnostic["code"] != "invalid_arguments" {
						t.Error("UTF-16 stored diagnostic code", diagnostic)
					}
					requireToolUTF16(t, diagnostic["message"], 0xd800)
					wire, err := json.Marshal(entry)
					if err != nil || !strings.Contains(string(wire), `\ud800`) || strings.Contains(string(wire), "�") || strings.Contains(string(wire), `"utf16Arguments"`) {
						t.Error("UTF-16 preparation public JSON", string(wire), err)
					}
				}
				if !found {
					t.Error("UTF-16 preparation result not stored")
				}
				value := watchReceive(t, published)
				data, _ := value.Get("data")
				diagnostics, _ := data.Get("diagnostics")
				diagnostic, _ := diagnostics.Index(0)
				message, _ := diagnostic.Get("message")
				requireToolUTF16(t, message.Copy(), 0xd800)
				view, err := conversation.View(bg)
				if err != nil {
					t.Fatal(err)
				}
				viewFound := false
				for index := range view.Entries.Len() {
					entry, _ := view.Entries.Index(index)
					kind, _ := entry.Get("kind")
					if kind.String() != "pi.tool-result" {
						continue
					}
					viewFound = true
					data, _ := entry.Get("data")
					diagnostics, _ := data.Get("diagnostics")
					diagnostic, _ := diagnostics.Index(0)
					message, _ := diagnostic.Get("message")
					requireToolUTF16(t, message.Copy(), 0xd800)
				}
				if !viewFound {
					t.Error("UTF-16 preparation result absent from readonly view")
				}
				viewJSON, err := json.Marshal(view)
				if err != nil || !strings.Contains(string(viewJSON), `\ud800`) || strings.Contains(string(viewJSON), `"utf16Arguments"`) {
					t.Error("UTF-16 readonly public JSON", string(viewJSON), err)
				}
			})
		})
	}
}
