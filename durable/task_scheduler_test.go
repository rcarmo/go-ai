package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"math"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func taskDone(value any) *TaskState {
	return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: value}}}
}
func taskAbort(context.Context, TaskRecord, *TaskRuntime) error { return nil }
func taskDefinition(t *testing.T, kind string, phase TaskPhase) *TaskDefinition {
	t.Helper()
	def, err := DefineTask(TaskDefinitionOptions{Kind: kind, Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "work"}, nil }, Phases: map[string]TaskPhase{"work": phase}, Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
			return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
		})
	}})
	if err != nil {
		t.Fatal(err)
	}
	return def
}
func taskTestHarness(t *testing.T, store Storage, definitions ...*TaskDefinition) *Harness {
	return taskTestHarnessOptions(t, store, Options{}, definitions...)
}
func taskTestHarnessOptions(t *testing.T, store Storage, options Options, definitions ...*TaskDefinition) *Harness {
	t.Helper()
	registry := NewRegistry()
	for _, def := range definitions {
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
	}
	options.Registry = registry
	h, err := Open(bg, store, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := h.Close(bg); err != nil {
			t.Error(err)
		}
	})
	return h
}
func createPublicTask(t *testing.T, h *Harness, def *TaskDefinition, input any, options TaskOptions) ID {
	t.Helper()
	var id ID
	_, err := h.CommitTasks(bg, 1, func(tx *Tx) error { var err error; id, err = tx.CreateTask(def, input, options); return err })
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func releaseTaskGate(gate chan struct{}) {
	select {
	case <-gate:
	default:
		close(gate)
	}
}
func cleanupTaskGates(t *testing.T, gates ...chan struct{}) {
	t.Helper()
	t.Cleanup(func() {
		for _, gate := range gates {
			releaseTaskGate(gate)
		}
	})
}

func awaitTaskSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("deterministic handler/commit barrier timed out")
	}
}
func waitPublicTask(t *testing.T, h *Harness, id ID) TaskRecord {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	record, err := h.WaitForTask(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func TestTaskSchedulerPhasesMemoWinnerAndEndedCapabilities(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var captured *TaskRuntime
		var calls atomic.Int64
		var winners []any
		def := taskDefinition(t, "task.phases", func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			captured = r
			calls.Add(1)
			n := 0
			if value, ok := task.State.Checkpoint["n"]; ok {
				switch v := value.(type) {
				case int:
					n = v
				default:
					if equalJSONValue(v, 1) {
						n = 1
					}
				}
			}
			if n == 0 {
				if _, ok, err := r.Memo(ctx, "toString"); err != nil || ok {
					return errors.New("inherited memo property")
				}
				first, err := r.MemoCandidate(ctx, "toString", nil)
				if err != nil {
					return err
				}
				winners = append(winners, first)
				second, err := r.MemoCandidate(ctx, "toString", func() {})
				if err != nil {
					return err
				}
				winners = append(winners, second)
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "running", Checkpoint: JSON{"phase": "work", "n": 1}}, nil
				})
			}
			return r.Commit(ctx, func(tx *Tx, current TaskRecord) (*TaskState, error) {
				id, err := tx.MintID()
				if err != nil {
					return nil, err
				}
				if err := tx.AppendEntry(Entry{ID: id, Conversation: current.Conversation, Kind: "task.answer", Value: JSON{"answer": "owned"}}); err != nil {
					return nil, err
				}
				return taskDone(JSON{"entryId": id}), nil
			})
		})
		h := taskTestHarness(t, b.store, def)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		inspect, err := h.InspectTasks(bg)
		if err != nil || inspect.Scheduling != "paused" || calls.Load() != 0 {
			t.Fatal("read enabled effects", err)
		}
		record := waitPublicTask(t, h, id)
		if record.State.Outcome == nil || record.State.Outcome.Status != "completed" || calls.Load() != 2 || len(winners) != 2 || winners[0] != nil || winners[1] != nil || record.Memos != nil {
			t.Fatal("phase/memo result", record, calls.Load(), winners)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if err := h.WaitForIdle(ctx); err != nil {
			t.Fatal(err)
		}
		if _, _, err := captured.Memo(bg, "late"); !errors.Is(err, ErrSealed) {
			t.Fatal("ended memo", err)
		}
		if _, err := captured.Registry(); !errors.Is(err, ErrSealed) {
			t.Fatal("ended snapshot", err)
		}
		if err := captured.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("late"), nil }); !errors.Is(err, ErrSealed) {
			t.Fatal("ended commit", err)
		}
		raw, _ := h.Snapshot(bg)
		for _, entry := range raw.Entries {
			if entry.Kind == "task.answer" && entry.ByTask != id {
				t.Fatal("runtime entry attribution", entry)
			}
		}
	})
}

func TestTaskSchedulerCapacityParentChildYieldOne(t *testing.T) {
	for _, kind := range []string{"memory", "journal"} {
		t.Run(kind, func(t *testing.T) {
			l := DefaultLimits()
			l.MaxPage = 1
			var store Storage
			var err error
			if kind == "memory" {
				store, err = OpenMemory(MemoryOptions{Limits: &l})
			} else {
				store, err = OpenJournal(t.TempDir()+"/private", JournalOptions{Limits: &l})
			}
			if err != nil {
				t.Fatal(err)
			}
			var parentReturned atomic.Bool
			var childRuns atomic.Int64
			yielded := make(chan struct{})
			returned := make(chan struct{})
			heldWaiting, releaseHost := make(chan struct{}), make(chan struct{})
			child := taskDefinition(t, "task.capacity.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				if !parentReturned.Load() {
					return errors.New("child ran before parent host return")
				}
				childRuns.Add(1)
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("child"), nil })
			})
			var childID ID
			parent := taskDefinition(t, "task.capacity.parent", func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				if task.State.Checkpoint["phase"] == "join" {
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("parent"), nil })
				}
				if err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					var err error
					childID, err = tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
					return nil, err
				}); err != nil {
					return err
				}
				_, err := r.WaitForTask(ctx, childID)
				var yield *InvocationWaitRequiresYield
				if !errors.As(err, &yield) {
					return errors.New("C1 inline wait did not reject")
				}
				close(yielded)
				if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "join"}, On: []ID{childID}, Policy: "allSettled"}, nil
				}); err != nil {
					return err
				}
				close(heldWaiting)
				<-releaseHost
				parentReturned.Store(true)
				close(returned)
				return nil
			})
			options := parent.options
			options.Phases = map[string]TaskPhase{"work": parent.options.Phases["work"], "join": parent.options.Phases["work"]}
			parent, err = DefineTask(options)
			if err != nil {
				t.Fatal(err)
			}
			h := taskTestHarness(t, store, parent, child)
			cleanupTaskGates(t, releaseHost)
			id := createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			if err := h.Resume(bg); err != nil {
				t.Fatal(err)
			}
			awaitTaskSignal(t, yielded)
			awaitTaskSignal(t, heldWaiting)
			childRecord, ok, err := h.Task(bg, childID)
			if err != nil || !ok || childRecord.State.Status != "pending" || childRuns.Load() != 0 {
				t.Fatal("child started while yielded host still holds permit", childRecord, err)
			}
			peak := 0
			h.session.taskBookkeeping(func() { peak = len(h.scheduler.invocations) + len(h.scheduler.tickets) })
			if peak != 1 {
				t.Fatal("held host acquired permit count", peak)
			}
			releaseTaskGate(releaseHost)
			awaitTaskSignal(t, returned)
			record := waitPublicTask(t, h, id)
			if record.State.Outcome.Status != "completed" || childRuns.Load() != 1 {
				t.Fatal("capacity stranded", record, childRuns.Load())
			}
			var held int
			h.session.taskBookkeeping(func() { held = len(h.scheduler.invocations) + len(h.scheduler.tickets) })
			if held > 1 {
				t.Fatal("permit cap increased", held)
			}
		})
	}
}

func TestTaskSchedulerNoProgressAndCommittedTerminalPrecedence(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		equal := taskDefinition(t, "task.equal", func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "running", Checkpoint: JSON{"phase": "work"}}, nil
			})
		})
		done := taskDefinition(t, "task.done.throw", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil }); err != nil {
				return err
			}
			return errors.New("after durable terminal")
		})
		h := taskTestHarness(t, b.store, equal, done)
		a := createPublicTask(t, h, equal, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		bID := createPublicTask(t, h, done, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if record := waitPublicTask(t, h, a); record.State.Outcome.Status != "faulted" {
			t.Fatal("no progress accepted", record)
		}
		if record := waitPublicTask(t, h, bID); record.State.Outcome.Status != "completed" {
			t.Fatal("late handler error replaced terminal", record)
		}
	})
}

func TestTaskSchedulerConcurrentBoundWaitRegistrationCycle(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		aReady, bReady, cReady := make(chan struct{}), make(chan struct{}), make(chan struct{})
		letA, letB, finish, releaseC := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
		t.Cleanup(func() {
			select {
			case <-finish:
			default:
				close(finish)
			}
		})
		var a, bID, cID ID
		var cycleRejected atomic.Bool
		aDef := taskDefinition(t, "task.wait.a", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(aReady)
			<-letA
			errs := make(chan error, 2)
			for _, id := range []ID{bID, cID} {
				go func(id ID) { _, err := r.WaitForTask(ctx, id); errs <- err }(id)
			}
			<-finish
			for k := 0; k < 2; k++ {
				if err := <-errs; err != nil {
					return err
				}
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("a"), nil })
		})
		bDef := taskDefinition(t, "task.wait.b", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(bReady)
			<-letB
			_, err := r.WaitForTask(ctx, a)
			var overload *InvocationWaitRequiresYield
			if errors.As(err, &overload) {
				cycleRejected.Store(true)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("b"), nil })
		})
		cDef := taskDefinition(t, "task.wait.c", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(cReady)
			<-releaseC
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("c"), nil })
		})
		h := taskTestHarness(t, b.store, aDef, bDef, cDef)
		cleanupTaskGates(t, letA, letB, releaseC, finish)
		a = createPublicTask(t, h, aDef, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		bID = createPublicTask(t, h, bDef, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		cID = createPublicTask(t, h, cDef, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, aReady)
		awaitTaskSignal(t, bReady)
		awaitTaskSignal(t, cReady)
		close(letA)
		// Acknowledge BOTH actual line registrations before releasing B; goroutine
		// launch order is not admission order and no sleep establishes this gate.
		ack, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for {
			registered := 0
			h.session.taskBookkeeping(func() {
				for wait := range h.scheduler.waiters {
					if wait.binding != nil && wait.binding.taskID == a {
						registered++
					}
				}
			})
			if registered == 2 {
				break
			}
			select {
			case <-ack.Done():
				t.Fatal("both bound waits never registered")
			default:
				runtime.Gosched()
			}
		}
		close(letB)
		waitPublicTask(t, h, bID)
		close(releaseC)
		waitPublicTask(t, h, cID)
		close(finish)
		waitPublicTask(t, h, a)
		if !cycleRejected.Load() {
			t.Fatal("concurrent caller edge lost")
		}
	})
}

// Every request traverses the production local HTTP provider. Barriers belong
// to real tool/child execution and persisted Hold states, not a fake scheduler.
func taskHTTPRound(w http.ResponseWriter, calls []string) {
	w.Header().Set("Content-Type", "text/event-stream")
	if len(calls) == 0 {
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"done\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\ndata: [DONE]\n\n")
		return
	}
	tools := []any{}
	for index, name := range calls {
		tools = append(tools, map[string]any{"index": index, "id": fmt.Sprintf("call-%d", index), "type": "function", "function": map[string]any{"name": name, "arguments": "{}"}})
	}
	payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": tools}, "finish_reason": nil}}})
	fmt.Fprintf(w, "data: %s\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\ndata: [DONE]\n\n", payload)
}

func TestTaskSchedulerCapacityToolChildHoldOne(t *testing.T) {
	for _, backendName := range []string{"memory", "journal"} {
		t.Run(backendName, func(t *testing.T) {
			limits := DefaultLimits()
			limits.MaxPage = 1
			var store Storage
			var err error
			if backendName == "memory" {
				store, err = OpenMemory(MemoryOptions{Limits: &limits})
			} else {
				store, err = OpenJournal(t.TempDir()+"/private", JournalOptions{Limits: &limits})
			}
			if err != nil {
				t.Fatal(err)
			}
			var requests, childRuns, effects, appCallbacks atomic.Int64
			toolHolding, releaseTool, childEntered, releaseChild := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if requests.Add(1) == 1 {
					taskHTTPRound(w, []string{"delegate"})
				} else {
					taskHTTPRound(w, nil)
				}
			}))
			defer server.Close()
			model := fakeModel(goai.ApiOpenAICompletions)
			model.BaseURL = server.URL
			model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
			registry := NewRegistry()
			var h *Harness
			var childID, toolID ID
			child := taskDefinition(t, "task.c1.toolchild", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				childRuns.Add(1)
				close(childEntered)
				<-releaseChild
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("child"), nil })
			})
			if _, err := registry.RegisterTask(child); err != nil {
				t.Fatal(err)
			}
			err = registry.Register(ToolRegistration{Definition: goai.Tool{Name: "delegate", Description: "owned child", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "task.delegate", Version: 1, ReplaySafe: true, Execute: func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
				effects.Add(1)
				toolID = api.TaskID()
				id, err := api.CreateTask(ctx, child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: toolID}})
				if err != nil {
					return ToolResult{}, err
				}
				childID = id
				_, err = api.WaitForTask(ctx, id)
				var yield *InvocationWaitRequiresYield
				if !errors.As(err, &yield) {
					return ToolResult{}, errors.New("C1 tool child wait did not yield")
				}
				close(toolHolding)
				<-releaseTool
				return ToolResult{Content: "child admitted", Details: JSON{"result": "persisted"}, Usage: &goai.Usage{Output: 1, TotalTokens: 1}, Commit: func(tx *Tx) error {
					appCallbacks.Add(1)
					var docID ID
					for _, doc := range tx.state.Documents {
						if doc.Kind == "app.result" {
							docID = doc.ID
						}
					}
					doc, err := tx.Document(docID)
					if err != nil {
						return err
					}
					return doc.Set(JSON{"sum": 1})
				}}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			h = openHarness(t, store, Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }})
			cleanupTaskGates(t, releaseTool, releaseChild)
			conv := root(t, h, ModelRef{Provider: model.Provider, ID: model.ID})
			docID := createAppDocument(t, conv)
			sub, err := conv.Submit(bg, Input{Content: "delegate", RequestID: "C1"})
			if err != nil {
				t.Fatal(err)
			}
			awaitTaskSignal(t, toolHolding)
			childRecord, ok, err := h.Task(bg, childID)
			if err != nil || !ok || childRecord.State.Status != "pending" || childRuns.Load() != 0 {
				t.Fatal("tool-child started before real tool return", childRecord, err)
			}
			acquired := 0
			h.session.taskBookkeeping(func() { acquired = len(h.scheduler.invocations) + len(h.scheduler.tickets) })
			if acquired != 1 {
				t.Fatal("C1 acquired permit", acquired)
			}
			releaseTaskGate(releaseTool)
			awaitTaskSignal(t, childEntered)
			held := observeTaskState(t, h, toolID, "completing")
			if held.State.Outcome.Status != "completed" {
				t.Fatal(held)
			}
			raw, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			assertTaskToolSpend(t, raw, conv.ID(), "delegate", 1)
			if raw.Documents[docID].Value["sum"] != json.Number("1") || appCallbacks.Load() != 1 || requests.Load() != 1 {
				t.Fatal("Hold app/spend/continuation timing", raw.Documents[docID], appCallbacks.Load(), requests.Load())
			}
			var cp toolCheckpoint
			if err := fromObject(raw.Tasks[toolID].Checkpoint, &cp, h.session.limits); err != nil || cp.Result == nil || cp.Result.Details["result"] != "persisted" || cp.Result.Usage.TotalTokens != 1 {
				t.Fatal("decided receipt lost", cp, err)
			}
			releaseTaskGate(releaseChild)
			settled := waitSubmission(t, sub)
			if settled.Submission.Status != "done" || effects.Load() != 1 || childRuns.Load() != 1 || appCallbacks.Load() != 1 || requests.Load() != 2 {
				t.Fatal("C1 toolhold final duplicate/stranding", settled, effects.Load(), requests.Load())
			}
			final, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			assertTaskToolSpend(t, final, conv.ID(), "delegate", 1)
			if err := h.Close(bg); err != nil {
				t.Fatal(err)
			}
			h.session.taskBookkeeping(func() {
				if len(h.scheduler.invocations)+len(h.scheduler.tickets)+len(h.scheduler.waiters) != 0 {
					t.Error("Close retained permit/ticket/wait")
				}
			})
		})
	}
}

func TestTaskSchedulerLegacyChildrenSequentialEligibility(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		firstEntered, firstRelease, secondEntered, secondRelease, childEntered, childRelease := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
		var requests, active, peak atomic.Int64
		var firstID ID
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if requests.Add(1) == 1 {
				taskHTTPRound(w, []string{"first", "second", "third"})
			} else {
				taskHTTPRound(w, nil)
			}
		}))
		defer server.Close()
		registry := NewRegistry()
		model := fakeModel(goai.ApiOpenAICompletions)
		model.BaseURL = server.URL
		model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
		child := taskDefinition(t, "task.sequence.held", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(childEntered)
			<-childRelease
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(child); err != nil {
			t.Fatal(err)
		}
		var callsMu sync.Mutex
		calls := []string{}
		for _, name := range []string{"first", "second", "third"} {
			name := name
			if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: name, Description: name, Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "sequence." + name, Version: 1, ReplaySafe: true, Execute: func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
				value := active.Add(1)
				for {
					old := peak.Load()
					if value <= old || peak.CompareAndSwap(old, value) {
						break
					}
				}
				defer active.Add(-1)
				callsMu.Lock()
				calls = append(calls, name)
				callsMu.Unlock()
				if name == "first" {
					firstID = api.TaskID()
					if _, err := api.CreateTask(ctx, child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: firstID}}); err != nil {
						return ToolResult{}, err
					}
					close(firstEntered)
					<-firstRelease
				}
				if name == "second" {
					close(secondEntered)
					<-secondRelease
				}
				return ToolResult{Content: name}, nil
			}}); err != nil {
				t.Fatal(err)
			}
		}
		h := openHarness(t, b.store, Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }})
		cleanupTaskGates(t, firstRelease, secondRelease, childRelease)
		conv, err := h.Root(bg, AgentChange{Model: ModelRef{model.Provider, model.ID}, Settings: RequestSettings{ToolExecution: "sequential"}})
		if err != nil {
			t.Fatal(err)
		}
		sub, err := conv.Submit(bg, Input{Content: "ordered"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, firstEntered)
		awaitTaskSignal(t, childEntered)
		releaseTaskGate(firstRelease)
		observeTaskState(t, h, firstID, "completing")
		callsMu.Lock()
		count := len(calls)
		callsMu.Unlock()
		if count != 1 || requests.Load() != 1 {
			t.Fatal("later listed tool bypassed Hold", count, requests.Load())
		}
		releaseTaskGate(childRelease)
		awaitTaskSignal(t, secondEntered)
		if requests.Load() != 1 {
			t.Fatal("successor ran before second host return")
		}
		releaseTaskGate(secondRelease)
		if result := waitSubmission(t, sub); result.Submission.Status != "done" {
			t.Fatal(result)
		}
		callsMu.Lock()
		defer callsMu.Unlock()
		if strings.Join(calls, ",") != "first,second,third" || peak.Load() != 1 || requests.Load() != 2 {
			t.Fatal("legacy order/overlap", calls, peak.Load(), requests.Load())
		}
	})
}

func taskLimitedBackends(t *testing.T, capacity int, run func(*testing.T, Storage)) {
	t.Helper()
	for _, name := range []string{"memory", "journal"} {
		t.Run(name, func(t *testing.T) {
			limits := DefaultLimits()
			limits.MaxPage = capacity
			var store Storage
			var err error
			if name == "memory" {
				store, err = OpenMemory(MemoryOptions{Limits: &limits})
			} else {
				store, err = OpenJournal(t.TempDir()+"/private", JournalOptions{Limits: &limits})
			}
			if err != nil {
				t.Fatal(err)
			}
			run(t, store)
		})
	}
}

func TestTaskSchedulerAdoptedFrontierBeforeOrdinaryBacklog(t *testing.T) {
	taskLimitedBackends(t, 3, func(t *testing.T, store Storage) {
		callerEntered, letCaller := make(chan struct{}), make(chan struct{})
		targetEntered, releaseTarget := make(chan struct{}), make(chan struct{})
		ordinaryEntered, releaseOrdinary := make(chan struct{}), make(chan struct{})
		var callerID, targetID, ordinaryID ID
		var ordinaryRuns atomic.Int64
		target := taskDefinition(t, "task.frontier.target", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(targetEntered)
			<-releaseTarget
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("target"), nil })
		})
		caller := taskDefinition(t, "task.frontier.caller", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(callerEntered)
			<-letCaller
			if _, err := r.WaitForTask(ctx, targetID); err != nil {
				return err
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("caller"), nil })
		})
		ordinary := taskDefinition(t, "task.frontier.ordinary", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			ordinaryRuns.Add(1)
			close(ordinaryEntered)
			<-releaseOrdinary
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("ordinary"), nil })
		})
		h := taskTestHarness(t, store, target, caller, ordinary)
		cleanupTaskGates(t, letCaller, releaseTarget, releaseOrdinary)
		targetID = createPublicTask(t, h, target, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		callerID = createPublicTask(t, h, caller, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		ordinaryID = createPublicTask(t, h, ordinary, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		h.session.taskBookkeeping(func() { h.scheduler.cursor = targetID })
		reservations, err := h.scheduler.reservePass(false)
		if err != nil || len(reservations) != 1 || reservations[0].taskID != callerID {
			discardUnstartedTaskReservations(t, h, reservations)
			t.Fatal("caller reservation fixture", err)
		}
		callerRuntime := reservations[0]
		callerStarted := false
		t.Cleanup(func() {
			if !callerStarted {
				discardUnstartedTaskReservations(t, h, []*TaskRuntime{callerRuntime})
			}
		})
		callerStarted = true
		go h.scheduler.run(callerRuntime)
		awaitTaskSignal(t, callerEntered)
		releaseTaskGate(letCaller)
		ack, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for {
			registered := false
			h.session.taskBookkeeping(func() {
				for wait := range h.scheduler.waiters {
					if wait.binding != nil && wait.binding.taskID == callerID && wait.target == targetID && h.scheduler.tickets[targetID][wait] {
						registered = true
					}
				}
			})
			if registered {
				break
			}
			select {
			case <-ack.Done():
				t.Fatal("caller actual wait admission missing")
			default:
				runtime.Gosched()
			}
		}
		var held int
		h.session.taskBookkeeping(func() { held = len(h.scheduler.invocations) + len(h.scheduler.tickets) })
		if held != 2 {
			t.Fatal("spare-capacity ticket fixture lost", held)
		}
		pending, ok, err := h.Task(bg, ordinaryID)
		if err != nil || !ok || pending.State.Status != "pending" {
			t.Fatal("ordinary backlog fixture missing", pending, err)
		}
		reservations, err = h.scheduler.reservePass(false)
		if err != nil || len(reservations) != 1 || reservations[0].taskID != targetID {
			discardUnstartedTaskReservations(t, h, reservations)
			t.Fatal("ticket target lost priority over earlier cyclic ordinary backlog", err, reservations)
		}
		targetRuntime := reservations[0]
		targetStarted := false
		t.Cleanup(func() {
			if !targetStarted {
				discardUnstartedTaskReservations(t, h, []*TaskRuntime{targetRuntime})
			}
		})
		if ordinaryRuns.Load() != 0 {
			t.Fatal("ordinary backlog stole spare permit")
		}
		pending, ok, err = h.Task(bg, ordinaryID)
		if err != nil || !ok || pending.State.Status != "pending" {
			t.Fatal("ordinary backlog not left pending", pending, err)
		}
		targetStarted = true
		go h.scheduler.run(targetRuntime)
		awaitTaskSignal(t, targetEntered)
		releaseTaskGate(releaseTarget)
		awaitTaskSignal(t, targetRuntime.done)
		awaitTaskSignal(t, callerRuntime.done)
		callerRecord, ok, err := h.Task(bg, callerID)
		if err != nil || !ok || callerRecord.State.Outcome == nil || callerRecord.State.Outcome.Status != "completed" {
			t.Fatal("caller did not finish after ticket target", callerRecord, err)
		}
		if ordinaryRuns.Load() != 0 {
			t.Fatal("ordinary backlog ran before ticket chain settled")
		}
		reservations, err = h.scheduler.reservePass(false)
		if err != nil || len(reservations) != 1 || reservations[0].taskID != ordinaryID {
			discardUnstartedTaskReservations(t, h, reservations)
			t.Fatal("ordinary backlog not retained after ticket work", err)
		}
		ordinaryRuntime := reservations[0]
		ordinaryStarted := false
		t.Cleanup(func() {
			if !ordinaryStarted {
				discardUnstartedTaskReservations(t, h, []*TaskRuntime{ordinaryRuntime})
			}
		})
		ordinaryStarted = true
		go h.scheduler.run(ordinaryRuntime)
		awaitTaskSignal(t, ordinaryEntered)
		releaseTaskGate(releaseOrdinary)
		awaitTaskSignal(t, ordinaryRuntime.done)
		if ordinaryRuns.Load() != 1 {
			t.Fatal("ordinary backlog final run count", ordinaryRuns.Load())
		}
	})
}

func TestTaskSchedulerIdleFrontierAdmissionAtomic(t *testing.T) {
	taskLimitedBackends(t, 2, func(t *testing.T, store Storage) {
		entered, letWait, rejected, releaseHost, releaseChildren := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
		var h *Harness
		var foreign ID
		child := taskDefinition(t, "task.idle.atomic.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			<-releaseChildren
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		parent := taskDefinition(t, "task.idle.atomic.caller", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(entered)
			<-letWait
			// Private selection seam, not a fake task outcome: an in-flight pass
			// rechecks enabled ONline. Actual caller/children remain public tasks.
			h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(false) })
			defer h.scheduler.enable()
			if err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				for k := 0; k < 2; k++ {
					if _, err := tx.CreateTask(child, nil, TaskOptions{Conversation: foreign, Ownership: TaskOwnership{Kind: "conversation"}}); err != nil {
						return nil, err
					}
				}
				return nil, nil
			}); err != nil {
				return err
			}
			conv, err := r.Conversation(ctx, foreign)
			if err != nil {
				return err
			}
			// Bypass only the public Resume wake; exercise its identical internal
			// idle admission with selection held until the whole rejection lands.
			err = h.waitIdleAdmission(ctx, conv.ID(), r)
			var yield *InvocationWaitRequiresYield
			if !errors.As(err, &yield) {
				return fmt.Errorf("whole idle frontier not rejected: %w", err)
			}
			// Inspect while this caller's permit is still live. A failed admission
			// may leave neither an edge nor a partial claim for its first target.
			var claims, registrations int
			h.session.taskBookkeeping(func() {
				for wait := range h.scheduler.waiters {
					if wait.binding == r {
						registrations++
					}
				}
				for _, owners := range h.scheduler.tickets {
					for wait := range owners {
						if wait.binding == r {
							claims++
						}
					}
				}
			})
			if claims != 0 || registrations != 0 {
				return errors.New("rejected idle leaked provisional frontier")
			}
			close(rejected)
			<-releaseHost
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		h = taskTestHarness(t, store, parent, child)
		cleanupTaskGates(t, letWait, releaseHost, releaseChildren)
		_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			foreign, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.CreateConversation(Conversation{ID: foreign})
		})
		if err != nil {
			t.Fatal(err)
		}
		id := createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		releaseTaskGate(letWait)
		awaitTaskSignal(t, rejected)
		releaseTaskGate(releaseHost)
		releaseTaskGate(releaseChildren)
		waitPublicTask(t, h, id)
	})
}

func TestTaskSchedulerTerminalHostReturnKeepsPermitAndPinsDrain(t *testing.T) {
	taskLimitedBackends(t, 1, func(t *testing.T, store Storage) {
		decided, release, childEntered := make(chan struct{}), make(chan struct{}), make(chan struct{})
		def := taskDefinition(t, "task.terminal.host", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("decided"), nil }); err != nil {
				return err
			}
			close(decided)
			<-release
			return nil
		})
		child := taskDefinition(t, "task.terminal.backlog", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(childEntered)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		h := taskTestHarness(t, store, def, child)
		cleanupTaskGates(t, release)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, decided)
		if record := waitPublicTask(t, h, id); record.State.Outcome.Status != "completed" {
			t.Fatal(record)
		}
		backlog := createPublicTask(t, h, child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		reservations, err := h.scheduler.reserve()

		discardUnstartedTaskReservations(t, h, reservations)
		if err != nil || len(reservations) != 0 {
			t.Fatal("terminal host permit released early", reservations, err)
		}
		raw, ok, err := h.Task(bg, backlog)
		if err != nil || !ok || raw.State.Status != "pending" {
			t.Fatal(raw, err)
		}
		closing := make(chan error, 1)
		go func() { closing <- h.Close(bg) }()
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for !h.closing.Load() {
			select {
			case <-ctx.Done():
				t.Fatal("close seal missing")
			default:
				runtime.Gosched()
			}
		}
		h.session.taskBookkeeping(func() {
			if len(h.scheduler.invocations) != 1 {
				t.Error("terminal host join lost")
			}
		})
		select {
		case err := <-closing:
			t.Fatal("Close released terminal stubborn host", err)
		default:
		}
		releaseTaskGate(release)
		select {
		case err := <-closing:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("Close did not join actual return")
		}
		select {
		case <-childEntered:
			t.Fatal("postseal backlog effect")
		default:
		}
	})
}

func TestTaskSchedulerCompletedGenerationsReleasePinnedRegistrations(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var requests atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); taskHTTPRound(w, nil) }))
		defer server.Close()
		registry := NewRegistry()
		if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "unused", Description: "pin", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "unused.pin", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			return ToolResult{}, errors.New("unused tool dispatched")
		}}); err != nil {
			t.Fatal(err)
		}
		model := fakeModel(goai.ApiOpenAICompletions)
		model.BaseURL = server.URL
		model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
		h := openHarness(t, b.store, Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }})
		conv := root(t, h, ModelRef{model.Provider, model.ID})
		for k := 0; k < 6; k++ {
			sub, err := conv.Submit(bg, Input{Content: "answer", RequestID: fmt.Sprintf("pin-%d", k)})
			if err != nil {
				t.Fatal(err)
			}
			if settled := waitSubmission(t, sub); settled.Submission.Status != "done" {
				t.Fatal(settled)
			}
			ctx, cancel := context.WithTimeout(bg, 3*time.Second)
			if err := conv.WaitForIdle(ctx); err != nil {
				cancel()
				t.Fatal(err)
			}
			cancel()
			// Force the same off-line pruning path after actual host return. No
			// success inferred from asynchronous diagnostic goroutine scheduling.
			h.scheduler.updateDiagnostics()
			h.mu.Lock()
			pins := len(h.pins)
			h.mu.Unlock()
			if pins != 0 {
				t.Fatal("completed generation retained host closures", pins)
			}
		}
		if requests.Load() != 6 {
			t.Fatal("unexpected requests", requests.Load())
		}
	})
}

func TestTaskSchedulerBoundWriteAttributionAndEscapedSettlementSeal(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var captured *InvocationSubmission
		var boundConversation *InvocationConversation
		var subID ID
		parent := taskDefinition(t, "task.bound.write", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			var err error
			boundConversation, err = r.Conversation(ctx, 1)
			if err != nil {
				return err
			}
			captured, err = boundConversation.Submit(ctx, Input{Type: "write", Content: "task-attributed", RequestID: "bound-write"})
			if err != nil {
				return err
			}
			subID = captured.ID()
			settled, err := captured.Wait(ctx)
			if err != nil {
				return err
			}
			if settled.Submission.Status != "done" {
				return errors.New("bound write not settled")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("sealed"), nil })
		})
		h := taskTestHarness(t, b.store, parent)
		// Configure target through the genuine native public conversation API.
		root(t, h, ModelRef{goai.ProviderOpenAI, "unused-bound-model"})
		id := createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		waitPublicTask(t, h, id)
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		var writes int
		for _, entry := range state.Entries {
			if entry.Kind == "message" {
				var receipt MessageReceipt
				if fromObject(entry.Value, &receipt, h.session.limits) == nil && receipt.Role == goai.RoleUser {
					writes++
					if entry.ByTask != id || entry.Conversation != 1 {
						t.Fatal("bound write attribution", entry)
					}
				}
			}
		}
		if writes != 1 || state.Submissions[subID].Status != "done" {
			t.Fatal("bound write placement", writes)
		}
		// Terminal publication is an adopted seal; final read checks on its
		// own admission and cannot reuse the ordinary host submission Wait.
		if _, err := captured.Wait(bg); !errors.Is(err, ErrSealed) {
			t.Fatal("escaped bound settlement survived adopted seal", err)
		}
		if _, err := boundConversation.Submit(bg, Input{Type: "write", Content: "late"}); !errors.Is(err, ErrSealed) {
			t.Fatal("escaped submit survived adopted seal", err)
		}
		after, err := h.Snapshot(bg)
		if err != nil || after.Seq != state.Seq {
			t.Fatal("escaped bound write published", after.Seq, state.Seq, err)
		}
	})
}

func TestTaskSchedulerEscapedIdleCannotWakeFailedIntent(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, releaseHost := make(chan struct{}), make(chan struct{})
		var captured *TaskRuntime
		var bound *InvocationConversation
		var foreign ID
		var targetCalls atomic.Int64
		caller := taskDefinition(t, "task.idle.seal.caller", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			captured = r
			var err error
			bound, err = r.Conversation(ctx, foreign)
			if err != nil {
				return err
			}
			// This public idle admission precedes the seal; the empty foreign
			// scope resolves and may legitimately enable shared scheduling.
			if err := bound.WaitForIdle(ctx); err != nil {
				return err
			}
			close(entered)
			<-releaseHost
			return nil
		})
		target := taskDefinition(t, "task.idle.seal.target", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			targetCalls.Add(1)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		h := taskTestHarness(t, b.store, caller, target)
		cleanupTaskGates(t, releaseHost)
		_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			foreign, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.CreateConversation(Conversation{ID: foreign})
		})
		if err != nil {
			t.Fatal(err)
		}
		id := createPublicTask(t, h, caller, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(false) })
		targetID := createPublicTask(t, h, target, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		var epoch uint64
		h.session.taskBookkeeping(func() { epoch = h.scheduler.epoch.Load(); h.scheduler.retryAfter[targetID] = epoch })
		// Admission is held by a real deciding runtime commit. Queue the shared
		// authoritative idle path behind it, bypassing only the early fast check:
		// this models a caller that passed that check before the adopted seal.
		queued, started := make(chan error, 1), make(chan struct{})
		if err := captured.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) {
			go func() { close(started); queued <- h.waitIdleAdmissionMode(bg, foreign, captured, true) }()
			<-started
			return taskDone("seal"), nil
		}); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-queued:
			if !errors.Is(err, ErrSealed) {
				t.Fatal("queued idle survived adopted seal", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("queued idle admission missing")
		}
		if err := bound.WaitForIdle(bg); !errors.Is(err, ErrSealed) {
			t.Fatal("escaped public idle", err)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		h.session.taskBookkeeping(func() {
			if h.scheduler.epoch.Load() != epoch || h.scheduler.enabled.Load() || h.scheduler.retryAfter[targetID] != epoch || h.scheduler.runnable(state, state.Tasks[targetID]) {
				t.Error("sealed idle donated retry permission")
			}
			if len(h.scheduler.waiters) != 0 || len(h.scheduler.tickets) != 0 {
				t.Error("sealed idle registered dependencies")
			}
		})
		if state.Tasks[id].Status != "done" || state.Tasks[targetID].Status != "pending" || targetCalls.Load() != 0 {
			t.Fatal("sealed idle changed intent", state.Tasks[id], state.Tasks[targetID])
		}
		releaseTaskGate(releaseHost)
	})
}

func TestTaskSchedulerBoundPendingDedupSealedAdmissionIsInert(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, releaseHost, attempted := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var captured *TaskRuntime
		var bound *InvocationConversation
		var foreign ID
		caller := taskDefinition(t, "task.bound.dedup.caller", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			captured = r
			var err error
			bound, err = r.Conversation(ctx, foreign)
			if err != nil {
				return err
			}
			close(entered)
			<-releaseHost
			return nil
		})
		h := taskTestHarness(t, b.store, caller)
		cleanupTaskGates(t, releaseHost)
		_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			foreign, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.CreateConversation(Conversation{ID: foreign})
		})
		if err != nil {
			t.Fatal(err)
		}
		createPublicTask(t, h, caller, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(false) })
		var subID ID
		_, err = h.CommitTasks(bg, foreign, func(tx *Tx) error {
			var err error
			subID, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.PutSubmission(Submission{ID: subID, Conversation: foreign, RequestID: "pending-dedup", Type: "follow-up", Status: "pending", Value: JSON{"content": "original"}})
		})
		if err != nil {
			t.Fatal(err)
		}
		// Live dedup resolves the existing request and may enable scheduling.
		beforeLive := h.scheduler.epoch.Load()
		live, err := bound.Submit(bg, Input{Content: "dedup live", RequestID: "pending-dedup"})
		if err != nil || live.ID() != subID || h.scheduler.epoch.Load() <= beforeLive {
			t.Fatal("live bound dedup did not resolve/wake", err)
		}
		h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(false) })
		// Hold h.mu around the decisive admission, and explicitly witness the
		// already-passed early check. Seal via runtime without taking h.mu.
		result := make(chan error, 1)
		var epoch uint64
		err = func() error {
			h.mu.Lock()
			defer h.mu.Unlock() // release even if a failure barrier calls Goexit
			go func() {
				if err := captured.check(); err != nil {
					result <- err
					close(attempted)
					return
				}
				close(attempted)
				_, err := bound.handle.submitAdmission(bg, Input{Content: "same request", RequestID: "pending-dedup"}, captured)
				result <- err
			}()
			awaitTaskSignal(t, attempted)
			err := captured.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("sealed"), nil })
			h.session.taskBookkeeping(func() { epoch = h.scheduler.epoch.Load() })
			return err
		}()
		if err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-result:
			if !errors.Is(err, ErrSealed) {
				t.Fatal("bound dedup survived authoritative seal", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("bound dedup admission missing")
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		h.session.taskBookkeeping(func() {
			if h.scheduler.epoch.Load() != epoch || h.scheduler.enabled.Load() {
				t.Error("sealed dedup enabled/woke scheduling")
			}
		})
		if len(state.Submissions) != 1 || state.Submissions[subID].Status != "pending" || len(state.Tasks) != 1 || len(state.Entries) != 0 {
			t.Fatal("sealed dedup changed admitted records")
		}
		releaseTaskGate(releaseHost)
	})
}

// A failing private reserve assertion must not strand an ADOPTED but unstarted
// runtime in Close. These test-owned runtimes have never run host code; cancel
// them off-line, then release their real scheduler registration under the line.
func discardUnstartedTaskReservations(t *testing.T, h *Harness, reservations []*TaskRuntime) {
	t.Helper()
	for _, r := range reservations {
		r.cancel()
	}
	h.session.taskBookkeeping(func() {
		for _, r := range reservations {
			if h.scheduler.invocations[r.taskID] != r {
				continue
			}
			delete(h.scheduler.invocations, r.taskID)
			r.ended.Store(true)
			close(r.done)
		}
	})
}

func TestTaskSchedulerCycleThroughDurableHoldOrWaitRejects(t *testing.T) {
	for _, join := range []string{"completing", "waiting"} {
		t.Run(join, func(t *testing.T) {
			taskLimitedBackends(t, 2, func(t *testing.T, store Storage) {
				aEntered, bEntered, letA, letB, releaseA := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
				var aID, bID, parentID ID
				var rejected atomic.Bool
				cycleRejected := make(chan struct{})
				a := taskDefinition(t, "task.cycle.durable.a", func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
					if task.State.Checkpoint["phase"] == "join" {
						return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("a"), nil })
					}
					close(aEntered)
					<-letA
					if _, err := r.WaitForTask(ctx, parentID); err != nil {
						var yield *InvocationWaitRequiresYield
						if join != "waiting" || !errors.As(err, &yield) {
							return err
						}
						// B is terminal but still owns its real return permit; P's
						// fresh executor needs capacity. Yield durably and RETURN.
						return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
							return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "join"}, On: []ID{parentID}, Policy: "allSettled"}, nil
						})
					}
					<-releaseA
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("a"), nil })
				})
				aOptions := a.options
				aOptions.Phases = map[string]TaskPhase{"work": a.options.Phases["work"], "join": a.options.Phases["work"]}
				var definitionErr error
				a, definitionErr = DefineTask(aOptions)
				if definitionErr != nil {
					t.Fatal(definitionErr)
				}
				b := taskDefinition(t, "task.cycle.durable.b", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					close(bEntered)
					<-letB
					_, err := r.WaitForTask(ctx, aID)
					var yield *InvocationWaitRequiresYield
					if !errors.As(err, &yield) {
						return fmt.Errorf("cycle through durable parent did not yield: %v", err)
					}
					rejected.Store(true)
					close(cycleRejected)
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("b"), nil })
				})
				parent := taskDefinition(t, "task.cycle.durable.parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("parent"), nil })
				})
				h := taskTestHarness(t, store, a, b, parent)
				cleanupTaskGates(t, letA, letB, releaseA)
				aID = createPublicTask(t, h, a, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
					var err error
					parentID, err = tx.CreateTask(parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
					if err != nil {
						return err
					}
					bID, err = tx.CreateTask(b, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: parentID}})
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
					task, err := copyTask(tx.state.Tasks[parentID], tx.limits)
					if err != nil {
						return err
					}
					if join == "completing" {
						task.Status = "completing"
						task.Execution.Native.State = TaskState{Status: "completing", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: "parent"}}}
					} else {
						task.Status = "waiting"
						task.Execution.Native.State = TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: []ID{bID}, Policy: "allSettled"}
					}
					return tx.stage(Write{Op: "put-task", Task: &task})
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, aEntered)
				awaitTaskSignal(t, bEntered)
				releaseTaskGate(letA)
				ctx, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				for {
					registered := false
					h.session.taskBookkeeping(func() {
						for wait := range h.scheduler.waiters {
							if wait.binding != nil && wait.binding.taskID == aID && wait.target == parentID {
								registered = true
							}
						}
					})
					if registered {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal("first A->P dependency not admitted")
					default:
						runtime.Gosched()
					}
				}
				releaseTaskGate(letB)
				// Do not fill the two-registration limit with a public waiter before
				// B attempts its cycle admission; observe the actual rejection first.
				awaitTaskSignal(t, cycleRejected)
				bRecord := waitPublicTask(t, h, bID)
				if !rejected.Load() {
					t.Fatalf("B->A through persisted P join not rejected: outcome=%+v error=%+v", bRecord.State.Outcome, bRecord.State.Outcome.Error)
				}
				h.session.taskBookkeeping(func() {
					for wait := range h.scheduler.waiters {
						if wait.binding != nil && wait.binding.taskID == bID {
							t.Error("rejected cycle retained wait")
						}
					}
				})
				releaseTaskGate(releaseA)
				waitPublicTask(t, h, aID)
				waitPublicTask(t, h, parentID)
			})
		})
	}
}

func TestTaskSchedulerMarkedForeignWaitDoesNotCreateStaleCycle(t *testing.T) {
	taskLimitedBackends(t, 2, func(t *testing.T, store Storage) {
		entered, letWait := make(chan struct{}), make(chan struct{})
		var aID, bID ID
		var abortCalls, oldPhaseCalls atomic.Int64
		a := taskDefinition(t, "task.marked.foreign.target", func(context.Context, TaskRecord, *TaskRuntime) error {
			oldPhaseCalls.Add(1)
			return errors.New("marked phase dispatched")
		})
		options := a.options
		options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			abortCalls.Add(1)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
			})
		}
		var err error
		a, err = DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		b := taskDefinition(t, "task.marked.foreign.caller", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(entered)
			<-letWait
			if _, err := r.WaitForTask(ctx, aID); err != nil {
				return err
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("foreign abort joined"), nil })
		})
		h := taskTestHarness(t, store, a, b)
		cleanupTaskGates(t, letWait)
		bID = createPublicTask(t, h, b, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(false) })
		aID = createPublicTask(t, h, a, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			task, err := copyTask(tx.state.Tasks[aID], tx.limits)
			if err != nil {
				return err
			}
			task.Status = "waiting"
			task.Execution.Native.State = TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: []ID{bID}, Policy: "allSettled"}
			task = markTask(task)
			return tx.stage(Write{Op: "put-task", Task: &task})
		})
		if err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(letWait)
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for {
			claimed := false
			h.session.taskBookkeeping(func() {
				for wait := range h.scheduler.waiters {
					if wait.binding != nil && wait.binding.taskID == bID && wait.target == aID && h.scheduler.tickets[aID][wait] {
						claimed = true
					}
				}
			})
			if claimed {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("stale foreign waiting cycle rejected abort ticket")
			default:
				runtime.Gosched()
			}
		}
		h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(true) })
		h.scheduler.kick()
		if record := waitPublicTask(t, h, bID); record.State.Outcome.Status != "completed" {
			t.Fatal(record)
		}
		if abortCalls.Load() != 1 || oldPhaseCalls.Load() != 0 {
			t.Fatal("marked foreign wait dispatch", abortCalls.Load(), oldPhaseCalls.Load())
		}
	})
}

func TestTaskSchedulerBoundAbortSealedOrSelfScopeAdmissionIsInert(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, releaseHost := make(chan struct{}), make(chan struct{})
		var captured *TaskRuntime
		var foreign ID
		caller := taskDefinition(t, "task.abort.seal.caller", func(_ context.Context, _ TaskRecord, r *TaskRuntime) error {
			captured = r
			close(entered)
			<-releaseHost
			return nil
		})
		target := taskDefinition(t, "task.abort.seal.target", func(context.Context, TaskRecord, *TaskRuntime) error { return errors.New("failed target executed") })
		h := taskTestHarness(t, b.store, caller, target)
		cleanupTaskGates(t, releaseHost)
		_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			foreign, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.CreateConversation(Conversation{ID: foreign})
		})
		if err != nil {
			t.Fatal(err)
		}
		callerID := createPublicTask(t, h, caller, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(false) })
		var targetID ID
		_, err = h.CommitTasks(bg, foreign, func(tx *Tx) error {
			var err error
			targetID, err = tx.CreateTask(target, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		rootHandle, _ := h.Conversation(bg, 1)
		foreignHandle, _ := h.Conversation(bg, foreign)
		var epoch uint64
		h.session.taskBookkeeping(func() { epoch = h.scheduler.epoch.Load(); h.scheduler.retryAfter[targetID] = epoch })
		// A live self-scope rejection grants no marks, cancellation or wake.
		err = rootHandle.abortAdmission(bg, ConversationAbortOptions{}, captured)
		var yield *InvocationWaitRequiresYield
		if !errors.As(err, &yield) {
			t.Fatal("self-scope abort was admitted", err)
		}
		h.session.taskBookkeeping(func() {
			if h.scheduler.epoch.Load() != epoch || h.scheduler.enabled.Load() {
				t.Error("selfscope abort woke scheduling")
			}
		})
		queued := make(chan error, 1)
		if err := captured.check(); err != nil {
			t.Fatal(err)
		}
		// The real deciding commit holds the line until seal adoption. The
		// authoritative helper models an already-passed early check without
		// claiming goroutine launch determines admission order.
		err = captured.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) {
			go func() { queued <- foreignHandle.abortAdmission(bg, ConversationAbortOptions{}, captured) }()
			return taskDone("seal"), nil
		})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-queued:
			if !errors.Is(err, ErrSealed) {
				t.Fatal("seal-overtaken bound abort admitted", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("queued bound abort not settled")
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		h.session.taskBookkeeping(func() {
			if h.scheduler.epoch.Load() != epoch || h.scheduler.enabled.Load() || h.scheduler.retryAfter[targetID] != epoch {
				t.Error("sealed abort donated retry epoch")
			}
		})
		if state.Tasks[callerID].Status != "done" || state.Tasks[targetID].Status != "pending" || taskAborted(state.Tasks[targetID]) {
			t.Fatal("rejected bound abort changed target", state.Tasks[targetID])
		}
		releaseTaskGate(releaseHost)
	})
}

func TestTaskSchedulerPersistedWriteBudgetReservationsAndRetirement(t *testing.T) {
	for _, maxWrites := range []int{1, 4} {
		for _, backendName := range []string{"memory", "journal"} {
			t.Run(fmt.Sprintf("writes%d/%s", maxWrites, backendName), func(t *testing.T) {
				limits := DefaultLimits()
				limits.MaxPage = 2
				limits.MaxWrites = maxWrites
				var store Storage
				var err error
				if backendName == "memory" {
					store, err = OpenMemory(MemoryOptions{Limits: &limits})
				} else {
					store, err = OpenJournal(t.TempDir()+"/private", JournalOptions{Limits: &limits})
				}
				if err != nil {
					t.Fatal(err)
				}
				entered := make(chan ID, 2)
				release := make(chan struct{})
				var calls atomic.Int64
				def := taskDefinition(t, "task.persisted.write-budget", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					calls.Add(1)
					entered <- r.TaskID()
					<-release
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("bounded"), nil })
				})
				reports := make(chan error, 4)
				h := taskTestHarnessOptions(t, store, Options{OnReport: func(err error) {
					select {
					case reports <- err:
					default:
					}
				}}, def)
				cleanupTaskGates(t, release)
				first := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				second := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				var docIDs []ID
				if maxWrites == 4 {
					// Three task docs + one terminal write exactly fit each task's
					// indivisible final transition; combining both would exceed4.
					for _, owner := range []ID{first, second} {
						for k := 0; k < 3; k++ {
							_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
								id, err := tx.MintID()
								if err != nil {
									return err
								}
								docIDs = append(docIDs, id)
								_, err = tx.CreateDocument(Document{ID: id, Scope: "task", Owner: owner, Kind: fmt.Sprintf("app.budget%d", k), Version: 1, Value: JSON{}})
								return err
							})
							if err != nil {
								t.Fatal(err)
							}
						}
					}
				}
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				seen := map[ID]bool{}
				for k := 0; k < 2; k++ {
					select {
					case id := <-entered:
						seen[id] = true
					case err := <-reports:
						t.Fatal("valid prefix rejected by scheduler batching", err)
					case <-time.After(3 * time.Second):
						t.Fatal("tight persisted budget stranded task")
					}
				}
				if !seen[first] || !seen[second] || calls.Load() != 2 {
					t.Fatal("reservation selection lost valid work")
				}
				releaseTaskGate(release)
				for _, id := range []ID{first, second} {
					record := waitPublicTask(t, h, id)
					if record.State.Outcome.Status != "completed" {
						t.Fatal(record)
					}
				}
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				for _, id := range docIDs {
					if !state.Documents[id].Retired {
						t.Fatal("terminal budget retirement missing", id)
					}
				}
				select {
				case err := <-reports:
					t.Fatal("write-budget task pass rejected", err)
				default:
				}
			})
		}
	}
}

func TestTaskSchedulerPersistedWriteBudgetCancellationPrefix(t *testing.T) {
	for _, backendName := range []string{"memory", "journal"} {
		t.Run(backendName, func(t *testing.T) {
			l := DefaultLimits()
			l.MaxWrites = 1
			l.MaxPage = 2
			var store Storage
			var err error
			if backendName == "memory" {
				store, err = OpenMemory(MemoryOptions{Limits: &l})
			} else {
				store, err = OpenJournal(t.TempDir()+"/private", JournalOptions{Limits: &l})
			}
			if err != nil {
				t.Fatal(err)
			}
			var runs, aborts atomic.Int64
			def := taskDefinition(t, "task.cancel.write-budget", func(context.Context, TaskRecord, *TaskRuntime) error {
				runs.Add(1)
				return errors.New("cancelled pending phase dispatched")
			})
			options := def.options
			options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				aborts.Add(1)
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
				})
			}
			def, err = DefineTask(options)
			if err != nil {
				t.Fatal(err)
			}
			reports := make(chan error, 8)
			h := taskTestHarnessOptions(t, store, Options{OnReport: func(err error) {
				select {
				case reports <- err:
				default:
				}
			}}, def)
			parent := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			children := []ID{}
			for k := 0; k < 4; k++ {
				children = append(children, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: parent}}))
			}
			h.session.taskBookkeeping(func() { h.scheduler.cursor = children[0] }) // rotate past first marked child
			_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
				task := markTask(tx.state.Tasks[parent])
				return tx.stage(Write{Op: "put-task", Task: &task})
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := h.Resume(bg); err != nil {
				t.Fatal(err)
			}
			for _, id := range append(children, parent) {
				if record := waitPublicTask(t, h, id); record.State.Outcome.Status != "aborted" {
					t.Fatal(record)
				}
			}
			if runs.Load() != 0 || aborts.Load() != 5 {
				t.Fatal("bounded cascade dispatch", runs.Load(), aborts.Load())
			}
			select {
			case err := <-reports:
				t.Fatal("independent marks batched beyond persisted budget", err)
			default:
			}
		})
	}
}

func TestTaskSchedulerCascadeQueuedWithdrawalBudgetAtomicity(t *testing.T) {
	for _, maxWrites := range []int{1, 3} {
		for _, backendName := range []string{"memory", "journal"} {
			t.Run(fmt.Sprintf("writes%d/%s", maxWrites, backendName), func(t *testing.T) {
				l := DefaultLimits()
				l.MaxWrites = maxWrites
				l.MaxPage = 2
				var store Storage
				var err error
				if backendName == "memory" {
					store, err = OpenMemory(MemoryOptions{Limits: &l})
				} else {
					store, err = OpenJournal(t.TempDir()+"/private", JournalOptions{Limits: &l})
				}
				if err != nil {
					t.Fatal(err)
				}
				var effects atomic.Int64
				def := taskDefinition(t, "task.queue.budget.owner", func(context.Context, TaskRecord, *TaskRuntime) error {
					effects.Add(1)
					return errors.New("queued scope dispatched")
				})
				h := taskTestHarness(t, store, def)
				owner := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				var foreign, subID, generationID, inboxID, ownSubID, writeSubID ID
				_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
					var err error
					foreign, err = tx.MintID()
					if err != nil {
						return err
					}
					return tx.CreateConversation(Conversation{ID: foreign, Owner: owner})
				})
				if err != nil {
					t.Fatal(err)
				}
				_, err = h.CommitTasks(bg, foreign, func(tx *Tx) error {
					var err error
					subID, err = tx.MintID()
					if err != nil {
						return err
					}
					return tx.PutSubmission(Submission{ID: subID, Conversation: foreign, Type: "follow-up", Status: "pending", Value: JSON{"content": "queued"}})
				})
				if err != nil {
					t.Fatal(err)
				}
				_, err = h.CommitTasks(bg, foreign, func(tx *Tx) error {
					var err error
					generationID, err = tx.MintID()
					if err != nil {
						return err
					}
					cp, err := dtoObject(generationCheckpoint{Phase: "queued", Submission: subID, Input: "queued"}, tx.limits)
					if err != nil {
						return err
					}
					return tx.PutTask(Task{ID: generationID, Conversation: foreign, Kind: "pi.generation", Status: "pending", Checkpoint: cp})
				})
				if err != nil {
					t.Fatal(err)
				}
				_, err = h.CommitTasks(bg, foreign, func(tx *Tx) error {
					var err error
					inboxID, err = tx.MintID()
					if err != nil {
						return err
					}
					_, err = tx.CreateDocument(Document{ID: inboxID, Scope: "conversation", Owner: foreign, Kind: "pi.inbox", Version: 1, Value: JSON{"items": []any{JSON{"id": subID, "mode": "followUp", "input": JSON{"content": "queued"}}}}})
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				for _, scope := range []struct {
					conversation ID
					mode         string
					dest         *ID
				}{{1, "follow-up", &ownSubID}, {foreign, "write", &writeSubID}} {
					_, err = h.CommitTasks(bg, scope.conversation, func(tx *Tx) error {
						id, err := tx.MintID()
						if err != nil {
							return err
						}
						*scope.dest = id
						status := "pending"
						if scope.mode == "write" {
							status = "done"
						}
						return tx.PutSubmission(Submission{ID: id, Conversation: scope.conversation, Type: scope.mode, Status: status, Value: JSON{"content": "preserved"}})
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
					task := markTask(tx.state.Tasks[owner])
					return tx.stage(Write{Op: "put-task", Task: &task})
				})
				if err != nil {
					t.Fatal(err)
				}
				before, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				err = h.scheduler.reconcile()
				if maxWrites == 1 {
					var rejection *StorageRejected
					if !errors.As(err, &rejection) {
						t.Fatal("indivisible3write withdrawal budget accepted", err)
					}
				} else if err != nil {
					t.Fatal("one valid withdrawal aggregated/rejected", err)
				}
				after, readErr := h.Snapshot(bg)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if maxWrites == 1 {
					if after.Seq != before.Seq || after.Tasks[generationID].Status != "pending" || taskAborted(after.Tasks[generationID]) || after.Submissions[subID].Status != "pending" || !equalJSONValue(after.Documents[inboxID].Value, before.Documents[inboxID].Value) {
						t.Fatal("indivisible withdrawal partially admitted")
					}
				} else {
					if after.Tasks[generationID].Status != "aborted" || after.Submissions[subID].Status != "aborted" || len(after.Documents[inboxID].Value["items"].([]any)) != 0 {
						t.Fatal("queued withdrawal atomic unit missing")
					}
				}
				if after.Submissions[ownSubID].Status != "pending" || after.Submissions[writeSubID].Status != "done" || effects.Load() != 0 {
					t.Fatal("cascade withdrew own/write scope or invoked effects")
				}
			})
		}
	}
}

func TestTaskSchedulerPendingDedupCloseSealDoesNotWake(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, releaseHost, blockerEntered, releaseBlocker := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
		var captured *TaskRuntime
		var foreign ID
		caller := taskDefinition(t, "task.dedup.close", func(_ context.Context, _ TaskRecord, r *TaskRuntime) error {
			captured = r
			close(entered)
			<-releaseHost
			return nil
		})
		h := taskTestHarness(t, b.store, caller)
		cleanupTaskGates(t, releaseHost, releaseBlocker)
		_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			foreign, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.CreateConversation(Conversation{ID: foreign})
		})
		if err != nil {
			t.Fatal(err)
		}
		createPublicTask(t, h, caller, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(false) })
		var subID ID
		_, err = h.CommitTasks(bg, foreign, func(tx *Tx) error {
			var err error
			subID, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.PutSubmission(Submission{ID: subID, Conversation: foreign, RequestID: "close-dedup", Type: "follow-up", Status: "pending", Value: JSON{}})
		})
		if err != nil {
			t.Fatal(err)
		}
		handle, _ := h.Conversation(bg, foreign)
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		blocked := make(chan error, 1)
		go func() {
			_, err := h.session.taskCommit(bg, func(*Tx) error { close(blockerEntered); <-releaseBlocker; return nil })
			blocked <- err
		}()
		awaitTaskSignal(t, blockerEntered)
		dedup := make(chan error, 1)
		go func() {
			_, err := handle.submitAdmission(bg, Input{Type: "follow-up", RequestID: "close-dedup"}, captured)
			dedup <- err
		}()
		closing := make(chan error, 1)
		go func() { closing <- h.Close(bg) }()
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for !h.closing.Load() {
			select {
			case <-ctx.Done():
				t.Fatal("Close seal missing")
			default:
				runtime.Gosched()
			}
		}
		epoch := h.scheduler.epoch.Load()
		releaseTaskGate(releaseBlocker)
		select {
		case err := <-blocked:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("held line missing")
		}
		select {
		case err := <-dedup:
			if !errors.Is(err, ErrClosed) && !errors.Is(err, ErrSealed) {
				t.Fatal("pending dedup accepted after Close seal", err)
			}
		case <-ctx.Done():
			t.Fatal("dedup admission missing")
		}
		after, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if after.Seq != before.Seq || h.scheduler.epoch.Load() != epoch || h.scheduler.enabled.Load() || after.Submissions[subID].Status != "pending" {
			t.Fatal("Close dedup enabled/published")
		}
		releaseTaskGate(releaseHost)
		select {
		case err := <-closing:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("Close join missing")
		}
	})
}

func TestTaskSchedulerFailFastPrefixCursorCannotDispatchUnmarkedSibling(t *testing.T) {
	for _, backendName := range []string{"memory", "journal"} {
		t.Run(backendName, func(t *testing.T) {
			l := DefaultLimits()
			l.MaxWrites = 1
			l.MaxPage = 2
			var store Storage
			var err error
			if backendName == "memory" {
				store, err = OpenMemory(MemoryOptions{Limits: &l})
			} else {
				store, err = OpenJournal(t.TempDir()+"/private", JournalOptions{Limits: &l})
			}
			if err != nil {
				t.Fatal(err)
			}
			var siblingRuns, aborts atomic.Int64
			sibling := taskDefinition(t, "task.failfast.budget.sibling", func(context.Context, TaskRecord, *TaskRuntime) error {
				siblingRuns.Add(1)
				return errors.New("unmarked failFast sibling dispatched")
			})
			options := sibling.options
			options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				aborts.Add(1)
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
				})
			}
			sibling, err = DefineTask(options)
			if err != nil {
				t.Fatal(err)
			}
			parentDef := taskDefinition(t, "task.failfast.budget.parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("joined"), nil })
			})
			h := taskTestHarness(t, store, parentDef, sibling)
			parent := createPublicTask(t, h, parentDef, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			members := []ID{}
			for k := 0; k < 5; k++ {
				members = append(members, createPublicTask(t, h, sibling, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: parent}}))
			}
			_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
				task, err := copyTask(tx.state.Tasks[members[0]], tx.limits)
				if err != nil {
					return err
				}
				task.Status = "failed"
				task.Execution.Native.State = TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "failed member"}}}
				return tx.stage(Write{Op: "put-task", Task: &task})
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
				task, err := copyTask(tx.state.Tasks[parent], tx.limits)
				if err != nil {
					return err
				}
				task.Status = "waiting"
				task.Execution.Native.State = TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: members, Policy: "failFast"}
				return tx.stage(Write{Op: "put-task", Task: &task})
			})
			if err != nil {
				t.Fatal(err)
			}
			h.session.taskBookkeeping(func() { h.scheduler.cursor = members[1] })
			if err := h.Resume(bg); err != nil {
				t.Fatal(err)
			}
			for _, id := range members[1:] {
				record := waitPublicTask(t, h, id)
				if record.State.Outcome.Status != "aborted" {
					t.Fatal(record)
				}
			}
			if record := waitPublicTask(t, h, parent); record.State.Outcome.Status != "completed" || record.AbortRequested {
				t.Fatal("failFast parent changed", record)
			}
			if siblingRuns.Load() != 0 || aborts.Load() != 4 {
				t.Fatal("cursor skipped early failFast cancellation", siblingRuns.Load(), aborts.Load())
			}
		})
	}
}

func TestTaskSchedulerActiveProgressRespectsPendingCascadePrefix(t *testing.T) {
	for _, intent := range []string{"ancestor-mark", "failFast"} {
		t.Run(intent, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				entered, releasePhase := make(chan struct{}), make(chan struct{})
				var extraEffects, aborts atomic.Int64
				var captured *TaskRuntime
				active, err := DefineTask(TaskDefinitionOptions{Kind: "task.cascade.active", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "first"}, nil }, Phases: map[string]TaskPhase{
					"first": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
						captured = r
						close(entered)
						<-releasePhase
						return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
							return &TaskState{Status: "running", Checkpoint: JSON{"phase": "forbidden-next"}}, nil
						})
					},
					"forbidden-next": func(context.Context, TaskRecord, *TaskRuntime) error {
						extraEffects.Add(1)
						return errors.New("phase dispatched after confirmed cascade intent")
					},
				}, Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					aborts.Add(1)
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
					})
				}})
				if err != nil {
					t.Fatal(err)
				}
				parentDef := taskDefinition(t, "task.cascade.active.parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				h := taskTestHarness(t, b.store, active, parentDef)
				cleanupTaskGates(t, releasePhase)
				parent := createPublicTask(t, h, parentDef, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				child := createPublicTask(t, h, active, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: parent}})
				failed := createPublicTask(t, h, parentDef, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: parent}})
				_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
					task, err := copyTask(tx.state.Tasks[parent], tx.limits)
					if err != nil {
						return err
					}
					task.Status = "waiting"
					task.Execution.Native.State = TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: []ID{child, failed}, Policy: "allSettled"}
					return tx.stage(Write{Op: "put-task", Task: &task})
				})
				if err != nil {
					t.Fatal(err)
				}
				// Keep the control member pending/blocked while the actual child
				// phase enters. Its eventual failed adoption is the failFast intent.
				h.session.taskBookkeeping(func() { h.scheduler.retryAfter[failed] = ^uint64(0) })
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, entered)
				h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(false) })
				_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
					if intent == "ancestor-mark" {
						task := markTask(tx.state.Tasks[parent])
						return tx.stage(Write{Op: "put-task", Task: &task})
					}
					task, err := copyTask(tx.state.Tasks[failed], tx.limits)
					if err != nil {
						return err
					}
					task.Status = "failed"
					task.Execution.Native.State = TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "confirmed failure"}}}
					if err := tx.stage(Write{Op: "put-task", Task: &task}); err != nil {
						return err
					}
					p, err := copyTask(tx.state.Tasks[parent], tx.limits)
					if err != nil {
						return err
					}
					p.Execution.Native.State.Policy = "failFast"
					return tx.stage(Write{Op: "put-task", Task: &p})
				})
				if err != nil {
					t.Fatal(err)
				}
				releaseTaskGate(releasePhase)
				awaitTaskSignal(t, captured.done)
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				if taskAborted(state.Tasks[child]) || state.Tasks[child].Status != "running" || extraEffects.Load() != 0 {
					t.Fatal("next phase ran before bounded own mark", state.Tasks[child], extraEffects.Load())
				}
				h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(true) })
				h.scheduler.kick()
				if record := waitPublicTask(t, h, child); record.State.Outcome.Status != "aborted" {
					t.Fatal(record)
				}
				if extraEffects.Load() != 0 || aborts.Load() != 1 {
					t.Fatal("active cascade dispatch", extraEffects.Load(), aborts.Load())
				}
			})
		})
	}
}

func TestTaskSchedulerQueuedWithdrawalTaskDocumentsAtomicRetirement(t *testing.T) {
	for _, mode := range []string{"explicit", "cascade"} {
		for _, budget := range []int{3, 4} {
			for _, backendName := range []string{"memory", "journal"} {
				t.Run(fmt.Sprintf("%s/writes%d/%s", mode, budget, backendName), func(t *testing.T) {
					l := DefaultLimits()
					l.MaxWrites = budget
					var store Storage
					var err error
					if backendName == "memory" {
						store, err = OpenMemory(MemoryOptions{Limits: &l})
					} else {
						store, err = OpenJournal(t.TempDir()+"/private", JournalOptions{Limits: &l})
					}
					if err != nil {
						t.Fatal(err)
					}
					h := taskTestHarness(t, store)
					var owner, conversation, subID, generation, docID ID
					_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
						var err error
						owner, err = tx.MintID()
						if err != nil {
							return err
						}
						task := foundationTask(owner, nil)
						return tx.stage(Write{Op: "put-task", Task: &task})
					})
					if err != nil {
						t.Fatal(err)
					}
					_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
						var err error
						conversation, err = tx.MintID()
						if err != nil {
							return err
						}
						return tx.CreateConversation(Conversation{ID: conversation, Owner: owner})
					})
					if err != nil {
						t.Fatal(err)
					}
					_, err = h.CommitTasks(bg, conversation, func(tx *Tx) error {
						if err := initializeBuiltins(tx, conversation); err != nil {
							return err
						}
						return nil
					})
					if err != nil {
						t.Fatal(err)
					}
					_, err = h.CommitTasks(bg, conversation, func(tx *Tx) error {
						var err error
						subID, err = tx.MintID()
						if err != nil {
							return err
						}
						generation, err = tx.MintID()
						if err != nil {
							return err
						}
						if err := tx.PutSubmission(Submission{ID: subID, Conversation: conversation, Type: "follow-up", Status: "pending", Value: JSON{}}); err != nil {
							return err
						}
						cp, err := dtoObject(generationCheckpoint{Phase: "queued", Submission: subID}, tx.limits)
						if err != nil {
							return err
						}
						if err := tx.PutTask(Task{ID: generation, Conversation: conversation, Kind: "pi.generation", Status: "pending", Checkpoint: cp}); err != nil {
							return err
						}
						inbox, err := builtin(tx, conversation, "pi.inbox")
						if err != nil {
							return err
						}
						return inbox.Set(JSON{"items": []any{JSON{"id": subID, "mode": "followUp", "input": JSON{}}}})
					})
					if err != nil {
						t.Fatal(err)
					}
					_, err = h.CommitTasks(bg, conversation, func(tx *Tx) error {
						var err error
						docID, err = tx.MintID()
						if err != nil {
							return err
						}
						_, err = tx.CreateDocument(Document{ID: docID, Scope: "task", Owner: generation, Kind: "app.withdraw-doc", Version: 1, Value: JSON{"retained": true}})
						return err
					})
					if err != nil {
						t.Fatal(err)
					}
					if mode == "cascade" {
						_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
							task := markTask(tx.state.Tasks[owner])
							return tx.stage(Write{Op: "put-task", Task: &task})
						})
						if err != nil {
							t.Fatal(err)
						}
					}
					before, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					if mode == "explicit" {
						err = (&SubmissionHandle{h: h, id: subID}).Withdraw(bg)
					} else {
						err = h.scheduler.reconcile()
					}
					after, readErr := h.Snapshot(bg)
					if readErr != nil {
						t.Fatal(readErr)
					}
					if budget == 3 {
						var rejectErr *StorageRejected
						if !errors.As(err, &rejectErr) || after.Seq != before.Seq || after.Tasks[generation].Status != "pending" || after.Submissions[subID].Status != "pending" || after.Documents[docID].Retired {
							t.Fatal("task-doc withdrawal exceeded atomic budget/leaked", err)
						}
					} else {
						if err != nil || after.Tasks[generation].Status != "aborted" || after.Submissions[subID].Status != "aborted" || !after.Documents[docID].Retired {
							t.Fatal("queued native taskdoc not atomically retired", err)
						}
						if after.Tasks[generation].Execution != nil {
							t.Fatal("withdrawal fabricated legacy execution format")
						}
					}
					if len(after.Entries) != 0 {
						t.Fatal("withdrawal fabricated receipt/effect")
					}
				})
			}
		}
	}
}

func TestTaskSchedulerRuntimeCommittedReadsClockReportsAndWatchLifetime(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		notes, err := DefineDocument(DefinitionOptions{Kind: "app.runtime-notes", Scope: "conversation", Version: 1, History: "rewindable", Fork: "asOf", Initial: func(JSON) (JSON, error) { return JSON{"text": ""}, nil }})
		if err != nil {
			t.Fatal(err)
		}
		var firstEntry, secondEntry ID
		var captured *TaskRuntime
		var watch *DocumentWatch
		var seen []any
		reported := errors.New("bounded host report")
		reports := make(chan error, 2)
		def := taskDefinition(t, "task.runtime.reads", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			captured = r
			current, ok, err := r.SnapshotDefinition(ctx, notes, 1, nil)
			if err != nil || !ok {
				return errors.New("runtime committed notes missing")
			}
			seen = append(seen, current["text"])
			asOf, ok, err := r.SnapshotDefinitionAsOf(ctx, notes, 1, nil, firstEntry)
			if err != nil || !ok {
				return errors.New("runtime historical notes missing")
			}
			seen = append(seen, asOf["text"])
			cut, err := r.ContextView(ctx, 1, firstEntry)
			if err != nil {
				return err
			}
			seen = append(seen, len(cut.Entries))
			full, err := r.ContextView(ctx, 1, secondEntry)
			if err != nil {
				return err
			}
			seen = append(seen, len(full.Messages))
			now, err := r.Now()
			if err != nil {
				return err
			}
			seen = append(seen, now)
			if err := r.Report(reported); err != nil {
				return err
			}
			watch, err = r.WatchDefinition(ctx, notes, 1, nil)
			if err != nil || watch == nil {
				return errors.New("runtime watch missing")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("read"), nil })
		})
		h := taskTestHarnessOptions(t, b.store, Options{Now: func() int64 { return 1234 }, OnReport: func(err error) { reports <- err }}, def)
		for index, text := range []string{"one", "two"} {
			_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
				doc, err := tx.AcquireDocument(notes, 1, nil, nil)
				if err != nil {
					return err
				}
				if err := doc.Set(JSON{"text": text}); err != nil {
					return err
				}
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				if index == 0 {
					firstEntry = id
				} else {
					secondEntry = id
				}
				value, err := dtoObject(userReceipt(text), tx.limits)
				if err != nil {
					return err
				}
				return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "message", Value: value})
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		waitPublicTask(t, h, id)
		if len(seen) != 5 || seen[0] != "two" || seen[1] != "one" || seen[2] != 1 || seen[3] != 2 || seen[4] != int64(1234) {
			t.Fatal("runtime committed reads/clock", seen)
		}
		select {
		case err := <-reports:
			if err != reported {
				t.Fatal(err)
			}
		default:
			t.Fatal("runtime report not forwarded")
		}
		awaitTaskSignal(t, watch.Closed())
		if end, ok := watch.End(); !ok || end.Reason != "stopped" {
			t.Fatal("invocation watch not stopped", end)
		}
		if _, _, err := captured.SnapshotDefinition(bg, notes, 1, nil); !errors.Is(err, ErrSealed) {
			t.Fatal("ended snapshot", err)
		}
		if _, err := captured.ContextView(bg, 1, 0); !errors.Is(err, ErrSealed) {
			t.Fatal("ended context", err)
		}
		if _, err := captured.Now(); !errors.Is(err, ErrSealed) {
			t.Fatal("ended clock", err)
		}
		if err := captured.Report(reported); !errors.Is(err, ErrSealed) {
			t.Fatal("ended report", err)
		}
	})
}

func TestTaskSchedulerSleepClockAndCancellation(t *testing.T) {
	for _, mode := range []string{"clock", "caller-cancel", "invocation-mark"} {
		t.Run(mode, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				entered := make(chan struct{})
				var clock atomic.Int64
				clock.Store(1000)
				var reads atomic.Int64
				var cancelled atomic.Bool
				def := taskDefinition(t, "task.sleep.control", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					if err := r.SleepUntil(ctx, 900); err != nil {
						return err
					}
					if mode == "caller-cancel" {
						caller, cancel := context.WithCancel(ctx)
						cancel()
						if err := r.SleepUntil(caller, 60000); !errors.Is(err, context.Canceled) {
							return errors.New("sleep caller cancellation lost")
						}
						cancelled.Store(true)
						return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
					}
					close(entered)
					deadline := int64(1005)
					if mode == "invocation-mark" {
						deadline = 60000
					}
					err := r.SleepUntil(ctx, deadline)
					if mode == "invocation-mark" {
						if errors.Is(err, context.Canceled) || errors.Is(err, ErrSealed) {
							cancelled.Store(true)
						}
						return err
					}
					if err != nil {
						return err
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				h := taskTestHarnessOptions(t, b.store, Options{Now: func() int64 { reads.Add(1); return clock.Load() }}, def)
				id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				if mode == "clock" {
					awaitTaskSignal(t, entered)
					ctx, cancel := context.WithTimeout(bg, 3*time.Second)
					defer cancel()
					for reads.Load() < 4 {
						select {
						case <-ctx.Done():
							t.Fatal("sleep never rechecked stationary Harness clock")
						default:
							runtime.Gosched()
						}
					}
					record, _, err := h.Task(bg, id)
					if err != nil || record.State.Status != "running" {
						t.Fatal("stationary clock sleep completed", record, err)
					}
					clock.Store(1005)
				}
				if mode == "invocation-mark" {
					awaitTaskSignal(t, entered)
					if _, err := h.AbortTask(bg, id); err != nil {
						t.Fatal(err)
					}
				}
				record := waitPublicTask(t, h, id)
				want := "completed"
				if mode == "invocation-mark" {
					want = "aborted"
				}
				if record.State.Outcome.Status != want {
					t.Fatal(record)
				}
				if mode != "clock" && !cancelled.Load() {
					t.Fatal("sleep cancellation not witnessed")
				}
			})
		})
	}
}

func TestTaskSchedulerCheckpointValueProgressPolicy(t *testing.T) {
	for _, variant := range []string{"key-order-number", "array-equal", "document-only", "array-changed", "throw-after-progress"} {
		t.Run(variant, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var calls atomic.Int64
				initial := JSON{"phase": "work", "n": json.Number("1.0"), "items": []any{1, JSON{"nested": true}}}
				def, err := DefineTask(TaskDefinitionOptions{Kind: "task.checkpoint.value", Version: 1, Initial: func(any) (JSON, error) { return initial, nil }, Phases: map[string]TaskPhase{"work": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
					if calls.Add(1) > 1 {
						return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("changed"), nil })
					}
					err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
						if variant == "document-only" {
							definition, err := DefineDocument(DefinitionOptions{Kind: "app.progress", Scope: "conversation", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{}, nil }})
							if err != nil {
								return nil, err
							}
							doc, err := tx.AcquireDocument(definition, 1, nil, nil)
							if err != nil {
								return nil, err
							}
							return nil, doc.Set(JSON{"changed": true})
						}
						next := JSON{"items": []any{json.Number("1"), JSON{"nested": true}}, "n": json.Number("1e0"), "phase": "work"}
						if variant == "array-changed" || variant == "throw-after-progress" {
							next["items"] = []any{2, JSON{"nested": true}}
						}
						return &TaskState{Status: "running", Checkpoint: next}, nil
					})
					if err != nil {
						return err
					}
					if variant == "throw-after-progress" {
						return errors.New("error wins over progress")
					}
					return nil
				}}, Abort: taskAbort})
				if err != nil {
					t.Fatal(err)
				}
				h := taskTestHarness(t, b.store, def)
				id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				record := waitPublicTask(t, h, id)
				want := "faulted"
				wantCalls := int64(1)
				if variant == "array-changed" {
					want = "completed"
					wantCalls = 2
				}
				if record.State.Outcome.Status != want || calls.Load() != wantCalls {
					t.Fatal("checkpoint progress contract", variant, record, calls.Load())
				}
			})
		})
	}
}

func TestTaskSchedulerPhaseRegistrySnapshotPinRefresh(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		var h *Harness
		var firstSeen, secondSeen bool
		later := taskDefinition(t, "task.registry.later", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		def, err := DefineTask(TaskDefinitionOptions{Kind: "task.registry.phase", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "a"}, nil }, Phases: map[string]TaskPhase{
			"a": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				snapshot, err := r.Registry()
				if err != nil {
					return err
				}
				if snapshot.Task(later.Kind()) != nil {
					return errors.New("unexpected later definition")
				}
				close(entered)
				<-release
				same, err := r.Registry()
				if err != nil {
					return err
				}
				firstSeen = same.Task(later.Kind()) == nil
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "running", Checkpoint: JSON{"phase": "b"}}, nil
				})
			},
			"b": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				snapshot, err := r.Registry()
				if err != nil {
					return err
				}
				secondSeen = snapshot.Task(later.Kind()) == later
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			},
		}, Abort: taskAbort})
		if err != nil {
			t.Fatal(err)
		}
		h = taskTestHarness(t, b.store, def)
		cleanupTaskGates(t, release)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		if _, err := h.options.Registry.RegisterTask(later); err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(release)
		waitPublicTask(t, h, id)
		if !firstSeen || !secondSeen {
			t.Fatal("phase registry was not pinned/refreshed", firstSeen, secondSeen)
		}
	})
}

func TestTaskSchedulerWaitUnknownTerminalCancelAndClose(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		var captured *TaskRuntime
		def := taskDefinition(t, "task.wait.public", func(_ context.Context, _ TaskRecord, r *TaskRuntime) error {
			captured = r
			close(entered)
			<-release
			return nil
		})
		h := taskTestHarness(t, b.store, def)
		cleanupTaskGates(t, release)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if _, err := h.WaitForTask(bg, ID(MaxID)); err == nil {
			t.Fatal("unknown task wait accepted")
		}
		awaitTaskSignal(t, entered)
		ctx, cancel := context.WithCancel(bg)
		waiting := make(chan error, 1)
		go func() { _, err := h.WaitForTask(ctx, id); waiting <- err }()
		ack, stop := context.WithTimeout(bg, 3*time.Second)
		defer stop()
		for {
			registered := false
			h.session.taskBookkeeping(func() {
				for wait := range h.scheduler.waiters {
					if wait.binding == nil && wait.target == id {
						registered = true
					}
				}
			})
			if registered {
				break
			}
			select {
			case <-ack.Done():
				t.Fatal("actual public wait registration missing")
			default:
				runtime.Gosched()
			}
		}
		cancel()
		select {
		case err := <-waiting:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("wait caller cancellation", err)
			}
		case <-ack.Done():
			t.Fatal("cancelled wait not detached")
		}
		state, err := h.Snapshot(bg)
		if err != nil || state.Tasks[id].Status != "running" {
			t.Fatal("wait cancellation cancelled durable work", err)
		}
		terminalDef := taskDefinition(t, "task.wait.alreadyterminal", func(context.Context, TaskRecord, *TaskRuntime) error { return errors.New("terminal task executed") })
		terminalID := createPublicTask(t, h, terminalDef, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			task, err := copyTask(tx.state.Tasks[terminalID], tx.limits)
			if err != nil {
				return err
			}
			task.Status = "done"
			task.Execution.Native.State = *taskDone("terminal receipt")
			return tx.stage(Write{Op: "put-task", Task: &task})
		})
		if err != nil {
			t.Fatal(err)
		}
		if record := waitPublicTask(t, h, terminalID); record.State.Outcome.Result.Value != "terminal receipt" {
			t.Fatal("already terminal wait", record)
		}
		pending := make(chan error, 1)
		go func() { _, err := h.WaitForTask(bg, id); pending <- err }()
		for {
			registered := false
			h.session.taskBookkeeping(func() {
				for wait := range h.scheduler.waiters {
					if wait.binding == nil && wait.target == id {
						registered = true
					}
				}
			})
			if registered {
				break
			}
			select {
			case <-ack.Done():
				t.Fatal("pending close wait missing")
			default:
				runtime.Gosched()
			}
		}
		closing := make(chan error, 1)
		go func() { closing <- h.Close(bg) }()
		for !h.closing.Load() {
			select {
			case <-ack.Done():
				t.Fatal("Close seal missing")
			default:
				runtime.Gosched()
			}
		}
		// Close waits for the actual stubborn host; the registered reader must
		// be rejected before that host is allowed to return.
		select {
		case err := <-pending:
			if !errors.Is(err, ErrClosed) {
				t.Fatal("close wait", err)
			}
		case <-ack.Done():
			t.Fatal("Close waiter rejection missing")
		}
		releaseTaskGate(release)
		select {
		case err := <-closing:
			if err != nil {
				t.Fatal(err)
			}
		case <-ack.Done():
			t.Fatal("Close join missing")
		}
		_ = captured
	})
}

func TestTaskSchedulerSharedTicketCancellationBeforeAfterAdoption(t *testing.T) {
	for _, cancelAt := range []string{"before-adoption", "after-adoption"} {
		t.Run(cancelAt, func(t *testing.T) {
			taskLimitedBackends(t, 3, func(t *testing.T, store Storage) {
				aEntered, bEntered, letA, letB, aCancelled, releaseA, cEntered, releaseC := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
				var aID, bID, cID ID
				var cRuns atomic.Int64
				waitContext, cancelWait := context.WithCancel(bg)
				defer cancelWait()
				a := taskDefinition(t, "task.ticket.cancel.a", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					close(aEntered)
					<-letA
					if _, err := r.WaitForTask(waitContext, cID); !errors.Is(err, context.Canceled) {
						return fmt.Errorf("cancelled shared wait: %w", err)
					}
					close(aCancelled)
					<-releaseA
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("a"), nil })
				})
				b := taskDefinition(t, "task.ticket.cancel.b", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					close(bEntered)
					<-letB
					if _, err := r.WaitForTask(ctx, cID); err != nil {
						return err
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("b"), nil })
				})
				c := taskDefinition(t, "task.ticket.cancel.target", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					cRuns.Add(1)
					close(cEntered)
					<-releaseC
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("c"), nil })
				})
				h := taskTestHarness(t, store, a, b, c)
				cleanupTaskGates(t, letA, letB, releaseA, releaseC)
				aID = createPublicTask(t, h, a, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				bID = createPublicTask(t, h, b, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, aEntered)
				awaitTaskSignal(t, bEntered)
				h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(false) })
				cID = createPublicTask(t, h, c, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				releaseTaskGate(letA)
				releaseTaskGate(letB)
				ctx, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				for {
					claimants := 0
					h.session.taskBookkeeping(func() { claimants = len(h.scheduler.tickets[cID]) })
					if claimants == 2 {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal("two actual shared ticket claims missing")
					default:
						runtime.Gosched()
					}
				}
				if cancelAt == "after-adoption" {
					h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(true) })
					h.scheduler.kick()
					awaitTaskSignal(t, cEntered)
				}
				cancelWait()
				awaitTaskSignal(t, aCancelled)
				h.session.taskBookkeeping(func() {
					for wait := range h.scheduler.waiters {
						if wait.binding != nil && wait.binding.taskID == aID {
							t.Error("cancelled caller retained registration")
						}
					}
					if cancelAt == "before-adoption" {
						if len(h.scheduler.tickets[cID]) != 1 {
							t.Error("one cancellation removed healthy shared claim")
						}
						for wait := range h.scheduler.tickets[cID] {
							if wait.binding.taskID != bID {
								t.Error("wrong remaining shared claimant")
							}
						}
					} else {
						if h.scheduler.invocations[cID] == nil || len(h.scheduler.tickets) != 0 {
							t.Error("cancelled caller altered adopted reservation")
						}
					}
				})
				if cancelAt == "before-adoption" {
					h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(true) })
					h.scheduler.kick()
					awaitTaskSignal(t, cEntered)
				}
				if record, _, err := h.Task(bg, cID); err != nil || record.AbortRequested || record.State.Status != "running" {
					t.Fatal("wait cancellation marked durable target", record, err)
				}
				releaseTaskGate(releaseC)
				waitPublicTask(t, h, bID)
				releaseTaskGate(releaseA)
				waitPublicTask(t, h, aID)
				if cRuns.Load() != 1 {
					t.Fatal("shared target executed twice", cRuns.Load())
				}
			})
		})
	}
}

func TestTaskSchedulerMultiFrontierFailureReleasesOnlyFailedRegistration(t *testing.T) {
	taskLimitedBackends(t, 4, func(t *testing.T, store Storage) {
		aEntered, bEntered, letA, letB, aRejected, releaseA, dEntered, releaseD := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
		var h *Harness
		var foreign, cID, dID, aID, bID ID
		var cRuns, dRuns atomic.Int64
		a := taskDefinition(t, "task.frontier.failure.a", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(aEntered)
			<-letA
			conversation, err := r.Conversation(ctx, foreign)
			if err != nil {
				return err
			}
			err = h.waitIdleAdmission(ctx, conversation.ID(), r)
			var yield *InvocationWaitRequiresYield
			if !errors.As(err, &yield) {
				return fmt.Errorf("failed multi-frontier admission: %w", err)
			}
			close(aRejected)
			<-releaseA
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("a"), nil })
		})
		b := taskDefinition(t, "task.frontier.failure.b", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(bEntered)
			<-letB
			if _, err := r.WaitForTask(ctx, dID); err != nil {
				return err
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("b"), nil })
		})
		c := taskDefinition(t, "task.frontier.failure.c", func(context.Context, TaskRecord, *TaskRuntime) error {
			cRuns.Add(1)
			return errors.New("missing definition target dispatched")
		})
		d := taskDefinition(t, "task.frontier.failure.d", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			dRuns.Add(1)
			close(dEntered)
			<-releaseD
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("d"), nil })
		})
		h = taskTestHarness(t, store, a, b, d)
		cleanupTaskGates(t, letA, letB, releaseA, releaseD)
		disposeC, err := h.options.Registry.RegisterTask(c)
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			foreign, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.CreateConversation(Conversation{ID: foreign})
		})
		if err != nil {
			t.Fatal(err)
		}
		aID = createPublicTask(t, h, a, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		bID = createPublicTask(t, h, b, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, aEntered)
		awaitTaskSignal(t, bEntered)
		h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(false) })
		_, err = h.CommitTasks(bg, foreign, func(tx *Tx) error {
			var err error
			cID, err = tx.CreateTask(c, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			if err != nil {
				return err
			}
			dID, err = tx.CreateTask(d, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(letA)
		releaseTaskGate(letB)
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for {
			claimsC, claimsD := 0, 0
			h.session.taskBookkeeping(func() { claimsC = len(h.scheduler.tickets[cID]); claimsD = len(h.scheduler.tickets[dID]) })
			if claimsC == 1 && claimsD == 2 {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("multi-frontier + shared actual claims missing")
			default:
				runtime.Gosched()
			}
		}
		disposeC()
		// Resolve-blocked C rejects A's WHOLE frontier. D keeps B's healthy
		// shared claim and its actual adopted reservation; no cancellation.
		reservations, err := h.scheduler.reservePass(false)
		if err != nil || len(reservations) != 1 || reservations[0].taskID != dID {
			discardUnstartedTaskReservations(t, h, reservations)
			t.Fatal("healthy shared target lost after C resolution failure", err)
		}
		runtimeD := reservations[0]
		started := false
		t.Cleanup(func() {
			if !started {
				discardUnstartedTaskReservations(t, h, []*TaskRuntime{runtimeD})
			}
		})
		awaitTaskSignal(t, aRejected)
		h.session.taskBookkeeping(func() {
			if len(h.scheduler.tickets) != 0 {
				t.Error("failed registration's unadopted claims survived")
			}
			for wait := range h.scheduler.waiters {
				if wait.binding != nil && wait.binding.taskID == aID {
					t.Error("failed multi-frontier registration survived")
				}
			}
			foundB := false
			for wait := range h.scheduler.waiters {
				if wait.binding != nil && wait.binding.taskID == bID {
					foundB = true
				}
			}
			if !foundB {
				t.Error("healthy shared waiter detached")
			}
		})
		started = true
		go h.scheduler.run(runtimeD)
		awaitTaskSignal(t, dEntered)
		releaseTaskGate(releaseD)
		waitPublicTask(t, h, bID)
		releaseTaskGate(releaseA)
		waitPublicTask(t, h, aID)
		if cRuns.Load() != 0 || dRuns.Load() != 1 {
			t.Fatal("frontier resolution effects", cRuns.Load(), dRuns.Load())
		}
	})
}

func TestTaskSchedulerLateWatchAcquisitionAttachmentSealsAndStops(t *testing.T) {
	for _, seal := range []string{"terminal", "close"} {
		t.Run(seal, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				entered, releaseHost := make(chan struct{}), make(chan struct{})
				var captured *TaskRuntime
				def := taskDefinition(t, "task.late.watch", func(context.Context, TaskRecord, *TaskRuntime) error { return errors.New("replaced phase") })
				options := def.options
				options.Phases = map[string]TaskPhase{"work": func(_ context.Context, _ TaskRecord, r *TaskRuntime) error {
					captured = r
					close(entered)
					<-releaseHost
					return nil
				}}
				var err error
				def, err = DefineTask(options)
				if err != nil {
					t.Fatal(err)
				}
				h := taskTestHarness(t, b.store, def)
				cleanupTaskGates(t, releaseHost)
				notes, err := DefineDocument(DefinitionOptions{Kind: "app.late-watch", Scope: "session", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"value": "owned"}, nil }})
				if err != nil {
					t.Fatal(err)
				}
				_, err = h.CommitTasks(bg, 1, func(tx *Tx) error { _, err := tx.AcquireDocument(notes, 0, nil, nil); return err })
				if err != nil {
					t.Fatal(err)
				}
				id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, entered)
				// Public Session acquisition completes before the invocation-owned
				// attach. Its exact shared helper is queued behind adopted seal.
				acquired, ok, err := h.session.WatchDefinition(bg, notes, 0, nil)
				if err != nil || !ok {
					t.Fatal(err)
				}
				result := make(chan error, 1)
				var closing chan error
				if seal == "terminal" {
					err = captured.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) {
						go func() { _, err := captured.attachWatch(bg, acquired); result <- err }()
						return taskDone(nil), nil
					})
					if err != nil {
						t.Fatal(err)
					}
				} else {
					_, err = h.session.taskCommit(bg, func(*Tx) error {
						go func() { _, err := captured.attachWatch(bg, acquired); result <- err }()
						closing = make(chan error, 1)
						go func() { closing <- h.Close(bg) }()
						ctx, cancel := context.WithTimeout(bg, 3*time.Second)
						defer cancel()
						for !h.closing.Load() {
							select {
							case <-ctx.Done():
								return ctx.Err()
							default:
								runtime.Gosched()
							}
						}
						return nil
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				select {
				case err := <-result:
					if seal == "terminal" && !errors.Is(err, ErrSealed) {
						t.Fatal("late watch survived invocation seal", err)
					}
					if seal == "close" && !errors.Is(err, ErrClosed) && !errors.Is(err, ErrSealed) {
						t.Fatal("late watch survived Close", err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("late attachment admission missing")
				}
				awaitTaskSignal(t, acquired.Closed())
				h.session.observerMu.Lock()
				remaining := len(h.session.watches)
				h.session.observerMu.Unlock()
				if remaining != 0 {
					t.Fatal("late acquired watch retained registration", remaining)
				}
				if seal == "terminal" {
					if err := acquired.Start(func(context.Context, JSON, []Operation) error { return errors.New("closed callback dispatched") }); !errors.Is(err, ErrClosed) {
						t.Fatal("closed late watch listener accepted", err)
					}
				}
				releaseTaskGate(releaseHost)
				if closing != nil {
					select {
					case err := <-closing:
						if err != nil {
							t.Fatal(err)
						}
					case <-time.After(3 * time.Second):
						t.Fatal("late-watch Close actualjoin missing")
					}
				}
				_ = id
			})
		})
	}
}

func TestTaskSchedulerSleepFullClockRangeAndCancelledPast(t *testing.T) {
	for _, variant := range []string{"extreme-future", "cross-sign-future", "extreme-past", "cancelled-past", "nil-context"} {
		t.Run(variant, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var clock atomic.Int64
				clock.Store(0)
				if variant == "cross-sign-future" {
					clock.Store(math.MinInt64)
				}
				if variant == "extreme-past" || variant == "cancelled-past" {
					clock.Store(math.MaxInt64)
				}
				observed := make(chan int64, 4)
				var reads atomic.Int64
				var cancel context.CancelFunc
				def := taskDefinition(t, "task.sleep.range", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					caller, stop := context.WithCancel(ctx)
					defer stop()
					cancel = stop
					deadline := int64(math.MaxInt64)
					if variant == "extreme-past" || variant == "cancelled-past" {
						deadline = math.MinInt64
					}
					if variant == "cancelled-past" {
						stop()
					}
					var err error
					if variant == "nil-context" {
						//lint:ignore SA1012 Intentional nil-context rejection boundary test.
						err = r.SleepUntil(nil, deadline)
					} else {
						err = r.SleepUntil(caller, deadline)
					}
					if variant == "extreme-past" {
						if err != nil {
							return err
						}
					} else if variant == "nil-context" {
						if err == nil {
							return errors.New("nil sleep context accepted")
						}
					} else if !errors.Is(err, context.Canceled) {
						return errors.New("sleep cancellation lost on clock range")
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(variant), nil })
				})
				h := taskTestHarnessOptions(t, b.store, Options{Now: func() int64 {
					value := clock.Load()
					if reads.Add(1) <= 4 {
						observed <- value
					}
					return value
				}}, def)
				id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				if variant == "extreme-future" || variant == "cross-sign-future" {
					for k := 0; k < 2; k++ {
						select {
						case value := <-observed:
							if value != clock.Load() {
								t.Fatal("clock observer mismatch")
							}
						case <-time.After(3 * time.Second):
							t.Fatal("future sleep failed bounded timer clock recheck")
						}
					}
					if delay := taskSleepDelay(clock.Load(), math.MaxInt64); delay != time.Second {
						t.Fatal("full range not clamped before duration conversion", delay)
					}
					cancel()
				}
				if record := waitPublicTask(t, h, id); record.State.Outcome.Status != "completed" || record.State.Outcome.Result.Value != variant {
					t.Fatal(record)
				}
				if variant == "cancelled-past" || variant == "nil-context" {
					if reads.Load() != 0 {
						t.Fatal("invalid/cancelled past sleep observed host clock")
					}
				}
			})
		})
	}
}

// Context callbacks witness actual Session/core admission attempts. A queued
// signal is emitted only by Done AFTER the runtime's fast lifetime check; it is
// not a goroutine launch acknowledgement or a FIFO assumption.
type taskAdmissionContext struct {
	context.Context
	doneCalls atomic.Int64
	queued    chan struct{}
	once      sync.Once
}

func (c *taskAdmissionContext) Done() <-chan struct{} {
	c.doneCalls.Add(1)
	if c.queued != nil {
		c.once.Do(func() { close(c.queued) })
	}
	return c.Context.Done()
}

func TestTaskSchedulerOutcomesSingleAdmissionOrderEmptyAndDetached(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, releaseHost := make(chan struct{}), make(chan struct{})
		var captured *TaskRuntime
		def := taskDefinition(t, "task.outcomes.collection", func(_ context.Context, _ TaskRecord, r *TaskRuntime) error {
			captured = r
			close(entered)
			<-releaseHost
			return nil
		})
		h := taskTestHarness(t, b.store, def)
		cleanupTaskGates(t, releaseHost)
		// Terminal fixtures predate the reader invocation. Explicit null and
		// nested result/error placements all remain detached in caller order.
		var first, second, nullID, liveID ID
		_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			for index, dest := range []*ID{&first, &second, &nullID, &liveID} {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				*dest = id
				task := foundationTask(id, nil)
				if index < 3 {
					task.Status = "done"
					task.Execution.Native.State = *taskDone(JSON{"index": index, "nested": JSON{"value": "owned"}})
					if index == 1 {
						task.Status = "failed"
						task.Execution.Native.State = TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Result: &TaskValue{Present: true, Value: JSON{"result": []any{"kept"}}}, Error: &TaskOutcomeError{Message: "bounded", Detail: &TaskValue{Present: true, Value: JSON{"detail": "owned"}}}}}
					}
					if index == 2 {
						task.Execution.Native.State = *taskDone(nil)
					}
				}
				if err := tx.stage(Write{Op: "put-task", Task: &task}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		reader := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		h.session.taskBookkeeping(func() { h.scheduler.retryAfter[liveID] = ^uint64(0) })
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		baseline := &taskAdmissionContext{Context: bg}
		if _, ok, err := captured.Task(baseline, first); err != nil || !ok {
			t.Fatal(err)
		}
		singleAdmission := baseline.doneCalls.Load()
		if singleAdmission == 0 {
			t.Fatal("reference Task made no native admission")
		}
		collectionContext := &taskAdmissionContext{Context: bg}
		values, err := captured.Outcomes(collectionContext, []ID{second, first, second, nullID})
		if err != nil {
			t.Fatal(err)
		}
		if collectionContext.doneCalls.Load() != singleAdmission {
			t.Fatal("Outcomes used more than one snapshot admission", collectionContext.doneCalls.Load(), singleAdmission)
		}
		if len(values) != 4 || values[0].Status != "failed" || values[1].Status != "completed" || values[2].Status != "failed" || values[3].Result == nil || !values[3].Result.Present || values[3].Result.Value != nil {
			t.Fatal("outcome order/duplicates/null", values)
		}
		values[0].Result.Value.(map[string]any)["result"].([]any)[0] = "mutated"
		values[0].Error.Detail.Value.(map[string]any)["detail"] = "mutated"
		if values[2].Result.Value.(map[string]any)["result"].([]any)[0] != "kept" || values[2].Error.Detail.Value.(map[string]any)["detail"] != "owned" {
			t.Fatal("duplicate placements aliased")
		}
		fresh, err := captured.Outcomes(bg, []ID{second})
		if err != nil || fresh[0].Result.Value.(map[string]any)["result"].([]any)[0] != "kept" || fresh[0].Error.Detail.Value.(map[string]any)["detail"] != "owned" {
			t.Fatal("outcome read mutated storage", err)
		}
		emptyContext := &taskAdmissionContext{Context: bg}
		empty, err := captured.Outcomes(emptyContext, nil)
		if err != nil || empty == nil || len(empty) != 0 || emptyContext.doneCalls.Load() != singleAdmission {
			t.Fatal("empty outcome collection skipped admission", empty, err, emptyContext.doneCalls.Load())
		}
		cancelled, cancel := context.WithCancel(bg)
		cancel()
		if values, err := captured.Outcomes(cancelled, nil); !errors.Is(err, context.Canceled) || values != nil {
			t.Fatal("live empty cancelled collection", values, err)
		}
		//lint:ignore SA1012 Intentional nil-context rejection boundary test.
		if values, err := captured.Outcomes(nil, nil); err == nil || values != nil {
			t.Fatal("live empty nil-context collection", values, err)
		}
		if _, _, err := captured.Memo(cancelled, "missing"); !errors.Is(err, context.Canceled) {
			t.Fatal("live Memo caller cancellation", err)
		}
		if _, _, err := captured.TypedEntry(bg, nil, ID(MaxID)); err == nil || errors.Is(err, ErrSealed) {
			t.Fatal("live invalid entry token", err)
		}
		for _, ids := range [][]ID{{first, liveID}, {first, ID(MaxID)}, make([]ID, h.session.limits.MaxPage+1)} {
			if values, err := captured.Outcomes(bg, ids); err == nil || values != nil {
				t.Fatal("invalid collection returned partial values", values, err)
			}
		}
		if err := captured.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("sealed"), nil }); err != nil {
			t.Fatal(err)
		}
		for _, ids := range [][]ID{nil, {second, first}} {
			if _, err := captured.Outcomes(cancelled, ids); !errors.Is(err, ErrSealed) {
				t.Fatal("ended Outcomes did not precede cancelled caller", err)
			}
		}
		if _, _, err := captured.Memo(cancelled, "missing"); !errors.Is(err, ErrSealed) {
			t.Fatal("ended Memo did not precede cancelled caller", err)
		}
		if _, _, err := captured.TypedEntry(cancelled, nil, ID(MaxID)); !errors.Is(err, ErrSealed) {
			t.Fatal("ended TypedEntry did not precede invalid token/context", err)
		}
		state, err := h.Snapshot(bg)
		if err != nil || state.Tasks[reader].Status != "done" {
			t.Fatal(err)
		}
		releaseTaskGate(releaseHost)
	})
}

func TestTaskSchedulerOutcomesMemoTypedEntryQueuedSealPrecedence(t *testing.T) {
	for _, method := range []string{"outcomes-empty", "outcomes-values", "memo", "typed-entry"} {
		t.Run(method, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				entered, releaseHost := make(chan struct{}), make(chan struct{})
				var captured *TaskRuntime
				def := taskDefinition(t, "task.read.queued-seal", func(_ context.Context, _ TaskRecord, r *TaskRuntime) error {
					captured = r
					close(entered)
					<-releaseHost
					return nil
				})
				h := taskTestHarness(t, b.store, def)
				cleanupTaskGates(t, releaseHost)
				entryDef, err := DefineEntry("app.read-token")
				if err != nil {
					t.Fatal(err)
				}
				var terminalID, entryID ID
				_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
					var err error
					terminalID, err = tx.MintID()
					if err != nil {
						return err
					}
					task := foundationTask(terminalID, nil)
					task.Status = "done"
					task.Execution.Native.State = *taskDone(JSON{"owned": "terminal"})
					if err := tx.stage(Write{Op: "put-task", Task: &task}); err != nil {
						return err
					}
					entryID, err = tx.MintID()
					if err != nil {
						return err
					}
					return tx.AppendEntry(Entry{ID: entryID, Conversation: 1, Kind: "app.read-token", Value: JSON{"entry": "owned"}})
				})
				if err != nil {
					t.Fatal(err)
				}
				id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, entered)
				probe := &taskAdmissionContext{Context: bg, queued: make(chan struct{})}
				result := make(chan error, 1)
				err = captured.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) {
					go func() {
						var err error
						switch method {
						case "outcomes-empty":
							_, err = captured.Outcomes(probe, nil)
						case "outcomes-values":
							_, err = captured.Outcomes(probe, []ID{terminalID, terminalID})
						case "memo":
							_, _, err = captured.Memo(probe, "missing")
						case "typed-entry":
							_, _, err = captured.TypedEntry(probe, entryDef, entryID)
						}
						result <- err
					}()
					select {
					case <-probe.queued:
					case <-time.After(3 * time.Second):
						return nil, errors.New("read never reached real Session admission")
					}
					return taskDone("sealed"), nil
				})
				if err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-result:
					if !errors.Is(err, ErrSealed) {
						t.Fatal("queued read skipped authoritative lifetime", method, err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("queued read not released")
				}
				state, err := h.Snapshot(bg)
				if err != nil || state.Tasks[id].Status != "done" {
					t.Fatal(err)
				}
				releaseTaskGate(releaseHost)
			})
		})
	}
}

func TestTaskSchedulerOutcomesCollectionBudgetAndQueuedCancellation(t *testing.T) {
	for _, backendName := range []string{"memory", "journal"} {
		t.Run(backendName, func(t *testing.T) {
			l := DefaultLimits()
			l.MaxPage = 8
			l.MaxRecordBytes = 1024
			l.MaxFramePayloadBytes = 2048
			l.MaxDocumentBytes = 512
			l.MaxStringBytes = 512
			var store Storage
			var err error
			if backendName == "memory" {
				store, err = OpenMemory(MemoryOptions{Limits: &l})
			} else {
				store, err = OpenJournal(t.TempDir()+"/private", JournalOptions{Limits: &l})
			}
			if err != nil {
				t.Fatal(err)
			}
			entered, releaseHost := make(chan struct{}), make(chan struct{})
			var captured *TaskRuntime
			def := taskDefinition(t, "task.outcomes.budget", func(_ context.Context, _ TaskRecord, r *TaskRuntime) error {
				captured = r
				close(entered)
				<-releaseHost
				return nil
			})
			h := taskTestHarness(t, store, def)
			cleanupTaskGates(t, releaseHost)
			var terminal ID
			_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
				var err error
				terminal, err = tx.MintID()
				if err != nil {
					return err
				}
				task := foundationTask(terminal, nil)
				task.Status = "done"
				task.Execution.Native.State = *taskDone(strings.Repeat("v", 300))
				return tx.stage(Write{Op: "put-task", Task: &task})
			})
			if err != nil {
				t.Fatal(err)
			}
			reader := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			if err := h.Resume(bg); err != nil {
				t.Fatal(err)
			}
			awaitTaskSignal(t, entered)
			if values, err := captured.Outcomes(bg, []ID{terminal, terminal}); err != nil || len(values) != 2 {
				t.Fatal("valid bounded duplicate collection rejected", values, err)
			}
			// Each stored outcome fits; six independent caller placements exceed
			// the complete response budget but not its item-count policy.
			if values, err := captured.Outcomes(bg, []ID{terminal, terminal, terminal, terminal, terminal, terminal}); err == nil || values != nil {
				t.Fatal("aggregate outcome budget returned oversized/partial response", values, err)
			}
			caller, cancel := context.WithCancel(bg)
			defer cancel()
			probe := &taskAdmissionContext{Context: caller, queued: make(chan struct{})}
			result := make(chan error, 1)
			_, err = h.session.taskCommit(bg, func(*Tx) error {
				go func() { _, err := captured.Outcomes(probe, nil); result <- err }()
				select {
				case <-probe.queued:
				case <-time.After(3 * time.Second):
					return errors.New("empty read did not reach Session while held")
				}
				cancel()
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatal("queued empty cancellation", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("queued empty read not cancelled")
			}
			state, err := h.Snapshot(bg)
			if err != nil || state.Tasks[reader].Status != "running" {
				t.Fatal("cancelled outcome read ended invocation", err)
			}
			if err := captured.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil }); err != nil {
				t.Fatal(err)
			}
			releaseTaskGate(releaseHost)
		})
	}
}

func TestTaskSchedulerBoundWaitNilContextLifetimePrecedence(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, releaseHost := make(chan struct{}), make(chan struct{})
		var captured *TaskRuntime
		def := taskDefinition(t, "task.wait.nil", func(_ context.Context, _ TaskRecord, r *TaskRuntime) error {
			captured = r
			close(entered)
			<-releaseHost
			return nil
		})
		h := taskTestHarness(t, b.store, def)
		cleanupTaskGates(t, releaseHost)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		var epoch uint64
		h.session.taskBookkeeping(func() { epoch = h.scheduler.epoch.Load() })
		//lint:ignore SA1012 Intentional nil-context rejection boundary test.
		if _, err := captured.WaitForTask(nil, id); err == nil || errors.Is(err, ErrSealed) {
			t.Fatal("live nil taskwait context", err)
		}
		api := &ToolAPI{runtime: captured}
		//lint:ignore SA1012 Intentional nil-context rejection boundary test.
		if _, err := api.WaitForTask(nil, id); err == nil {
			t.Fatal("tool forwarding live nil wait accepted")
		}
		h.session.taskBookkeeping(func() {
			if len(h.scheduler.waiters) != 0 || len(h.scheduler.tickets) != 0 || h.scheduler.epoch.Load() != epoch {
				t.Error("nil taskwait leaked registration/claim/wake")
			}
		})
		if err := captured.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil }); err != nil {
			t.Fatal(err)
		}
		cancelled, cancel := context.WithCancel(bg)
		cancel()
		for _, caller := range []context.Context{nil, cancelled} {
			if _, err := captured.WaitForTask(caller, id); !errors.Is(err, ErrSealed) {
				t.Fatal("ended wait precedence", err)
			}
		}
		releaseTaskGate(releaseHost)
	})
}

func TestTaskSchedulerPhaseRegistryToolsCapturedPersistedLimits(t *testing.T) {
	// Zero/default snapshots stay empty without consulting a live Registry.
	zero, err := (TaskRegistrySnapshot{}).Tools()
	if err != nil || len(zero) != 0 {
		t.Fatal("zero snapshot tools", zero, err)
	}
	for _, backendName := range []string{"memory", "journal"} {
		for _, schemaSize := range []string{"fits-document", "fits-record-only"} {
			t.Run(backendName+"/"+schemaSize, func(t *testing.T) {
				l := DefaultLimits()
				l.MaxDocumentBytes = 256
				// OpenSession supplies only the root conversation. Do not create
				// agent/config builtin Documents under this deliberately small
				// document-envelope policy; no fixture limit widening is needed.
				var store Storage
				var err error
				if backendName == "memory" {
					store, err = OpenMemory(MemoryOptions{Limits: &l})
				} else {
					store, err = OpenJournal(t.TempDir()+"/private", JournalOptions{Limits: &l})
				}
				if err != nil {
					t.Fatal(err)
				}
				registry := NewRegistry()
				schema := JSON{"type": "object"}
				if schemaSize == "fits-record-only" {
					schema["description"] = strings.Repeat("x", 512)
				}
				wire, err := encodeBounded(schema, l, l.MaxRecordBytes)
				if err != nil || schemaSize == "fits-record-only" && len(wire) <= l.MaxDocumentBytes {
					t.Fatal("schema does not distinguish record/document policy", err)
				}
				registration := ToolRegistration{Definition: goai.Tool{Name: "policy_tool", Parameters: json.RawMessage(wire)}, Implementation: "policy.tool", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
					return ToolResult{}, errors.New("registry inspection dispatched tool")
				}}
				if err := registry.Register(registration); err != nil {
					t.Fatal("legal default registry schema", err)
				}
				before, ok := registry.current("policy_tool")
				if !ok {
					t.Fatal("missing registration")
				}
				probe := make(chan error, 1)
				definition := taskDefinition(t, "task.registry.persisted-policy", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					snapshot, err := r.Registry()
					if err == nil {
						if snapshot.limits != l || snapshot.Task(r.definition.Kind()) != r.definition || !sameImplementation(snapshot.pins["policy_tool"].offer, before.offer) {
							err = errors.New("phase snapshot policy/identity changed")
						}
					}
					if err == nil {
						first, firstErr := snapshot.Tools()
						if schemaSize == "fits-record-only" {
							var rejected *StorageRejected
							if !errors.As(firstErr, &rejected) || first != nil {
								err = fmt.Errorf("oversize Tools did not reject strictly: %v", firstErr)
							}
							second, secondErr := snapshot.Tools()
							if !errors.As(secondErr, &rejected) || second != nil {
								err = fmt.Errorf("repeat oversize Tools did not reject: %v", secondErr)
							}
						} else {
							if firstErr != nil || len(first) != 1 {
								err = fmt.Errorf("fitting Tools: %v", firstErr)
							} else {
								first[0].Parameters[0] = 'x'
								first[0].Name = "caller_mutation"
								second, secondErr := snapshot.Tools()
								var detached JSON
								if secondErr != nil || len(second) != 1 || second[0].Name != "policy_tool" {
									err = fmt.Errorf("repeat detached Tools: %v", secondErr)
								} else if e := decodeStrict(second[0].Parameters, l, l.MaxDocumentBytes, &detached); e != nil || !equalJSONValue(detached, schema) {
									err = fmt.Errorf("schema mutation escaped: %v", e)
								}
							}
						}
						current, exists := registry.current("policy_tool")
						if !exists || !sameImplementation(current.offer, before.offer) {
							err = errors.New("Tools altered registration")
						}
					}
					probe <- err
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("policy checked"), nil })
				})
				if _, err := registry.RegisterTask(definition); err != nil {
					t.Fatal(err)
				}
				h, err := Open(bg, store, Options{Registry: registry})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := h.Close(bg); err != nil {
						t.Error(err)
					}
				})
				id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if record := waitPublicTask(t, h, id); record.State.Outcome.Status != "completed" {
					t.Fatal("policy probe failed", record)
				}
				select {
				case err := <-probe:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("public runtime policy probe missing")
				}
			})
		}
	}
}

func TestTaskSchedulerAdoptedOwnMarkKeepsReadsRejectsWritesAndJoinsBeforeFreshAbort(t *testing.T) {
	// Pinned harness-tasks:865: marks reject writes, not live host reads.
	backends(t, func(t *testing.T, b backend) {
		entered, releaseRun := make(chan struct{}), make(chan struct{})
		captured := make(chan *TaskRuntime, 1)
		fresh := make(chan *TaskRuntime, 1)
		markedProbe := make(chan error, 1)
		var runs, aborts, callbacks atomic.Int64
		definition := taskDefinition(t, "task.ownmark.sealed", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			runs.Add(1)
			if _, err := r.MemoCandidate(ctx, "kept", 1); err != nil {
				return err
			}
			captured <- r
			close(entered)
			<-ctx.Done()
			// Continue inside the actual marked run despite cancellation; use
			// fresh caller contexts so lifetime, not caller cancellation, wins.
			var probeErr error
			if value, ok, err := r.Memo(bg, "kept"); err != nil || !ok || !equalJSONValue(value, 1) {
				probeErr = fmt.Errorf("marked run kept memo read: %v", err)
			}
			if record, ok, err := r.Task(bg, r.TaskID()); err != nil || !ok || !record.AbortRequested {
				probeErr = fmt.Errorf("marked run Task read: %v", err)
			}
			if _, err := r.Registry(); err != nil {
				probeErr = fmt.Errorf("marked run Registry read: %v", err)
			}
			if _, err := r.MemoCandidate(bg, "late", 2); err == nil || !strings.Contains(err.Error(), "abort") {
				probeErr = fmt.Errorf("marked run memo write: %v", err)
			}
			if err := r.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) { callbacks.Add(1); return taskDone("late"), nil }); err == nil || !strings.Contains(err.Error(), "abort") {
				probeErr = fmt.Errorf("marked run commit: %v", err)
			}
			markedProbe <- probeErr
			<-releaseRun // Deliberately retain actual host permit after the probe.
			return nil
		})
		options := definition.options
		options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			aborts.Add(1)
			fresh <- r
			value, ok, err := r.Memo(ctx, "kept")
			if err != nil || !ok || !equalJSONValue(value, 1) {
				return fmt.Errorf("fresh abort kept memo: %v", err)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
			})
		}
		var err error
		definition, err = DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, b.store, definition)
		cleanupTaskGates(t, releaseRun)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		old := <-captured
		aborting := make(chan error, 1)
		go func() { _, err := h.AbortTask(bg, id); aborting <- err }()
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for {
			record, _, err := h.Task(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if record.AbortRequested {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("ownmark adoption missing")
			default:
				runtime.Gosched()
			}
		}
		select {
		case <-old.Context().Done():
		case <-ctx.Done():
			t.Fatal("mark did not signal old invocation")
		}
		if value, ok, err := old.Memo(bg, "kept"); err != nil || !ok || !equalJSONValue(value, 1) {
			t.Fatal("marked native memo read", err)
		}
		if record, ok, err := old.Task(bg, id); err != nil || !ok || !record.AbortRequested {
			t.Fatal("marked native task read", err)
		}
		if _, err := old.Registry(); err != nil {
			t.Fatal("marked native registry read", err)
		}
		if _, err := old.MemoCandidate(bg, "late", 2); err == nil || !strings.Contains(err.Error(), "abort") {
			t.Fatal("marked native memo write", err)
		}
		if err := old.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) { callbacks.Add(1); return taskDone("late"), nil }); err == nil || !strings.Contains(err.Error(), "abort") {
			t.Fatal("marked native commit", err)
		}
		select {
		case <-old.done:
			t.Fatal("stubborn old host joined early")
		default:
		}
		select {
		case <-aborting:
			t.Fatal("public abort returned before actual host")
		default:
		}
		select {
		case err := <-markedProbe:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("actual marked run read/write probe missing")
		}
		state, err := h.Snapshot(bg)
		if err != nil || state.Tasks[id].Execution.Native.Memos["kept"] == nil || !equalJSONValue(state.Tasks[id].Execution.Native.Memos["kept"].Value, 1) || state.Tasks[id].Execution.Native.Memos["late"] != nil {
			t.Fatal("marked run changed retained memo", err)
		}
		if callbacks.Load() != 0 || runs.Load() != 1 || aborts.Load() != 0 {
			t.Fatal("marked host lost permit or executed callback")
		}
		releaseTaskGate(releaseRun)
		awaitTaskSignal(t, old.done)
		if _, _, err := old.Memo(bg, "kept"); !errors.Is(err, ErrSealed) {
			t.Fatal("returned old runtime remained readable", err)
		}
		select {
		case err := <-aborting:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("abort actualjoin missing")
		}
		if record := waitPublicTask(t, h, id); record.State.Outcome.Status != "aborted" {
			t.Fatal(record)
		}
		select {
		case next := <-fresh:
			if next == old || !next.abortMode {
				t.Fatal("abort reused ended invocation")
			}
			awaitTaskSignal(t, next.done)
		case <-ctx.Done():
			t.Fatal("fresh abort invocation missing")
		}
		if callbacks.Load() != 0 || runs.Load() != 1 || aborts.Load() != 1 {
			t.Fatal("ownmark effects", runs.Load(), aborts.Load(), callbacks.Load())
		}
	})
}

// Adapted from the auditors uncompiled initial-acquisition source supplement.
// Uses the actual public runtime WatchDefinition; absent acquisition remains a
// read exception before the post-await ended check, as in pinned #watchDoc.
func TestTaskSchedulerInitialWatchAcquisitionQueuedSealCancelAbsent(t *testing.T) {
	for _, seal := range []string{"terminal", "close", "caller-cancel"} {
		for _, present := range []bool{false, true} {
			name := "absent"
			if present {
				name = "present"
			}
			t.Run(seal+"/"+name, func(t *testing.T) {
				backends(t, func(t *testing.T, b backend) {
					entered, releaseHost := make(chan struct{}), make(chan struct{})
					capturedRuntime := make(chan *TaskRuntime, 1)
					def := taskDefinition(t, "task.initial-watch", func(_ context.Context, _ TaskRecord, r *TaskRuntime) error {
						capturedRuntime <- r
						close(entered)
						<-releaseHost
						return nil
					})
					h := taskTestHarness(t, b.store, def)
					cleanupTaskGates(t, releaseHost)
					notes, err := DefineDocument(DefinitionOptions{Kind: "app.initial-watch", Scope: "session", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"value": "owned"}, nil }})
					if err != nil {
						t.Fatal(err)
					}
					if present {
						_, err = h.CommitTasks(bg, 1, func(tx *Tx) error { _, err := tx.AcquireDocument(notes, 0, nil, nil); return err })
						if err != nil {
							t.Fatal(err)
						}
					}
					createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
					if err := h.Resume(bg); err != nil {
						t.Fatal(err)
					}
					awaitTaskSignal(t, entered)
					captured := <-capturedRuntime
					caller, cancel := context.WithCancel(bg)
					defer cancel()
					probe := &taskAdmissionContext{Context: caller, queued: make(chan struct{})}
					type result struct {
						watch *DocumentWatch
						err   error
					}
					finished := make(chan result, 1)
					var closing chan error
					queueAcquisition := func() error {
						go func() {
							watch, err := captured.WatchDefinition(probe, notes, 0, nil)
							finished <- result{watch, err}
						}()
						// Done is evaluated only after the runtime early checks when
						// the actual public Session acquisition waits for this line.
						select {
						case <-probe.queued:
							return nil
						case <-time.After(3 * time.Second):
							return errors.New("public watch never queued on initial Session acquisition")
						}
					}
					if seal == "terminal" {
						err = captured.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) {
							if err := queueAcquisition(); err != nil {
								return nil, err
							}
							return taskDone("sealed"), nil
						})
					} else {
						_, err = h.session.taskCommit(bg, func(*Tx) error {
							if err := queueAcquisition(); err != nil {
								return err
							}
							if seal == "caller-cancel" {
								cancel()
								return nil
							}
							closing = make(chan error, 1)
							go func() { closing <- h.Close(bg) }()
							ctx, stop := context.WithTimeout(bg, 3*time.Second)
							defer stop()
							for !h.closing.Load() {
								select {
								case <-ctx.Done():
									return ctx.Err()
								default:
									runtime.Gosched()
								}
							}
							return nil
						})
					}
					if err != nil {
						t.Fatal(err)
					}
					select {
					case got := <-finished:
						if got.watch != nil {
							got.watch.Stop()
							t.Fatal("initial queued acquisition escaped lifetime/close/cancel", got.err)
						}
						if seal == "caller-cancel" {
							if !errors.Is(got.err, context.Canceled) {
								t.Fatal("initial acquisition cancellation", got.err)
							}
						} else if !present {
							// Pinned scheduler.ts #watchDoc returns undefined on absent
							// acquisition before its post-await ended check.
							if got.err != nil {
								t.Fatal("absent-watch read exception", got.err)
							}
						} else if seal == "terminal" {
							if !errors.Is(got.err, ErrSealed) {
								t.Fatal("terminal before initial acquisition", got.err)
							}
						} else if !errors.Is(got.err, ErrClosed) && !errors.Is(got.err, ErrSealed) {
							t.Fatal("Close before initial acquisition", got.err)
						}
					case <-time.After(3 * time.Second):
						t.Fatal("initial queued acquisition did not return")
					}
					h.session.observerMu.Lock()
					remaining := len(h.session.watches)
					h.session.observerMu.Unlock()
					if remaining != 0 {
						t.Fatal("initial acquisition retained process registration", remaining)
					}
					if seal == "caller-cancel" {
						if err := captured.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil }); err != nil {
							t.Fatal("cancelled acquisition ended live invocation", err)
						}
					}
					releaseTaskGate(releaseHost)
					if closing != nil {
						select {
						case err := <-closing:
							if err != nil {
								t.Fatal(err)
							}
						case <-time.After(3 * time.Second):
							t.Fatal("Close did not join watch owner host")
						}
					}
					awaitTaskSignal(t, captured.done)
				})
			})
		}
	}
}

func TestTaskSchedulerPublicWaitQueuedCallerCancelOrClose(t *testing.T) {
	for _, seal := range []string{"caller-cancel", "close"} {
		t.Run(seal, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				entered, releaseHost := make(chan struct{}), make(chan struct{})
				captured := make(chan *TaskRuntime, 1)
				definition := taskDefinition(t, "task.publicwait.queued", func(_ context.Context, _ TaskRecord, r *TaskRuntime) error {
					captured <- r
					close(entered)
					<-releaseHost
					return nil
				})
				h := taskTestHarness(t, b.store, definition)
				cleanupTaskGates(t, releaseHost)
				id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, entered)
				picked := <-captured
				caller, cancel := context.WithCancel(bg)
				defer cancel()
				probe := &taskAdmissionContext{Context: caller, queued: make(chan struct{})}
				finished := make(chan error, 1)
				var closing chan error
				_, err := h.session.taskCommit(bg, func(*Tx) error {
					go func() { _, err := h.WaitForTask(probe, id); finished <- err }()
					// Public Resume does no line acquisition. Done therefore proves
					// this public wait passed Resume and reached initial Session.enter.
					select {
					case <-probe.queued:
					case <-time.After(3 * time.Second):
						return errors.New("public wait never queued on actual line")
					}
					if seal == "caller-cancel" {
						cancel()
						return nil
					}
					closing = make(chan error, 1)
					go func() { closing <- h.Close(bg) }()
					ctx, stop := context.WithTimeout(bg, 3*time.Second)
					defer stop()
					for !h.closing.Load() {
						select {
						case <-ctx.Done():
							return ctx.Err()
						default:
							runtime.Gosched()
						}
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-finished:
					want := error(context.Canceled)
					if seal == "close" {
						want = ErrClosed
					}
					if !errors.Is(err, want) {
						t.Fatal("queued public wait result", err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("queued public wait rejection missing")
				}
				h.session.taskBookkeeping(func() {
					if len(h.scheduler.waiters) != 0 || len(h.scheduler.tickets) != 0 {
						t.Error("queued rejected wait retained registration/claims")
					}
					if h.scheduler.invocations[id] != picked {
						t.Error("queued wait lost stubborn host permit")
					}
				})
				select {
				case <-picked.done:
					t.Fatal("queued wait ended actual host early")
				default:
				}
				if seal == "caller-cancel" {
					if err := picked.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) {
						return taskDone("caller cancellation leaves task live"), nil
					}); err != nil {
						t.Fatal(err)
					}
				}
				releaseTaskGate(releaseHost)
				awaitTaskSignal(t, picked.done)
				if closing != nil {
					select {
					case err := <-closing:
						if err != nil {
							t.Fatal(err)
						}
					case <-time.After(3 * time.Second):
						t.Fatal("queued wait Close actualjoin missing")
					}
				}
			})
		})
	}
}

func TestTaskSchedulerCloseRejectsQueuedTaskAndBothPendingIdleScopes(t *testing.T) {
	// Pinned tasks822: all three admitted host readers are live before a fourth
	// public task wait queues on a witnessed line and Close seals that line.
	backends(t, func(t *testing.T, b backend) {
		entered, releaseHost := make(chan struct{}), make(chan struct{})
		captured := make(chan *TaskRuntime, 1)
		definition := taskDefinition(t, "task.waitclose.both-idle", func(_ context.Context, _ TaskRecord, r *TaskRuntime) error {
			captured <- r
			close(entered)
			<-releaseHost
			return nil
		})
		h := taskTestHarness(t, b.store, definition)
		cleanupTaskGates(t, releaseHost)
		conversation, err := h.Conversation(bg, 1)
		if err != nil {
			t.Fatal(err)
		}
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		picked := <-captured
		type result struct {
			origin string
			err    error
		}
		finished := make(chan result, 4)
		go func() { _, err := h.WaitForTask(bg, id); finished <- result{"pending-task", err} }()
		go func() { finished <- result{"pending-harness-idle", h.WaitForIdle(bg)} }()
		go func() { finished <- result{"pending-conversation-idle", conversation.WaitForIdle(bg)} }()
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for {
			var taskWaits, harnessIdle, conversationIdle int
			h.session.taskBookkeeping(func() {
				for wait := range h.scheduler.waiters {
					if wait.binding != nil {
						continue
					}
					if wait.idleScope == nil && wait.target == id {
						taskWaits++
					}
					if wait.idleScope != nil && *wait.idleScope == 0 {
						harnessIdle++
					}
					if wait.idleScope != nil && *wait.idleScope == conversation.ID() {
						conversationIdle++
					}
				}
			})
			if taskWaits == 1 && harnessIdle == 1 && conversationIdle == 1 {
				break
			}
			select {
			case got := <-finished:
				t.Fatal("reader finished before Close", got)
			case <-ctx.Done():
				t.Fatal("simultaneous actual wait registrations missing")
			default:
				runtime.Gosched()
			}
		}
		probe := &taskAdmissionContext{Context: bg, queued: make(chan struct{})}
		closing := make(chan error, 1)
		_, err = h.session.taskCommit(bg, func(*Tx) error {
			go func() { _, err := h.WaitForTask(probe, id); finished <- result{"queued-task", err} }()
			select {
			case <-probe.queued:
			case <-ctx.Done():
				return errors.New("fourth public task wait never queued on actual line")
			}
			go func() { closing <- h.Close(bg) }()
			for !h.closing.Load() {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
					runtime.Gosched()
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for len(seen) < 4 {
			select {
			case got := <-finished:
				if seen[got.origin] || !errors.Is(got.err, ErrClosed) {
					t.Fatal("Close reader result", got, seen)
				}
				seen[got.origin] = true
			case <-ctx.Done():
				t.Fatal("Close failed to detach all pending and queued readers", seen)
			}
		}
		h.session.taskBookkeeping(func() {
			if len(h.scheduler.waiters) != 0 || len(h.scheduler.tickets) != 0 {
				t.Error("Close retained reader registrations/claims")
			}
			if h.scheduler.invocations[id] != picked {
				t.Error("reader Close lost actual host permit")
			}
		})
		select {
		case <-picked.done:
			t.Fatal("Close joined stubborn host before release")
		default:
		}
		select {
		case err := <-closing:
			t.Fatal("Close returned before actual host", err)
		default:
		}
		releaseTaskGate(releaseHost)
		awaitTaskSignal(t, picked.done)
		select {
		case err := <-closing:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("Close actual host bookkeeping join missing")
		}
	})
}

func TestTaskSchedulerOwnMarkAfterProgressSkipsNextPhase(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		progressed, releaseRun := make(chan struct{}), make(chan struct{})
		captured, fresh := make(chan *TaskRuntime, 1), make(chan *TaskRuntime, 1)
		var firstPhases, secondPhases, aborts atomic.Int64
		definition, err := DefineTask(TaskDefinitionOptions{
			Kind: "task.mark.progress-boundary", Version: 1,
			Initial: func(any) (JSON, error) { return JSON{"phase": "one"}, nil },
			Phases: map[string]TaskPhase{
				"one": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					firstPhases.Add(1)
					if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "running", Checkpoint: JSON{"phase": "two"}}, nil
					}); err != nil {
						return err
					}
					captured <- r
					close(progressed)
					<-releaseRun // Ignore mark signal; durable progress must not dispatch two.
					return nil
				},
				"two": func(context.Context, TaskRecord, *TaskRuntime) error {
					secondPhases.Add(1)
					return errors.New("phase two escaped adopted mark")
				},
			},
			Abort: func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				aborts.Add(1)
				fresh <- r
				if !task.AbortRequested || task.State.Checkpoint["phase"] != "two" {
					return errors.New("fresh abort lost marked progressed checkpoint")
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "boundary"}}, nil
				})
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, b.store, definition)
		cleanupTaskGates(t, releaseRun)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, progressed)
		old := <-captured
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		aborting := make(chan error, 1)
		go func() {
			result, err := h.AbortTask(bg, id)
			if err == nil && result != "marked" {
				err = fmt.Errorf("abort result %s", result)
			}
			aborting <- err
		}()
		for {
			record, _, err := h.Task(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if record.AbortRequested {
				if record.State.Checkpoint["phase"] != "two" {
					t.Fatal("mark lost progress", record)
				}
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("progress mark adoption missing")
			default:
				runtime.Gosched()
			}
		}
		select {
		case <-old.done:
			t.Fatal("progressed host permit released before return")
		default:
		}
		if secondPhases.Load() != 0 || aborts.Load() != 0 {
			t.Fatal("fresh dispatch before actual return")
		}
		releaseTaskGate(releaseRun)
		awaitTaskSignal(t, old.done)
		select {
		case err := <-aborting:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("marked progressed host callerjoin missing")
		}
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "aborted" || record.State.Outcome.Reason != "boundary" {
			t.Fatal(record)
		}
		select {
		case next := <-fresh:
			if next == old || !next.abortMode {
				t.Fatal("fresh progress abort runtime identity")
			}
			awaitTaskSignal(t, next.done)
		case <-ctx.Done():
			t.Fatal("fresh abort missing")
		}
		if firstPhases.Load() != 1 || secondPhases.Load() != 0 || aborts.Load() != 1 {
			t.Fatal("mark progressed phase effects", firstPhases.Load(), secondPhases.Load(), aborts.Load())
		}
	})
}

// Adapted from the auditors uncompiled repeat-abort source supplement.
// Owner-authored source only; no external test is counted as a pass.
func TestTaskSchedulerRepeatedAbortCallerCancelKeepsMarkAndFreshAbort(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		runEntered, releaseRun := make(chan struct{}), make(chan struct{})
		abortEntered, releaseAbort := make(chan struct{}), make(chan struct{})
		oldRuntime, freshRuntime := make(chan *TaskRuntime, 1), make(chan *TaskRuntime, 1)
		var runs, aborts atomic.Int64
		definition, err := DefineTask(TaskDefinitionOptions{
			Kind: "task.repeat-abort", Version: 1,
			Initial: func(any) (JSON, error) { return JSON{"phase": "run"}, nil },
			Phases: map[string]TaskPhase{"run": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				runs.Add(1)
				if _, err := r.MemoCandidate(ctx, "kept", 1); err != nil {
					return err
				}
				oldRuntime <- r
				close(runEntered)
				<-releaseRun // Ignore cancellation; caller must detach before host return.
				return nil
			}},
			Abort: func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				aborts.Add(1)
				if !task.AbortRequested {
					return errors.New("fresh abort lacks adopted mark")
				}
				freshRuntime <- r
				close(abortEntered)
				<-releaseAbort // Repeat abort must neither signal nor replace this runtime.
				if ctx.Err() != nil {
					return fmt.Errorf("repeat abort signalled active abort: %w", ctx.Err())
				}
				value, ok, err := r.Memo(ctx, "kept")
				if err != nil || !ok || !equalJSONValue(value, 1) {
					return fmt.Errorf("fresh abort memo changed: %v", err)
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "once"}}, nil
				})
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, b.store, definition)
		cleanupTaskGates(t, releaseRun, releaseAbort)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, runEntered)
		old := <-oldRuntime
		caller, cancel := context.WithCancel(bg)
		defer cancel()
		firstResult := make(chan error, 1)
		go func() { _, err := h.AbortTask(caller, id); firstResult <- err }()
		deadline, stop := context.WithTimeout(bg, 3*time.Second)
		defer stop()
		for {
			record, found, err := h.Task(deadline, id)
			if err != nil || !found {
				t.Fatal("mark lookup", err)
			}
			if record.AbortRequested {
				break
			}
			select {
			case <-deadline.Done():
				t.Fatal("public mark never adopted")
			default:
				runtime.Gosched()
			}
		}
		awaitTaskSignal(t, old.Context().Done())
		select {
		case <-old.done:
			t.Fatal("old stubborn host returned before release")
		default:
		}
		cancel()
		select {
		case err := <-firstResult:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("cancelled abort caller did not detach", err)
			}
		case <-deadline.Done():
			t.Fatal("abort caller remained joined after cancellation")
		}
		h.session.taskBookkeeping(func() {
			if h.scheduler.invocations[id] != old {
				t.Error("caller cancellation released actual host permit")
			}
		})
		record, _, err := h.Task(bg, id)
		if err != nil || !record.AbortRequested || record.State.Outcome != nil || aborts.Load() != 0 {
			t.Fatal("cancelled caller erased mark or started abort early", record, err)
		}
		releaseTaskGate(releaseRun)
		awaitTaskSignal(t, old.done)
		awaitTaskSignal(t, abortEntered)
		next := <-freshRuntime
		if next == old || !next.abortMode {
			t.Fatal("abort reused marked run")
		}
		if result, err := h.AbortTask(bg, id); err != nil || result != "marked" {
			t.Fatal("repeated abort result", result, err)
		}
		if next.Context().Err() != nil || next.ended.Load() || aborts.Load() != 1 {
			t.Fatal("repeated abort signalled/ended/replayed handler")
		}
		h.session.taskBookkeeping(func() {
			if h.scheduler.invocations[id] != next {
				t.Error("repeated abort replaced actual abort host")
			}
		})
		releaseTaskGate(releaseAbort)
		settled := waitPublicTask(t, h, id)
		awaitTaskSignal(t, next.done)
		if settled.State.Outcome.Status != "aborted" || settled.State.Outcome.Reason != "once" || runs.Load() != 1 || aborts.Load() != 1 {
			t.Fatal("repeated abort final effect/outcome", settled, runs.Load(), aborts.Load())
		}
		if result, err := h.AbortTask(bg, id); err != nil || result != "terminal" {
			t.Fatal("terminal abort repeated", result, err)
		}
	})
}

func TestTaskSchedulerFailFastSelectedAncestorPrefixCannotReserveOrdinaryDescendants(t *testing.T) {
	for _, prefix := range []string{"no-mark", "one-adopted-mark"} {
		for _, descendantKind := range []string{"native", "generation", "tool"} {
			for _, edge := range []string{"task", "conversation"} {
				t.Run(prefix+"/"+descendantKind+"/"+edge, func(t *testing.T) {
					backends(t, func(t *testing.T, b backend) {
						var effects atomic.Int64
						definition := taskDefinition(t, "task.failfast.inherited", func(context.Context, TaskRecord, *TaskRuntime) error {
							effects.Add(1)
							return errors.New("prefix test must not dispatch")
						})
						h := taskTestHarness(t, b.store, definition)
						parent := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
						failed := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: parent}})
						first := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: parent}})
						selected := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: parent}})
						var descendant, conversation, background, backgroundChild, terminal, terminalConversation, afterTerminal, outside ID
						_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
							var err error
							conversation, err = tx.MintID()
							if err != nil {
								return err
							}
							return tx.CreateConversation(Conversation{ID: conversation, Owner: selected})
						})
						if err != nil {
							t.Fatal(err)
						}
						_, err = h.CommitTasks(bg, conversation, func(tx *Tx) error {
							ownership := TaskOwnership{Kind: "task", Task: selected}
							if edge == "conversation" {
								ownership = TaskOwnership{Kind: "conversation"}
							}
							var err error
							if descendantKind == "native" {
								descendant, err = tx.CreateTask(definition, JSON{"kept": "input"}, TaskOptions{Ownership: ownership})
								return err
							}
							descendant, err = tx.MintID()
							if err != nil {
								return err
							}
							kind := "pi.generation"
							checkpoint, err := dtoObject(generationCheckpoint{Phase: "intent", Input: "kept"}, tx.limits)
							if descendantKind == "tool" {
								kind = "pi.tool"
								checkpoint, err = dtoObject(toolCheckpoint{CallID: "inherited-call", Arguments: JSON{}}, tx.limits)
							}
							if err != nil {
								return err
							}
							owner := selected
							if edge == "conversation" {
								owner = 0
							}
							return tx.PutTask(Task{ID: descendant, Conversation: conversation, Owner: owner, Kind: kind, Status: "pending", Checkpoint: checkpoint})
						})
						if err != nil {
							t.Fatal(err)
						}
						// A background member is conversation-owned (background task
						// ownership must not be forged as a task-owned child).
						_, err = h.CommitTasks(bg, conversation, func(tx *Tx) error {
							var err error
							background, err = tx.CreateTask(definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}, Background: true})
							return err
						})
						if err != nil {
							t.Fatal(err)
						}
						backgroundChild = createPublicTask(t, h, definition, nil, TaskOptions{Conversation: conversation, Ownership: TaskOwnership{Kind: "task", Task: background}})
						terminal = createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: selected}})
						_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
							var err error
							terminalConversation, err = tx.MintID()
							if err != nil {
								return err
							}
							return tx.CreateConversation(Conversation{ID: terminalConversation, Owner: terminal})
						})
						if err != nil {
							t.Fatal(err)
						}
						outside = createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: parent}})
						_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
							for _, id := range []ID{failed, terminal} {
								task, err := copyTask(tx.state.Tasks[id], tx.limits)
								if err != nil {
									return err
								}
								task.Status = "done"
								task.Execution.Native.State = *taskDone("terminal fence")
								if id == failed {
									task.Status = "failed"
									task.Execution.Native.State = TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "selected failure"}}}
								}
								if err := tx.stage(Write{Op: "put-task", Task: &task}); err != nil {
									return err
								}
							}
							p, err := copyTask(tx.state.Tasks[parent], tx.limits)
							if err != nil {
								return err
							}
							p.Status = "waiting"
							p.Execution.Native.State = TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: []ID{failed, first, selected}, Policy: "failFast"}
							return tx.stage(Write{Op: "put-task", Task: &p})
						})
						if err != nil {
							t.Fatal(err)
						}
						_, err = h.CommitTasks(bg, terminalConversation, func(tx *Tx) error {
							var err error
							afterTerminal, err = tx.CreateTask(definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
							return err
						})
						if err != nil {
							t.Fatal(err)
						}
						if prefix == "one-adopted-mark" {
							if err := h.scheduler.reconcile(); err != nil {
								t.Fatal(err)
							}
						}
						before, err := h.Snapshot(bg)
						if err != nil {
							t.Fatal(err)
						}
						if taskAborted(before.Tasks[first]) != (prefix == "one-adopted-mark") || taskAborted(before.Tasks[selected]) || taskAborted(before.Tasks[descendant]) {
							t.Fatal("wrong bounded mark prefix")
						}
						err = h.session.readTasks(bg, func(state Snapshot) error {
							if !taskSelectedFailFastCancellation(state, selected) || !taskSelectedFailFastCancellation(state, descendant) || !taskBelowCancelled(state, state.Tasks[descendant]) || h.scheduler.runnable(state, state.Tasks[descendant]) {
								t.Error("ordinary descendant escaped selected-ancestor intent")
							}
							if !taskBelowCancelled(state, Task{Conversation: conversation}) {
								t.Error("ownedconversation queued-input probe missed selected intent")
							}
							for _, id := range []ID{background, backgroundChild, afterTerminal, outside} {
								if taskSelectedFailFastCancellation(state, id) || !h.scheduler.runnable(state, state.Tasks[id]) {
									t.Error("background/terminal/unselected positive fence widened", id)
								}
							}
							return nil
						})
						if err != nil {
							t.Fatal(err)
						}
						h.session.taskBookkeeping(func() {
							for _, id := range []ID{first, background, backgroundChild, afterTerminal, outside} {
								h.scheduler.retryAfter[id] = ^uint64(0)
							}
							h.scheduler.cursor = selected
						})
						reservations, err := h.scheduler.reservePass(false)
						discardUnstartedTaskReservations(t, h, reservations)
						if err != nil || len(reservations) != 0 {
							t.Fatal("rotating cursor reserved unmarked descendant", err, len(reservations))
						}
						after, err := h.Snapshot(bg)
						if err != nil || after.Seq != before.Seq || effects.Load() != 0 || taskAborted(after.Tasks[selected]) || taskAborted(after.Tasks[descendant]) {
							t.Fatal("prefix guard wrote/signalled/dispatched", err)
						}
					})
				})
			}
		}
	}
}

func TestTaskSchedulerActiveDescendantPhaseStopsBeforeSelectedOwnerMark(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		captured := make(chan *TaskRuntime, 1)
		var nextEffects, aborts atomic.Int64
		definition, err := DefineTask(TaskDefinitionOptions{Kind: "task.inherited.active", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "one"}, nil }, Phases: map[string]TaskPhase{
			"one": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				captured <- r
				close(entered)
				<-release
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "running", Checkpoint: JSON{"phase": "two"}}, nil
				})
			},
			"two": func(context.Context, TaskRecord, *TaskRuntime) error {
				nextEffects.Add(1)
				return errors.New("selected ancestor failed but next descendant phase dispatched")
			},
		}, Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			aborts.Add(1)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
			})
		}})
		if err != nil {
			t.Fatal(err)
		}
		control := taskDefinition(t, "task.inherited.control", func(context.Context, TaskRecord, *TaskRuntime) error {
			return errors.New("control must remain undispatched")
		})
		h := taskTestHarness(t, b.store, definition)
		cleanupTaskGates(t, release)
		p := createPublicTask(t, h, control, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		f := createPublicTask(t, h, control, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: p}})
		s := createPublicTask(t, h, control, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: p}})
		d := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: s}})
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			parent, err := copyTask(tx.state.Tasks[p], tx.limits)
			if err != nil {
				return err
			}
			parent.Status = "waiting"
			parent.Execution.Native.State = TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: []ID{f, s}, Policy: "failFast"}
			return tx.stage(Write{Op: "put-task", Task: &parent})
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		picked := <-captured
		h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(false) })
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			failed, err := copyTask(tx.state.Tasks[f], tx.limits)
			if err != nil {
				return err
			}
			failed.Status = "failed"
			failed.Execution.Native.State = TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "adopted sibling failure"}}}
			return tx.stage(Write{Op: "put-task", Task: &failed})
		})
		if err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(release)
		awaitTaskSignal(t, picked.done)
		state, err := h.Snapshot(bg)
		if err != nil || state.Tasks[d].Status != "running" || taskAborted(state.Tasks[s]) || taskAborted(state.Tasks[d]) || nextEffects.Load() != 0 || aborts.Load() != 0 {
			t.Fatal("pre-mark descendant phase escaped", err)
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		if record := waitPublicTask(t, h, d); record.State.Outcome.Status != "aborted" {
			t.Fatal(record)
		}
		if nextEffects.Load() != 0 || aborts.Load() != 1 {
			t.Fatal("descendant abort effects", nextEffects.Load(), aborts.Load())
		}
	})
}
