package durable

import (
	"context"
	"encoding/json"
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
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "schema", Description: "schema", Parameters: json.RawMessage(tc.schema)}, Implementation: "source.schema", Version: 1, Execute: func(ctx context.Context, args JSON, api *ToolAPI) (ToolResult, error) {
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
