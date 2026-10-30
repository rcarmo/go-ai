package durable

import (
	"context"
	"encoding/json"
	"reflect"
	"sync/atomic"
	"testing"
)

func TestCodemodeUTF16MainQuickJSExecutionReopen(t *testing.T) {
	for _, name := range []string{"serial", "parallel", "remote"} {
		parallel := name != "serial"
		t.Run(name, func(t *testing.T) {
			utf16Backends(t, func(t *testing.T, b backend) {
				high := toolUTF16Scalar(t, `"\ud800"`)
				key := high.WTF8()
				var effects atomic.Int64
				entered := make(chan struct{})
				registry := NewRegistry()
				reg := wrapRegistration("unicode_leaf")
				reg.ReplaySafe = true
				reg.Definition.Parameters = json.RawMessage(`{"type":"object"}`)
				reg.Execute = func(ctx context.Context, args JSON, _ *ToolAPI) (ToolResult, error) {
					requireToolUTF16(t, args[key], 0xdfff)
					args[key] = "host mutation"
					if effects.Add(1) == 1 {
						close(entered)
						<-ctx.Done()
						return ToolResult{}, ctx.Err()
					}
					return ToolResult{Content: "done"}, nil
				}
				if err := registry.Register(reg); err != nil {
					t.Fatal(err)
				}
				sandbox, err := NewQuickJSSandbox(bg)
				if err != nil {
					t.Fatal(err)
				}
				defer sandbox.Close(bg)
				sandbox.AcknowledgeDrains = true
				sandbox.ParallelCalls = parallel
				var runner CodemodeSandbox = sandbox
				if name == "remote" {
					runner = &RemoteCodemodeSandbox{Remote: pipeRemote120{}, Identity: sandbox.RuntimeIdentity()}
				}
				code := `return await tools.unicode_leaf({['\ud800']:'\udfff'});`
				definition := taskDefinition(t, "codemode-unicode-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					for i := 0; i < 2; i++ {
						result, err := r.RunCodemode(ctx, "unicode", code, runner)
						if err != nil {
							return err
						}
						if result.Error != "" || !result.HasValue || result.Value != "done" {
							t.Error("VM result changed", result)
						}
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
				state, err := first.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, doc := range state.Documents {
					if doc.Kind == codemodeJournalKind {
						var journal codemodeJournalState
						if err = fromObject(doc.Value, &journal, first.session.limits); err != nil {
							t.Fatal(err)
						}
						if len(journal.Issued) == 1 {
							found = true
							if !journal.Issued[0].StreamingArguments {
								t.Error("journal lost encoding mode")
							}
							requireToolUTF16(t, journal.Issued[0].Arguments[key], 0xdfff)
						}
					}
				}
				if !found {
					t.Fatal("issued UTF-16 call not journaled")
				}
				if err = first.Close(bg); err != nil {
					t.Fatal(err)
				}
				second := openHarness(t, utf16ReopenStore(t, b.store), Options{Registry: registry})
				result := waitPublicTask(t, second, id)
				if result.State.Outcome.Status != "completed" || effects.Load() != 2 {
					t.Fatal("codemode replay lost arguments/repeated effect", result.State.Outcome, effects.Load())
				}
			})
		})
	}
}

func TestCodemodeUTF16MainProjectionAndFences(t *testing.T) {
	args, err := decodeQuickJSToolArguments([]byte(`{"\ud800":"\udfff","n":9007199254740993}`))
	if err != nil {
		t.Fatal(err)
	}
	call := CodemodeCall{StreamingArguments: true, ID: 1, Name: "leaf", Arguments: args}
	object, err := dtoObject(call, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = copyObject(object, DefaultLimits()); err != nil {
		t.Fatal("wire call not generic JSON", err)
	}
	var next CodemodeCall
	if err = fromObject(object, &next, DefaultLimits()); err != nil || !reflect.DeepEqual(next.Arguments, args) {
		t.Fatal("call projection", err)
	}
	ordinary := call
	ordinary.StreamingArguments = false
	if _, err = ownCodemodeArguments(ordinary, DefaultLimits()); err == nil {
		t.Fatal("untyped sandbox acquired UTF-16 permission")
	}
	call.Global = true
	if _, err = ownCodemodeArguments(call, DefaultLimits()); err == nil {
		t.Fatal("tool mode promoted global arguments")
	}
	for _, raw := range []string{`null`, `[]`, `{"x":1} trailing`, `{"x":"\ud800","bad":NaN}`} {
		if _, err = decodeQuickJSToolArguments([]byte(raw)); err == nil {
			t.Fatal("invalid arguments accepted", raw)
		}
	}
	old := CodemodeCall{Arguments: JSON{"x": "ordinary"}}
	fresh := old
	fresh.StreamingArguments = true
	if !codemodeArgumentModeCompatible(old, fresh) {
		t.Fatal("old UTF-8 journal mode cannot replay")
	}
	fresh.Arguments = args
	if codemodeArgumentModeCompatible(old, fresh) {
		t.Fatal("exceptional mode transition accepted")
	}
}

func TestCodemodeUTF16MainStandardCodecAndOuterBudget(t *testing.T) {
	args, err := decodeQuickJSToolArguments([]byte(`{"x":{"y":"\ud800"}}`))
	if err != nil {
		t.Fatal(err)
	}
	call := CodemodeCall{ID: 1, Name: "leaf", StreamingArguments: true, Arguments: args}
	raw, err := json.Marshal(call)
	if err != nil {
		t.Fatal(err)
	}
	var decoded CodemodeCall
	if err = json.Unmarshal(raw, &decoded); err != nil || !reflect.DeepEqual(decoded.Arguments, args) {
		t.Fatal("native Go wire codec lost arguments", err)
	}
	limits := DefaultLimits()
	limits.MaxDepth = 2
	var capped CodemodeCall
	if err = decodeStrict(raw, limits, limits.MaxDocumentBytes, &capped); err == nil {
		t.Fatal("custom decoder bypassed caller depth budget")
	}
	decoded.Arguments = JSON{"x": "tampered"}
	if err = restoreArgumentDTO(reflect.ValueOf(&decoded).Elem(), DefaultLimits()); err == nil {
		t.Fatal("decoded witness changed logical arguments")
	}
}
