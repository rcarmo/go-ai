package durable

import (
	"context"
	"encoding/json"
	"math"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestMainToolPresentationLegacyAndOwnerFence(t *testing.T) {
	makeState := func() (Snapshot, Task, toolCheckpoint) {
		task := Task{ID: 7, Owner: 3, Conversation: 4, Kind: "pi.tool"}
		cp := toolCheckpoint{CallID: "c1", Offer: toolOffer{Name: "work"}, Output: "legacy"}
		live := Document{ID: 9, Kind: "pi.live", Scope: "conversation", Owner: 4, Value: JSON{"run": JSON{"task": 3}, "tools": []any{JSON{"taskId": 7, "callId": "c1", "status": "running", "output": "stored", "details": nil, "droppedBytes": 5}}}}
		return Snapshot{Documents: map[ID]Document{9: live}}, task, cp
	}
	state, task, cp := makeState()
	got, err := toolPresentation(state, task, cp, DefaultLimits())
	if err != nil || got.Output != "stored" || !got.HasDetails || got.Details != nil || got.DroppedBytes != 5 || got.CallID != "c1" || got.Offer.Name != "work" {
		t.Fatal(got, err)
	}
	if cp.Output != "legacy" {
		t.Fatal("checkpoint mutated")
	}
	for _, mode := range []string{"missing", "task", "call", "owner", "generation", "retired", "scope", "huge-task", "huge-generation", "done"} {
		t.Run(mode, func(t *testing.T) {
			state, task, cp := makeState()
			doc := state.Documents[9]
			slot := generationObject(doc.Value["tools"].([]any)[0])
			switch mode {
			case "missing":
				delete(slot, "taskId")
			case "task":
				slot["taskId"] = 8
			case "call":
				slot["callId"] = "other"
			case "owner":
				doc.Owner = 5
			case "generation":
				doc.Value["run"] = JSON{"task": 8}
			case "retired":
				doc.Retired = true
			case "scope":
				doc.Scope = "other"
			case "huge-task":
				slot["taskId"] = json.Number("18446744073709551623")
			case "huge-generation":
				doc.Value["run"] = JSON{"task": json.Number("18446744073709551619")}
			case "done":
				slot["status"] = "done"
			}
			state.Documents[9] = doc
			got, err := toolPresentation(state, task, cp, DefaultLimits())
			if err != nil || got.Output != "legacy" {
				t.Fatal("foreign presentation adopted", got, err)
			}
		})
	}
}

// An unrelated tool's immediate completion rebuilds pi.live.tools. It must not
// replace its sibling's held memory progress with an empty durable checkpoint.
func TestMainToolProgressSiblingCompletionKeepsHeldOutput(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if calls.Add(1) == 1 {
				a := toolAnswer("a", "a", JSON{})
				a.Message.Content = append(a.Message.Content, toolAnswer("b", "b", JSON{}).Message.Content...)
				ch <- a
			} else {
				ch <- terminal("done")
			}
			close(ch)
			return ch
		})
		type namedRun struct {
			name string
			run  *mainToolRun
		}
		runs := make(chan namedRun, 2)
		registry := NewRegistry()
		for _, name := range []string{"a", "b"} {
			if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: name, Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "test.sibling." + name, Version: 1, ReplaySafe: true, Execute: func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
				run := &mainToolRun{output: make(chan string, 1), finish: make(chan struct{})}
				runs <- namedRun{name, run}
				for {
					select {
					case <-ctx.Done():
						return ToolResult{}, ctx.Err()
					case text := <-run.output:
						if err := api.Output(text); err != nil {
							return ToolResult{}, err
						}
					case <-run.finish:
						return ToolResult{}, nil
					}
				}
			}}); err != nil {
				t.Fatal(err)
			}
		}
		options.Registry = registry
		clock := new(atomic.Int64)
		clock.Store(1000)
		f := mainOpenRecovery(t, b.store, ref, options, clock, math.Inf(1))
		sub, err := f.c.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		active := map[string]*mainToolRun{}
		for len(active) < 2 {
			r := watchReceive(t, runs)
			active[r.name] = r.run
		}
		output := func(live JSON, call string) (string, string) {
			slots, _ := live["tools"].([]any)
			for _, raw := range slots {
				slot := generationObject(raw)
				if slot["callId"] == call {
					text, _ := slot["output"].(string)
					status, _ := slot["status"].(string)
					return text, status
				}
			}
			return "", ""
		}
		for _, name := range []string{"a", "b"} {
			mainRecoveryAdvance(f)
			active[name].output <- name
			for {
				frame := watchReceive(t, f.frames)
				if text, _ := output(frame.value.Docs["pi.live"], name); text == name {
					break
				}
			}
		}
		close(active["a"].finish)
		for {
			frame := watchReceive(t, f.frames)
			if _, status := output(frame.value.Docs["pi.live"], "a"); status == "done" {
				if text, _ := output(frame.value.Docs["pi.live"], "b"); text != "b" {
					t.Error("sibling completion erased held output", text)
				}
				break
			}
		}
		if text, _ := output(f.live(f.stored()), "b"); text != "b" {
			t.Error("sibling completion did not preserve stored output", text)
		}
		close(active["b"].finish)
		mainFinishToolSubmission(t, f, sub.ID())
		if err := f.h.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}
