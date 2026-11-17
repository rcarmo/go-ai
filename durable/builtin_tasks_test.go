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
	"time"
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
	if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "tool-c1", Description: "tool-c1 tool", Parameters: []byte(`{"type":"object","properties":{}}`)}, Implementation: "oracle.c1", Version: 1, Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) { return ToolResult{}, nil }}); err != nil {
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
// observeCommits for a two-call scenario (parallel and sequential). Result
// completion is gated by an observed stored result in both reference/native
// fixtures, so publication order binds IDs without a goroutine race. The
// reverse parallel case preserves the same per-task state assertions.
// Consecutive identical states are one
// state: native commit granularity is a separate open contract.
func TestBuiltinTaskPublicProgressionMatchesSourceV150(t *testing.T) {
	for _, scenario := range []struct{ name, execution, firstCall string }{{"parallel", "parallel", "c1"}, {"sequential", "sequential", "c1"}, {"parallel-reverse", "parallel", "c2"}} {
		t.Run(scenario.name, func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			registry := NewRegistry()
			firstStored := make(chan struct{})
			var firstOnce sync.Once
			for _, name := range []string{"tool-c1", "tool-c2"} {
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: name, Description: name + " tool", Parameters: []byte(`{"type":"object","properties":{}}`)}, Implementation: "oracle." + name, Version: 1, Execute: func(ctx context.Context, _ any, _ *ToolAPI) (ToolResult, error) {
					if name != "tool-"+scenario.firstCall {
						select {
						case <-firstStored:
						case <-ctx.Done():
							return ToolResult{}, ctx.Err()
						}
					}
					return ToolResult{}, nil
				}}); err != nil {
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
			options.Settings = &HarnessSettings{ToolExecution: scenario.execution}
			h := openHarness(t, store, options)
			c, err := h.Root(bg, AgentChange{Model: ref})
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
						if kind == "pi.tool-result" {
							model := immutableViewMember(t, value, "model")
							message, _ := model.Index(0)
							if immutableViewMember(t, message, "toolCallId").String() == scenario.firstCall {
								firstOnce.Do(func() { close(firstStored) })
							}
						}
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
			if scenario.execution == "parallel" {
				first = []string{prepare("pending"), prepare("running"), request(system), round("waiting", tools, []string{}, tools), round("running", tools, []string{}, nil), done(assistant)}
			} else {
				first = []string{prepare("pending"), prepare("running"), request(system), round("waiting", tools[:1], []string{"c2"}, tools[:1]), round("running", tools[:1], []string{"c2"}, nil), round("waiting", tools, []string{}, tools[1:]), round("running", tools, []string{}, nil), done(assistant)}
			}
			want := map[uint64][]string{generations[0]: first, generations[1]: {prepare("pending"), prepare("running"), request(results[1]), done(final)}}
			for i, id := range tools {
				resultIndex := i
				if scenario.firstCall == "c2" {
					resultIndex = 1 - i
				}
				input := fmt.Sprintf(`{"assistant":%d,"callId":"c%d","kind":"model"}`, assistant, i+1)
				want[id] = []string{
					`{"input":` + input + `,"state":{"checkpoint":{"phase":"call"},"status":"pending"},"version":3}`,
					`{"input":` + input + `,"state":{"checkpoint":{"phase":"call"},"status":"running"},"version":3}`,
					`{"input":` + input + `,"state":{"checkpoint":{"arguments":{},"phase":"execute","replay":"unsafe"},"status":"running"},"version":3}`,
					fmt.Sprintf(`{"input":%s,"state":{"outcome":{"result":{"entryId":%d,"kind":"model"},"status":"completed"},"status":"terminal"},"version":3}`, input, results[resultIndex]),
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
	arguments, err := NewImmutableValue(sourceToolArguments{JSON{"nested": JSON{key: scalar}, "array": []any{scalar}}}, DefaultLimits())
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

// Source generation.ts162-175/tool.ts141-158 and unchanged original
// harness-nested-tools.test.ts1230-1250. These are definitions used by Harness,
// not publication projections of private builtin execution.
func TestSourceCanonicalBuiltinDefinitionsAndMigrations(t *testing.T) {
	registry := NewRegistry()
	snapshot, err := registry.taskSnapshot(DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"pi.generation", "pi.tool"} {
		t.Run(kind, func(t *testing.T) {
			definition := snapshot.Task(kind)
			if definition == nil {
				t.Fatal("source executable builtin definition missing", kind)
			}
			version, phase := uint64(2), "prepare"
			want := JSON{"phase": phase, "attempt": 1}
			if kind == "pi.tool" {
				version, phase = 3, "call"
				want = JSON{"phase": phase}
			}
			if definition.Version() != version {
				t.Fatal(definition.Version(), version)
			}
			execution, err := definition.initial(JSON{}, DefaultLimits())
			if err != nil || execution.State.Status != "pending" || !equalJSONValue(execution.State.Checkpoint, want) {
				t.Fatal(execution, err)
			}
			if definition.options.Migrate == nil {
				t.Fatal("source builtin migration missing")
			}
			checkpoint := JSON{"phase": "call"}
			if kind == "pi.generation" {
				input := JSON{}
				cp := JSON{"phase": "prepare", "attempt": 3}
				nextInput, nextCP, err := definition.options.Migrate(input, cp, 1)
				if err != nil || !equalJSONValue(nextInput, input) || !equalJSONValue(nextCP, cp) {
					t.Fatal(nextInput, nextCP, err)
				}
				return
			}
			old := JSON{"assistant": 3, "callId": "c1"}
			input, cp, err := definition.options.Migrate(old, checkpoint, 1)
			if err != nil || !equalJSONValue(input, JSON{"kind": "model", "assistant": 3, "callId": "c1"}) || !equalJSONValue(cp, checkpoint) {
				t.Fatal(input, cp, err)
			}
			for _, value := range []JSON{{"kind": "model", "assistant": 3, "callId": "c1"}, {"kind": "nested", "parent": 7, "parentCallId": "c1", "key": "k", "call": JSON{"type": "toolCall", "id": "c1/k", "name": "echo", "arguments": JSON{}}}} {
				input, cp, err := definition.options.Migrate(value, checkpoint, 2)
				if err != nil || !equalJSONValue(input, value) || !equalJSONValue(cp, checkpoint) {
					t.Fatal(input, cp, err)
				}
			}
		})
	}
}

// Source-derived imported-record execution: no private input/checkpoint is
// available to reinterpret. Generation v1 migrates unchanged, then prepare
// executes failNoModel through the actual generic runtime and persists v2.
func TestSourceCanonicalBuiltinImportedGenerationNoModel(t *testing.T) {
	for _, version := range []uint64{1, 2} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			record := TaskRecord{ID: 2, Conversation: 1, Kind: "pi.generation", Version: version, Input: &TaskValue{Present: true, Value: JSON{}}, State: TaskState{Status: "pending", Checkpoint: JSON{"phase": "prepare", "attempt": 1}}}
			result, err := store.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: 1}}}, {{Type: "task", Value: record}}}, nil)
			if err != nil || result.Refused != nil {
				t.Fatal(result, err)
			}
			h := openHarness(t, store, Options{})
			_ = root(t, h, ModelRef{})
			if err := h.Resume(bg); err != nil {
				t.Fatal(err)
			}
			deadline, cancel := context.WithTimeout(bg, 3*time.Second)
			defer cancel()
			if err := h.WaitForIdle(deadline); err != nil {
				t.Fatal("canonical imported generation did not finish", err)
			}
			task, found, err := h.Task(bg, 2)
			if err != nil || !found || task.Version != 2 || task.Input == nil || !equalJSONValue(task.Input.Value, JSON{}) {
				t.Fatal(task, found, err)
			}
			wire, err := task.State.MarshalJSON()
			want := []byte(`{"status":"terminal","outcome":{"status":"failed","error":{"message":"No model is configured","detail":{"reason":"no_model"}}}}`)
			if err != nil || !reflect.DeepEqual(dropinJSON(t, wire), dropinJSON(t, want)) {
				t.Fatal("canonical imported builtin did not execute source prepare", string(wire), err)
			}
		})
	}
}

// Source tool.ts136-239: canonical input, durable execute intent and migration
// execute through the generic runtime, without a native tool checkpoint.
func TestSourceCanonicalToolModelExecutionAndMigration(t *testing.T) {
	for _, version := range []uint64{1, 2, 3} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			registry := NewRegistry()
			var calls atomic.Int64
			if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "echo", Parameters: []byte(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)}, Implementation: "canonical.echo", Version: 1, Execute: func(_ context.Context, rawToolArguments any, _ *ToolAPI) (ToolResult, error) {
				args := rawToolArguments.(JSON)
				calls.Add(1)
				return ToolResult{Content: args["text"].(string)}, nil
			}}); err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, store, Options{Registry: registry})
			c := root(t, h, ModelRef{})
			snapshot, err := registry.taskSnapshot(DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			definition := snapshot.Task("pi.tool")
			if definition == nil {
				t.Fatal("canonical tool definition missing")
			}
			old := *definition
			old.options.Version = version
			var id ID
			_, err = c.Commit(bg, func(tx *Tx) (any, error) {
				event := toolAnswer("c1", "echo", JSON{"text": "owned"})
				receipt, err := contributionReceipt(*event.Message, tx.limits)
				if err != nil {
					return nil, err
				}
				wire, err := dtoObject(receipt, tx.limits)
				if err != nil {
					return nil, err
				}
				entry, err := tx.AppendEntry(c.ID(), JSON{"kind": "pi.assistant", "model": []any{wire}})
				if err != nil {
					return nil, err
				}
				input := JSON{"assistant": entry.ID, "callId": "c1"}
				if version != 1 {
					input["kind"] = "model"
				}
				id, err = tx.CreateTask(&old, input, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				return nil, err
			})
			if err != nil {
				t.Fatal(err)
			}
			record := waitPublicTask(t, h, id)
			if record.Version != 3 || calls.Load() != 1 || record.State.Outcome == nil || record.State.Outcome.Status != "completed" || record.State.Outcome.Result == nil {
				t.Fatal(record, calls.Load())
			}
			result := generationObject(record.State.Outcome.Result.Value)
			if result["kind"] != "model" || submissionEntryID(result["entryId"]) == 0 {
				t.Fatal(result)
			}
			entry, found, err := h.Entry(bg, submissionEntryID(result["entryId"]))
			if err != nil || !found || entry.Kind != "pi.tool-result" || len(entry.Model) != 1 || goai.ContentText(entry.Model[0].Content) != "owned" {
				t.Fatal(entry, found, err)
			}
		})
	}
}

func TestSourceCanonicalGenerationModelAndToolExecution(t *testing.T) {
	for _, tools := range []bool{false, true} {
		t.Run(fmt.Sprint(tools), func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			registry := NewRegistry()
			if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "echo", Parameters: []byte(`{"type":"object","properties":{}}`)}, Implementation: "source.echo", Version: 1, Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) { return ToolResult{Content: "echoed"}, nil }}); err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int64
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				if requests.Add(1) == 1 && tools {
					ch <- toolAnswer("c1", "echo", JSON{})
				} else {
					ch <- terminal("answer")
				}
				close(ch)
				return ch
			})
			options.Registry = registry
			h := openHarness(t, store, options)
			c := root(t, h, ref)
			snapshot, err := registry.taskSnapshot(DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			definition := snapshot.Task("pi.generation")
			if definition == nil {
				t.Fatal("canonical generation missing")
			}
			var id ID
			_, err = c.Commit(bg, func(tx *Tx) (any, error) {
				if _, err := tx.AppendEntry(c.ID(), JSON{"kind": "pi.user", "model": []any{JSON{"role": "user", "content": "go", "timestamp": 42}}}); err != nil {
					return nil, err
				}
				var err error
				id, err = tx.CreateTask(definition, JSON{}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				return nil, err
			})
			if err != nil {
				t.Fatal(err)
			}
			record := waitPublicTask(t, h, id)
			if record.State.Outcome == nil || record.State.Outcome.Status != "completed" {
				data, _ := record.MarshalJSON()
				t.Fatal(string(data))
			}
			deadline, cancel := context.WithTimeout(bg, 3*time.Second)
			defer cancel()
			if err := h.WaitForIdle(deadline); err != nil {
				t.Fatal(err)
			}
			page, err := c.Entries(bg, EntryQuery{}, 100, nil)
			if err != nil {
				t.Fatal(err)
			}
			answer := false
			tool := false
			for _, entry := range page.Items {
				if len(entry.Model) == 1 {
					if entry.Kind == "pi.assistant" && goai.ContentText(entry.Model[0].Content) == "answer" {
						answer = true
					}
					if entry.Kind == "pi.tool-result" && goai.ContentText(entry.Model[0].Content) == "echoed" {
						tool = true
					}
				}
			}
			want := int64(1)
			if tools {
				want = 2
			}
			if !answer || tools && !tool || requests.Load() != want {
				t.Fatal(answer, tool, requests.Load(), page.Items)
			}
		})
	}
}

func TestSourceCanonicalToolThrownExecutionFails(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "thrower", Parameters: []byte(`{"type":"object","properties":{}}`)}, Implementation: "source.thrower", Version: 1, Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) {
		return ToolResult{}, fmt.Errorf("private failure")
	}}); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, Options{Registry: registry})
	c := root(t, h, ModelRef{})
	var id ID
	_, err = c.Commit(bg, func(tx *Tx) (any, error) {
		event := toolAnswer("throw-1", "thrower", JSON{})
		receipt, err := contributionReceipt(*event.Message, tx.limits)
		if err != nil {
			return nil, err
		}
		wire, err := dtoObject(receipt, tx.limits)
		if err != nil {
			return nil, err
		}
		entry, err := tx.AppendEntry(c.ID(), JSON{"kind": "pi.assistant", "model": []any{wire}})
		if err != nil {
			return nil, err
		}
		id, err = tx.CreateTask(ToolTask(), JSON{"kind": "model", "assistant": entry.ID, "callId": "throw-1"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		return nil, err
	})
	if err != nil {
		t.Fatal(err)
	}
	record := waitPublicTask(t, h, id)
	outcome := record.State.Outcome
	if outcome == nil || outcome.Status != "failed" || outcome.Error == nil || outcome.Error.Message != "Tool thrower threw" || outcome.Result == nil {
		wire, _ := record.MarshalJSON()
		t.Fatal(string(wire))
	}
}

func TestSourceCanonicalToolRecoveryReplayPolicy(t *testing.T) {
	for _, safe := range []bool{false, true} {
		t.Run(fmt.Sprint(safe), func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			registry := NewRegistry()
			var calls atomic.Int64
			if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "recover", Parameters: []byte(`{"type":"object","properties":{}}`)}, Implementation: "source.recover", Version: 1, ReplaySafe: safe, Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) {
				calls.Add(1)
				return ToolResult{Content: "replayed"}, nil
			}}); err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, store, Options{Registry: registry})
			c := root(t, h, ModelRef{})
			definition := *ToolTask()
			definition.options.Initial = func(any) (JSON, error) {
				replay := "unsafe"
				if safe {
					replay = "safe"
				}
				return JSON{"phase": "execute", "arguments": JSON{}, "replay": replay}, nil
			}
			var id ID
			_, err = c.Commit(bg, func(tx *Tx) (any, error) {
				event := toolAnswer("c1", "recover", JSON{})
				receipt, err := contributionReceipt(*event.Message, tx.limits)
				if err != nil {
					return nil, err
				}
				wire, err := dtoObject(receipt, tx.limits)
				if err != nil {
					return nil, err
				}
				entry, err := tx.AppendEntry(c.ID(), JSON{"kind": "pi.assistant", "model": []any{wire}})
				if err != nil {
					return nil, err
				}
				id, err = tx.CreateTask(&definition, JSON{"kind": "model", "assistant": entry.ID, "callId": "c1"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				return nil, err
			})
			if err != nil {
				t.Fatal(err)
			}
			record := waitPublicTask(t, h, id)
			want := "failed"
			count := int64(0)
			if safe {
				want = "completed"
				count = 1
			}
			if record.State.Outcome == nil || record.State.Outcome.Status != want || calls.Load() != count {
				wire, _ := record.MarshalJSON()
				t.Fatal(string(wire), calls.Load())
			}
		})
	}
}

func TestSourceCanonicalToolNestedUsesSameDefinition(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	var leaf atomic.Int64
	if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "leaf", Parameters: []byte(`{"type":"object","properties":{}}`)}, Implementation: "source.leaf", Version: 1, StructuredOutputSchema: JSON{"type": "object", "properties": JSON{"value": JSON{"type": "string"}}, "required": []any{"value"}}, Callers: []string{"tools"}, Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) {
		leaf.Add(1)
		return ToolResult{StructuredOutput: JSON{"value": "owned"}, HasStructuredOutput: true, Diagnostics: []ToolDiagnostic{{Severity: "invalid", Message: "strict JSON severity"}}}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "parent", Parameters: []byte(`{"type":"object","properties":{}}`)}, Implementation: "source.parent", Version: 1, Execute: func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
		value, err := api.CallTool(ctx, "stable", "leaf", JSON{})
		if err != nil {
			t.Error("canonical nested call", err)
			return ToolResult{}, err
		}
		if !value.HasStructuredOutput || !equalJSONValue(value.StructuredOutput, JSON{"value": "owned"}) || value.IsError || !reflect.DeepEqual(value.Diagnostics, []ToolDiagnostic{{Severity: "invalid", Message: "strict JSON severity"}}) {
			return ToolResult{}, fmt.Errorf("nested structured output or diagnostic lost")
		}
		return ToolResult{Content: "parent done"}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, Options{Registry: registry})
	c := root(t, h, ModelRef{})
	var id ID
	_, err = c.Commit(bg, func(tx *Tx) (any, error) {
		event := toolAnswer("p1", "parent", JSON{})
		message, err := contributionReceipt(*event.Message, tx.limits)
		if err != nil {
			return nil, err
		}
		wire, err := dtoObject(message, tx.limits)
		if err != nil {
			return nil, err
		}
		entry, err := tx.AppendEntry(c.ID(), JSON{"kind": "pi.assistant", "model": []any{wire}})
		if err != nil {
			return nil, err
		}
		id, err = tx.CreateTask(ToolTask(), JSON{"kind": "model", "assistant": entry.ID, "callId": "p1"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		return nil, err
	})
	if err != nil {
		t.Fatal(err)
	}
	record := waitPublicTask(t, h, id)
	if record.State.Outcome == nil || record.State.Outcome.Status != "completed" || leaf.Load() != 1 {
		wire, _ := record.MarshalJSON()
		t.Fatal(string(wire), leaf.Load())
	}
	page, err := h.session.ScanTasks(bg, TaskQuery{Conversation: c.ID()}, 10, nil)
	if err != nil {
		t.Fatal(page, err)
	}
	children := []TaskRecord{}
	for _, task := range page.Items {
		if task.Owner == id {
			children = append(children, task)
		}
	}
	if len(children) != 1 {
		t.Fatal(children)
	}
	child := children[0]
	if child.Kind != "pi.tool" || child.Version != 3 || generationObject(child.Input.Value)["kind"] != "nested" || !equalJSONValue(child.State.Outcome.Result.Value, JSON{"kind": "nested"}) {
		wire, _ := child.MarshalJSON()
		t.Fatal(string(wire))
	}
}

func TestSourceCanonicalToolNestedUnavailableSettles(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	var leaf atomic.Int64
	if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "parent", Parameters: []byte(`{"type":"object","properties":{}}`)}, Implementation: "source.parent", Version: 1, Execute: func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
		value, err := api.CallTool(ctx, "stable", "leaf", JSON{})
		if err != nil {
			t.Error("canonical nested call", err)
			return ToolResult{}, err
		}
		if !value.IsError || len(value.Diagnostics) == 0 || value.Diagnostics[0].Code != "tool_unavailable" {
			t.Error("unavailable result", value)
			return ToolResult{}, fmt.Errorf("nested unavailable result lost")
		}
		return ToolResult{Content: "parent done"}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, Options{Registry: registry})
	c := root(t, h, ModelRef{})
	var id ID
	_, err = c.Commit(bg, func(tx *Tx) (any, error) {
		event := toolAnswer("p1", "parent", JSON{})
		message, err := contributionReceipt(*event.Message, tx.limits)
		if err != nil {
			return nil, err
		}
		wire, err := dtoObject(message, tx.limits)
		if err != nil {
			return nil, err
		}
		entry, err := tx.AppendEntry(c.ID(), JSON{"kind": "pi.assistant", "model": []any{wire}})
		if err != nil {
			return nil, err
		}
		id, err = tx.CreateTask(ToolTask(), JSON{"kind": "model", "assistant": entry.ID, "callId": "p1"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		return nil, err
	})
	if err != nil {
		t.Fatal(err)
	}
	record := waitPublicTask(t, h, id)
	if record.State.Outcome == nil || record.State.Outcome.Status != "completed" || leaf.Load() != 0 {
		wire, _ := record.MarshalJSON()
		t.Fatal(string(wire), leaf.Load())
	}
	page, err := h.session.ScanTasks(bg, TaskQuery{Conversation: c.ID()}, 10, nil)
	if err != nil {
		t.Fatal(page, err)
	}
	children := []TaskRecord{}
	for _, task := range page.Items {
		if task.Owner == id {
			children = append(children, task)
		}
	}
	if len(children) != 1 {
		t.Fatal(children)
	}
	child := children[0]
	if child.Kind != "pi.tool" || child.Version != 3 || generationObject(child.Input.Value)["kind"] != "nested" || !equalJSONValue(child.State.Outcome.Result.Value, JSON{"kind": "nested"}) {
		wire, _ := child.MarshalJSON()
		t.Fatal(string(wire))
	}
}
