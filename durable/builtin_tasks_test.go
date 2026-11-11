package durable

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	goai "github.com/rcarmo/go-ai"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

// Independent source-derived control paired with the real Harness probe in
// notes/oracles/session-admission.upstream.test.ts. It failed six assertions
// at 1eafca1 (version/state, early listener_error and lost tool publications).
func TestMainBuiltinCanonicalPublicationsV150(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "tool-c1", Description: "tool-c1 tool", Parameters: []byte(`{"type":"object","properties":{}}`)}, Implementation: "oracle.c1", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) { return ToolResult{}, nil }}); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			ch <- toolAnswer("c1", "tool-c1", JSON{})
		} else {
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	h := openHarness(t, store, options)
	c := root(t, h, ref)
	var mu sync.Mutex
	var changes []ReadonlyValue
	var ends []WatchEnd
	guard, err := h.ObserveCommits(CommitObserver{Advance: func(_ context.Context, p CommitPublication) error {
		mu.Lock()
		defer mu.Unlock()
		for i := range p.Changes.Len() {
			change, _ := p.Changes.Index(i)
			changes = append(changes, change)
		}
		return nil
	}, End: func(end WatchEnd) error { mu.Lock(); defer mu.Unlock(); ends = append(ends, end); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Detach()
	sub, err := c.Submit(bg, Input{Type: "input", Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if result := waitSubmission(t, sub); result.Submission.Status != "done" {
		t.Fatal(result)
	}
	mu.Lock()
	beforeClose := append([]WatchEnd(nil), ends...)
	mu.Unlock()
	if len(beforeClose) != 0 {
		t.Errorf("stored observer ended before close: %+v", beforeClose)
	}
	if err := h.Close(bg); err != nil {
		t.Fatal(err)
	}
	var generation, work, request ReadonlyValue
	for _, change := range changes {
		kind := immutableViewMember(t, change, "type").String()
		if kind != "task" && kind != "entry" {
			continue
		}
		value := immutableViewMember(t, change, "value")
		switch immutableViewMember(t, value, "kind").String() {
		case "pi.generation":
			if generation == nil {
				generation = value
			}
		case "pi.tool":
			if work == nil {
				work = value
			}
		case "pi.assistant":
			if request == nil {
				request = value
			}
		}
	}
	if generation == nil {
		t.Error("missing generation publication")
	} else {
		if got := immutableViewMember(t, generation, "version").Number().String(); got != "2" {
			t.Errorf("generation version=%s, want source2", got)
		}
		if value := immutableViewMember(t, generation, "input"); !equalJSONValue(value.Copy(), JSON{}) {
			t.Error("generation input differs from source empty object")
		}
		if state := immutableViewMember(t, generation, "state"); !equalJSONValue(state.Copy(), JSON{"status": "pending", "checkpoint": JSON{"phase": "prepare", "attempt": 1}}) {
			t.Errorf("generation initial state differs from source pending/prepare/attempt1: %v", state.Copy())
		}
	}
	if request == nil {
		t.Error("missing assistant tool-call publication")
	}
	if work == nil {
		t.Error("missing model-tool publication")
	} else {
		if got := immutableViewMember(t, work, "version").Number().String(); got != "3" {
			t.Errorf("tool version=%s, want source3", got)
		}
		if request != nil {
			expected := JSON{"kind": "model", "assistant": immutableViewMember(t, request, "id").Number(), "callId": "c1"}
			if input := immutableViewMember(t, work, "input"); !equalJSONValue(input.Copy(), expected) {
				t.Error("model-tool input differs from source")
			}
		}
		if state := immutableViewMember(t, work, "state"); !equalJSONValue(state.Copy(), JSON{"status": "pending", "checkpoint": JSON{"phase": "call"}}) {
			t.Error("tool initial state differs from source")
		}
	}
	if len(ends) != 1 || ends[0].Reason != "failed" {
		t.Errorf("source close ending differs: %+v", ends)
	}
}

// The expected sequences were captured from pinned v1.5.0 Harness
// observeCommits for the same two-call scenario (parallel and sequential), with
// IDs bound to the corresponding records. Consecutive identical states are one
// state: native commit granularity is a separate open contract.
func TestBuiltinTaskPublicProgressionMatchesSourceV150(t *testing.T) {
	for _, mode := range []string{"parallel", "sequential"} {
		t.Run(mode, func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			registry := NewRegistry()
			for _, name := range []string{"tool-c1", "tool-c2"} {
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: name, Description: name + " tool", Parameters: []byte(`{"type":"object","properties":{}}`)}, Implementation: "oracle." + name, Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) { return ToolResult{}, nil }}); err != nil {
					t.Fatal(err)
				}
			}
			var requests atomic.Int64
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				if requests.Add(1) == 1 {
					answer := toolAnswer("c1", "tool-c1", JSON{})
					answer.Message.Content = append(answer.Message.Content, goai.ContentBlock{Type: "toolCall", ID: "c2", Name: "tool-c2", Arguments: map[string]any{}})
					ch <- answer
				} else {
					ch <- terminal("done")
				}
				close(ch)
				return ch
			})
			options.Registry = registry
			h := openHarness(t, store, options)
			c, err := h.Root(bg, AgentChange{Model: ref, Settings: RequestSettings{ToolExecution: mode}})
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			states := map[uint64][]string{}
			kinds := map[uint64]string{}
			var order []uint64
			entries := map[string][]uint64{}
			var ends []WatchEnd
			guard, err := h.ObserveCommits(CommitObserver{Advance: func(_ context.Context, p CommitPublication) error {
				mu.Lock()
				defer mu.Unlock()
				for i := range p.Changes.Len() {
					change, _ := p.Changes.Index(i)
					value := immutableViewMember(t, change, "value")
					switch immutableViewMember(t, change, "type").String() {
					case "entry":
						id, _ := json.Number(immutableViewMember(t, value, "id").Number()).Int64()
						kind := immutableViewMember(t, value, "kind").String()
						entries[kind] = append(entries[kind], uint64(id))
					case "task":
						record := value.Copy().(map[string]any)
						id, _ := record["id"].(json.Number).Int64()
						kind := record["kind"].(string)
						if kind != "pi.generation" && kind != "pi.tool" {
							continue
						}
						if _, seen := kinds[uint64(id)]; !seen {
							order = append(order, uint64(id))
						}
						kinds[uint64(id)] = kind
						encoded, err := json.Marshal(JSON{"version": record["version"], "input": record["input"], "state": record["state"]})
						if err != nil {
							return err
						}
						list := states[uint64(id)]
						if len(list) == 0 || list[len(list)-1] != string(encoded) {
							states[uint64(id)] = append(list, string(encoded))
						}
					}
				}
				return nil
			}, End: func(end WatchEnd) error { mu.Lock(); ends = append(ends, end); mu.Unlock(); return nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Detach()
			sub, err := c.Submit(bg, Input{Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			if result := waitSubmission(t, sub); result.Submission.Status != "done" {
				t.Fatal(result)
			}
			if err := h.Close(bg); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(ends) != 1 || ends[0].Reason != "failed" {
				t.Fatalf("observer ending %+v", ends)
			}
			var generations, tools []uint64
			for _, id := range order {
				if kinds[id] == "pi.generation" {
					generations = append(generations, id)
				} else {
					tools = append(tools, id)
				}
			}
			if len(generations) != 2 || len(tools) != 2 || len(entries["pi.system"]) != 1 || len(entries["pi.assistant"]) != 2 || len(entries["pi.tool-result"]) != 2 {
				t.Fatalf("records generations=%v tools=%v entries=%v", generations, tools, entries)
			}
			system, assistant, final := entries["pi.system"][0], entries["pi.assistant"][0], entries["pi.assistant"][1]
			results := entries["pi.tool-result"]
			model := fmt.Sprintf(`{"modelId":%q,"provider":%q}`, ref.ID, ref.Provider)
			prepare := func(status string) string {
				return fmt.Sprintf(`{"input":{},"state":{"checkpoint":{"attempt":1,"phase":"prepare"},"status":%q},"version":2}`, status)
			}
			request := func(cutoff uint64) string {
				return fmt.Sprintf(`{"input":{},"state":{"checkpoint":{"attempt":1,"cutoff":%d,"model":%s,"phase":"request","streamOptions":{},"thinkingLevel":"off"},"status":"running"},"version":2}`, cutoff, model)
			}
			done := func(entry uint64) string {
				return fmt.Sprintf(`{"input":{},"state":{"outcome":{"result":{"entryId":%d},"status":"completed"},"status":"terminal"},"version":2}`, entry)
			}
			round := func(status string, ids []uint64, pending []string, on []uint64) string {
				idsJSON, _ := json.Marshal(ids)
				pendingJSON, _ := json.Marshal(pending)
				state := fmt.Sprintf(`"checkpoint":{"assistant":%d,"pending":%s,"phase":"tools","tools":%s}`, assistant, pendingJSON, idsJSON)
				if on != nil {
					onJSON, _ := json.Marshal(on)
					state += fmt.Sprintf(`,"on":%s,"policy":"allSettled"`, onJSON)
				}
				state += fmt.Sprintf(`,"status":%q`, status)
				return `{"input":{},"state":{` + state + `},"version":2}`
			}
			var first []string
			if mode == "parallel" {
				first = []string{prepare("pending"), prepare("running"), request(system), round("waiting", tools, []string{}, tools), round("running", tools, []string{}, nil), done(assistant)}
			} else {
				first = []string{prepare("pending"), prepare("running"), request(system), round("waiting", tools[:1], []string{"c2"}, tools[:1]), round("running", tools[:1], []string{"c2"}, nil), round("waiting", tools, []string{}, tools[1:]), round("running", tools, []string{}, nil), done(assistant)}
			}
			want := map[uint64][]string{generations[0]: first, generations[1]: {prepare("pending"), prepare("running"), request(results[1]), done(final)}}
			for i, id := range tools {
				input := fmt.Sprintf(`{"assistant":%d,"callId":"c%d","kind":"model"}`, assistant, i+1)
				want[id] = []string{
					`{"input":` + input + `,"state":{"checkpoint":{"phase":"call"},"status":"pending"},"version":3}`,
					`{"input":` + input + `,"state":{"checkpoint":{"phase":"call"},"status":"running"},"version":3}`,
					`{"input":` + input + `,"state":{"checkpoint":{"arguments":{},"phase":"execute","replay":"unsafe"},"status":"running"},"version":3}`,
					fmt.Sprintf(`{"input":%s,"state":{"outcome":{"result":{"entryId":%d,"kind":"model"},"status":"completed"},"status":"terminal"},"version":3}`, input, results[i]),
				}
			}
			for id, expected := range want {
				if !reflect.DeepEqual(states[id], expected) {
					t.Errorf("%s task %d progression\n got: %v\nwant: %v", kinds[id], id, states[id], expected)
				}
			}
		})
	}
}

// Native representation control: every readonly subtree of source arguments
// retains its JavaScript string units when encoded or independently owned.
func TestBuiltinToolUTF16ReadonlySubtrees(t *testing.T) {
	scalar := toolUTF16Scalar(t, `"\ud800"`)
	key := scalar.WTF8()
	arguments, err := NewImmutableValue(sourceToolArguments{"nested": JSON{key: scalar}, "array": []any{scalar}}, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	root := readonlyValue(arguments)
	nested := immutableViewMember(t, root, "nested")
	leaf := immutableViewMember(t, nested, key)
	array := immutableViewMember(t, root, "array")
	for _, value := range []ReadonlyValue{root, nested, leaf, array} {
		clone, err := NewImmutableValue(value, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		wire, err := clone.MarshalJSON()
		if err != nil || !bytes.Contains(wire, []byte(`\ud800`)) {
			t.Fatalf("subtree encoding %s: %v", wire, err)
		}
	}
	for _, value := range []any{scalar, JSON{key: scalar}, []any{scalar}} {
		if _, err := NewImmutableValue(value, DefaultLimits()); err == nil {
			t.Fatal("untyped value gained UTF-16 authority")
		}
	}
}
