package durable

import (
	"context"
	"encoding/json"
	"reflect"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestNestedUTF16MainReopenHooksAndReaders(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		original := toolUTF16Scalar(t, `"\ud800"`)
		prepared := toolUTF16Scalar(t, `"\udfff"`)
		rewrite := toolUTF16Scalar(t, `"\ud801"`)
		key := original.WTF8()
		entered := make(chan struct{})
		var prepares, before, after, effects atomic.Int64
		registry := NewRegistry()
		err := registry.Install(&Extension{Name: "nested-unicode", Tools: []ToolRegistration{{Definition: goai.Tool{Name: "effect", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "nested.unicode", Version: 1, ReplaySafe: true,
			PrepareArguments: func(_ context.Context, args JSON) (JSON, error) {
				prepares.Add(1)
				requireToolUTF16(t, args[key], 0xd800)
				return JSON{key: prepared}, nil
			},
			Execute: func(ctx context.Context, args JSON, _ *ToolAPI) (ToolResult, error) {
				requireToolUTF16(t, args[key], 0xd801)
				args[key] = "executor mutation"
				if effects.Add(1) == 1 {
					close(entered)
					<-ctx.Done()
					return ToolResult{}, ctx.Err()
				}
				return ToolResult{Content: "done"}, nil
			},
		}}, ToolHooks: ToolHooks{BeforeTool: func(_ context.Context, call goai.ToolCall, _ *HookAPI) (*BeforeToolResult, error) {
			before.Add(1)
			if !call.HasStreamingArguments() {
				t.Error("before hook lost ownership")
			}
			requireToolUTF16(t, call.Arguments[key], 0xdfff)
			return &BeforeToolResult{Arguments: JSON{key: rewrite}}, nil
		}, AfterTool: func(_ context.Context, call goai.ToolCall, result ToolResult, _ *HookAPI) (*ToolResult, error) {
			after.Add(1)
			if !call.HasStreamingArguments() {
				t.Error("after hook lost ownership")
			}
			requireToolUTF16(t, call.Arguments[key], 0xd801)
			return &result, nil
		}}})
		if err != nil {
			t.Fatal(err)
		}
		definition := taskDefinition(t, "nested-unicode-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if _, err := r.StartTool(ctx, "ordinary", "effect", JSON{key: original}); err == nil {
				t.Error("ordinary API acquired UTF-16 permission")
			}
			result, err := r.CallToolWithArguments(ctx, "stable", "effect", goai.StreamingArgumentsJSON{key: original})
			if err != nil {
				return err
			}
			if result.IsError || result.Content != "done" {
				t.Error("typed nested result failed", result)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err = registry.RegisterTask(definition); err != nil {
			t.Fatal(err)
		}
		first := openHarness(t, b.store, Options{Registry: registry})
		id := createPublicTask(t, first, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err = first.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		stream, err := first.WatchEvents(bg, 1)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := stream.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.NestedTools) != 1 || !snapshot.NestedTools[0].StreamingArguments {
			t.Fatal("typed nested snapshot missing", snapshot.NestedTools)
		}
		requireToolUTF16(t, snapshot.NestedTools[0].Arguments[key], 0xd801)
		snapshot.NestedTools[0].Arguments[key] = "snapshot mutation"
		fresh, err := stream.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		requireToolUTF16(t, fresh.NestedTools[0].Arguments[key], 0xd801)
		stream.Stop()
		state, err := first.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range state.Tasks {
			if task.Kind == nestedToolKind {
				old := Task{}
				events, err := nestedLifecycleEvents(state, old, task, state.Seq, first.session.limits)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, event := range events {
					if event.Type == "tool_execution_start" {
						found = true
						requireToolUTF16(t, event.Args[key], 0xd801)
						event.Args[key] = "reader mutation"
					}
				}
				if !found {
					t.Fatal("nested start event missing")
				}
			}
		}
		if err = first.Close(bg); err != nil {
			t.Fatal(err)
		}
		second := openHarness(t, utf16ReopenStore(t, b.store), Options{Registry: registry})
		result := waitPublicTask(t, second, id)
		if result.State.Outcome.Status != "completed" || prepares.Load() != 1 || before.Load() != 1 || effects.Load() != 2 || after.Load() != 1 {
			t.Fatal("nested replay repeated hooks or lost arguments", result.State.Outcome, prepares.Load(), before.Load(), effects.Load(), after.Load())
		}
	})
}

func TestNestedUTF16MainOwnershipAndIntentFences(t *testing.T) {
	scalar := toolUTF16Scalar(t, `"\ud800"`)
	args := JSON{"text": scalar, "n": json.Number("9007199254740993")}
	input := nestedToolInput{StreamingArguments: true, Arguments: args}
	object, err := dtoObject(input, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	var next nestedToolInput
	if err = fromObject(object, &next, DefaultLimits()); err != nil || !reflect.DeepEqual(next.Arguments, args) {
		t.Fatal("nested input roundtrip", err)
	}
	cp, err := nestedArgumentCheckpoint(args, true, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	owned, err := nestedCheckpointArguments(cp, next, DefaultLimits())
	if err != nil || !reflect.DeepEqual(owned, args) {
		t.Fatal("intent roundtrip", err)
	}
	for _, bad := range []JSON{{"argumentState": map[string]any{"streamingArguments": true, "arguments": nil}}, {"started": true}, {"started": true, "arguments": JSON{}}, {"argumentState": "bad"}, {"argumentState": cp["argumentState"], "arguments": JSON{}}} {
		if _, err = nestedCheckpointArguments(bad, next, DefaultLimits()); err == nil {
			t.Fatal("malformed typed intent accepted", bad)
		}
	}
	next.StreamingArguments = false
	if _, err = nestedCheckpointArguments(cp, next, DefaultLimits()); err == nil {
		t.Fatal("ordinary call promoted through argument metadata")
	}
	if _, err = copyScopedToolArguments(args, false, DefaultLimits()); err == nil {
		t.Fatal("ordinary nested JSON widened")
	}
}
