package durable

import (
	"context"
	"encoding/json"
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
