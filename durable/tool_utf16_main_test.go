package durable

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

func toolUTF16Scalar(t *testing.T, text string) goai.UTF16String {
	t.Helper()
	value, err := jsonparse.ParseJSONValue(text, true)
	if err != nil {
		t.Fatal(err)
	}
	return value.(goai.UTF16String)
}
func requireToolUTF16(t *testing.T, value any, unit uint16) {
	t.Helper()
	text, ok := value.(goai.UTF16String)
	if !ok || !reflect.DeepEqual(text.CodeUnits(), []uint16{unit}) {
		t.Errorf("UTF-16 scalar changed: %T %#v", value, value)
	}
}

func TestToolUTF16MainSchema(t *testing.T) {
	high := toolUTF16Scalar(t, `"\ud800"`)
	schema := JSON{"type": "string", "minLength": json.Number("1"), "maxLength": json.Number("1"), "pattern": "^.$"}
	if err := checkSchema(schema, high, 1); err != nil {
		t.Fatal("isolated unit is not one string character", err)
	}
	schema["pattern"] = "^�$"
	if err := checkSchema(schema, high, 1); err == nil {
		t.Fatal("isolated unit replaced with U+FFFD for regex")
	}
	schema["pattern"] = "^.$"
	schema["maxLength"] = json.Number("0")
	if err := checkSchema(schema, high, 1); err == nil {
		t.Fatal("length cap bypassed")
	}
	if !equalJSONValue(goai.UTF16String{}, "") || equalJSONValue(high, "�") {
		t.Fatal("UTF-16 equality conflates code units")
	}
	reader := schemaUTF16Reader{units: []uint16{0xd83d, 0xde00, 0xd800}}
	r, n, err := reader.ReadRune()
	if err != nil || r != '😀' || n != 4 {
		t.Fatal("pair lost", r, n, err)
	}
	r, n, err = reader.ReadRune()
	if err != nil || r != 0xd800 || n != 3 {
		t.Fatal("isolated unit lost", r, n, err)
	}
}

func TestToolUTF16MainCheckpointOwnership(t *testing.T) {
	args := JSON{"text": toolUTF16Scalar(t, `"\ud800"`), "n": json.Number("9007199254740993")}
	cp := toolCheckpoint{StreamingArguments: true, CallID: "c", Offer: toolOffer{Name: "f"}, Arguments: args}
	object, err := dtoObject(cp, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = copyObject(object, DefaultLimits()); err != nil {
		t.Fatal("physical checkpoint escapes generic JSON", err)
	}
	var next toolCheckpoint
	if err = fromObject(object, &next, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if !next.StreamingArguments || !reflect.DeepEqual(next.Arguments, args) {
		t.Fatal("checkpoint ownership/value lost")
	}
	owned, err := copyScopedToolArguments(next.Arguments, next.StreamingArguments, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	owned["text"] = "changed"
	requireToolUTF16(t, next.Arguments["text"], 0xd800)
	if _, err = copyScopedToolArguments(args, false, DefaultLimits()); err == nil {
		t.Fatal("ordinary JSON acquired typed permission")
	}
	bad, err := copyObject(object, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	bad["streamingArguments"] = false
	if err = fromObject(bad, &next, DefaultLimits()); err == nil {
		t.Fatal("carrier without ownership accepted")
	}
	cp.Arguments = JSON{"n": math.Inf(1)}
	if _, err = dtoObject(cp, DefaultLimits()); err == nil {
		t.Fatal("ownership bypassed finite-number guard")
	}
	cp.Arguments = JSON{"caller": callerStreamingMain{}}
	if _, err = dtoObject(cp, DefaultLimits()); err == nil {
		t.Fatal("caller marshaler accepted")
	}
}

func TestToolUTF16MainExecutionReopenHooks(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		original := toolUTF16Scalar(t, `"\ud800"`)
		prepared := toolUTF16Scalar(t, `"\udfff"`)
		rewritten := toolUTF16Scalar(t, `"\ud801"`)
		registry := NewRegistry()
		var prepares, validates, before, after, effects, requests atomic.Int64
		entered := make(chan struct{})
		err := registry.Install(&Extension{Name: "unicode", Tools: []ToolRegistration{{Definition: goai.Tool{Name: "effect", Parameters: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string","minLength":1,"maxLength":1,"pattern":"^.$"}},"required":["text"]}`)}, Implementation: "unicode.effect", Version: 1, ReplaySafe: true,
			PrepareArguments: func(_ context.Context, args JSON) (JSON, error) {
				prepares.Add(1)
				requireToolUTF16(t, args["text"], 0xd800)
				return JSON{"text": prepared}, nil
			},
			Validator: func(_ context.Context, args JSON) (JSON, error) {
				validates.Add(1)
				if _, ok := args["text"].(goai.UTF16String); !ok {
					t.Error("validator lost string")
				}
				return args, nil
			},
			Execute: func(ctx context.Context, args JSON, _ *ToolAPI) (ToolResult, error) {
				requireToolUTF16(t, args["text"], 0xd801)
				args["text"] = "mutated host copy"
				if effects.Add(1) == 1 {
					close(entered)
					<-ctx.Done()
					return ToolResult{}, ctx.Err()
				}
				return ToolResult{Content: "effect done"}, nil
			},
		}}, ToolHooks: ToolHooks{BeforeTool: func(_ context.Context, call goai.ToolCall, _ *HookAPI) (*BeforeToolResult, error) {
			before.Add(1)
			if !call.HasStreamingArguments() {
				t.Error("beforeTool lost ownership")
			}
			requireToolUTF16(t, call.Arguments["text"], 0xdfff)
			return &BeforeToolResult{Arguments: JSON{"text": rewritten}}, nil
		}, AfterTool: func(_ context.Context, call goai.ToolCall, result ToolResult, _ *HookAPI) (*ToolResult, error) {
			after.Add(1)
			if !call.HasStreamingArguments() {
				t.Error("afterTool lost ownership")
			}
			requireToolUTF16(t, call.Arguments["text"], 0xd801)
			return &result, nil
		}}})
		if err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if requests.Add(1) == 1 {
				event := toolAnswer("unicode-call", "effect", JSON{"text": original})
				event.Message.Content[0].SetStreamingArguments(map[string]any{"text": original}, "")
				ch <- event
			} else {
				found := false
				for _, message := range input.Messages {
					for _, block := range message.Content {
						if block.Type == "toolCall" {
							found = true
							requireToolUTF16(t, block.Arguments["text"], 0xd800)
						}
					}
				}
				if !found {
					t.Error("model transcript lost original call")
				}
				ch <- terminal("answer")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		first := openHarness(t, b.store, options)
		conversation := root(t, first, ref)
		submission, err := conversation.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		if err = first.Close(bg); err != nil {
			t.Fatal(err)
		}
		second := openHarness(t, utf16ReopenStore(t, b.store), options)
		handle, err := second.Submission(bg, submission.ID())
		if err != nil {
			t.Fatal(err)
		}
		result := waitSubmission(t, handle)
		if result.Submission.Status != "done" || prepares.Load() != 1 || validates.Load() != 2 || before.Load() != 1 || effects.Load() != 2 || after.Load() < 1 || requests.Load() != 2 {
			t.Fatal("ownership/replay contract failed", result.Submission.Status, prepares.Load(), validates.Load(), before.Load(), effects.Load(), after.Load(), requests.Load())
		}
	})
}

func TestToolUTF16MainCheckpointDiagnosticsAndFences(t *testing.T) {
	high := toolUTF16Scalar(t, `"\ud800"`)
	cp := toolCheckpoint{StreamingArguments: true, CallID: "c", Offer: toolOffer{Name: "f"}, Arguments: JSON{}, ArgumentsError: high.WTF8()}
	object, err := dtoObject(cp, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	var next toolCheckpoint
	if err = fromObject(object, &next, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if next.Arguments == nil || next.ArgumentsError != high.WTF8() {
		t.Fatal("diagnostic-only checkpoint changed")
	}
	for _, mutate := range []func(JSON){func(o JSON) { o["arguments"] = JSON{"unexpected": true} }, func(o JSON) { o["argumentsError"] = "shadow" }, func(o JSON) { o["utf16Arguments"] = []any{"!bad"} }, func(o JSON) { o["unexpected"] = true }} {
		bad, err := copyObject(object, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		mutate(bad)
		next = toolCheckpoint{}
		if err = fromObject(bad, &next, DefaultLimits()); err == nil {
			t.Fatal("malformed checkpoint accepted")
		}
	}
	cp.ArgumentsError = ""
	cp.Arguments = JSON{high.WTF8(): high}
	object, err = dtoObject(cp, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	next = toolCheckpoint{}
	if err = fromObject(object, &next, DefaultLimits()); err != nil || !reflect.DeepEqual(next.Arguments, cp.Arguments) {
		t.Fatal("isolated key/value changed", err)
	}
	cp.StreamingArguments = false
	if _, err = dtoObject(cp, DefaultLimits()); err == nil {
		t.Fatal("ordinary checkpoint acquired UTF-16 permission")
	}
	cp.StreamingArguments = true
	l := DefaultLimits()
	l.MaxDocumentBytes = 32
	if _, err = dtoObject(cp, l); err == nil {
		t.Fatal("physical byte cap bypassed")
	}
}

func TestToolUTF16MainOrdinaryOwnershipAndMixedString(t *testing.T) {
	cp := toolCheckpoint{StreamingArguments: true, Arguments: JSON{"text": "ordinary"}}
	object, err := dtoObject(cp, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := object["utf16Arguments"]; ok {
		t.Fatal("ordinary arguments need no carrier")
	}
	var next toolCheckpoint
	if err = fromObject(object, &next, DefaultLimits()); err != nil || !next.StreamingArguments {
		t.Fatal("ordinary ownership lost on restart", err)
	}
	mixed := toolUTF16Scalar(t, `"\ud83d\ude00\ud800"`)
	schema := JSON{"type": "string", "minLength": json.Number("2"), "maxLength": json.Number("2"), "pattern": "^..$"}
	if err = checkSchema(schema, mixed, 1); err != nil {
		t.Fatal("paired and isolated units miscounted", err)
	}
}
