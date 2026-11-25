package durable

import (
	"context"
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestToolExecutionParallelAndSequentialRounds(t *testing.T) {
	for _, mode := range []string{"parallel", "sequential", "tool-sequential", "v1-import"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				registry := NewRegistry()
				firstEntered, secondEntered, releaseFirst := make(chan struct{}), make(chan struct{}), make(chan struct{})
				var firstReturned atomic.Bool
				var secondPrepared atomic.Int64
				schema := json.RawMessage(`{"type":"object"}`)
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "first", Parameters: schema}, Implementation: "round.first", Version: 1, Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) {
					close(firstEntered)
					<-releaseFirst
					firstReturned.Store(true)
					return ToolResult{Content: "first result"}, nil
				}}); err != nil {
					t.Fatal(err)
				}
				toolMode := ""
				if mode == "tool-sequential" {
					toolMode = "sequential"
				}
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "second", Parameters: schema}, Implementation: "round.second", Version: 1, ExecutionMode: toolMode, PrepareArguments: func(_ context.Context, rawToolArguments any) (any, error) {
					args := rawToolArguments.(JSON)
					secondPrepared.Add(1)
					if (mode == "sequential" || mode == "tool-sequential") && !firstReturned.Load() {
						t.Error("sequential pending call prepared before predecessor returned")
					}
					return args, nil
				}, Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) {
					if mode != "parallel" && mode != "v1-import" && !firstReturned.Load() {
						t.Error("sequential tool overlapped")
					}
					close(secondEntered)
					return ToolResult{Content: "second result"}, nil
				}}); err != nil {
					t.Fatal(err)
				}
				var calls atomic.Int64
				var h *Harness
				requests := make(chan TaskRecord, 4)
				ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					ch := make(chan goai.Event, 1)
					state, err := h.Snapshot(bg)
					if err != nil {
						t.Error(err)
					}
					active := 0
					for id, task := range state.Tasks {
						if task.Kind != "pi.generation" || terminalStatus(task.Status) {
							continue
						}
						record, found, err := h.Task(bg, id)
						if err != nil || !found {
							t.Error("request task lookup", id, err)
							continue
						}
						if taskCheckpointMap(record.State.Checkpoint)["phase"] == "request" {
							active++
							requests <- record
						}
					}
					if active != 1 {
						t.Error("active canonical request cardinality", active)
					}
					if calls.Add(1) == 1 {
						answer := toolAnswer("first-call", "first", JSON{})
						answer.Message.Content = append(answer.Message.Content, goai.ContentBlock{Type: "toolCall", ID: "second-call", Name: "second", Arguments: map[string]any{}})
						ch <- answer
					} else {
						order := []string{}
						for _, message := range input.Messages {
							if message.Role == goai.RoleToolResult {
								order = append(order, message.ToolCallID)
							}
						}
						if fmt.Sprint(order) != "[first-call second-call]" {
							t.Error("tool result context order", order)
						}
						ch <- terminal("done")
					}
					close(ch)
					return ch
				})
				options.Registry = registry
				setting := mode
				if mode == "tool-sequential" || mode == "v1-import" {
					setting = "parallel"
				}
				options.Settings = &HarnessSettings{ToolExecution: setting}
				h = openHarness(t, b.store, options)
				cleanupTaskGates(t, releaseFirst)
				conversation, err := h.Root(bg, AgentChange{Model: ref})
				if err != nil {
					t.Fatal(err)
				}
				stream, err := h.WatchEvents(bg, conversation.ID())
				if err != nil {
					t.Fatal(err)
				}
				defer stream.Stop()
				var eventMu sync.Mutex
				var observed []AgentEvent
				ended := make(chan struct{})
				if err := stream.Start(func(_ context.Context, events []AgentEvent) error {
					eventMu.Lock()
					defer eventMu.Unlock()
					observed = append(observed, events...)
					for _, event := range events {
						if event.Type == "run_end" {
							select {
							case <-ended:
							default:
								close(ended)
							}
						}
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				if mode == "v1-import" {
					h.scheduler.sealed.Store(true)
				}
				sub, err := conversation.Submit(bg, Input{Content: "round"})
				if err != nil {
					t.Fatal(err)
				}
				if mode == "v1-import" {
					// Supplemental import control: change only the source version on
					// the real undispatched record; input/checkpoint stay untouched.
					_, err := h.Commit(bg, func(tx *Tx) (any, error) {
						for id, stored := range tx.state.Tasks {
							if stored.Kind != "pi.generation" || terminalStatus(stored.Status) {
								continue
							}
							record, found, err := tx.Task(id)
							if err != nil || !found {
								return nil, fmt.Errorf("import task lookup: %v", err)
							}
							record.Version = 1
							return nil, tx.setTask(record)
						}
						return nil, fmt.Errorf("import generation unavailable")
					})
					if err != nil {
						t.Fatal(err)
					}
					h.scheduler.sealed.Store(false)
					if err := h.Resume(bg); err != nil {
						t.Fatal(err)
					}
				}
				awaitTaskSignal(t, firstEntered)
				if mode == "sequential" || mode == "tool-sequential" {
					state, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					count := 0
					for _, task := range state.Tasks {
						if task.Kind == "pi.tool" {
							count++
						}
					}
					if count != 1 {
						t.Fatal("sequential round precreated later tool", count)
					}
				}

				if mode == "parallel" || mode == "v1-import" {
					awaitTaskSignal(t, secondEntered)
				}
				releaseTaskGate(releaseFirst)
				awaitTaskSignal(t, secondEntered)
				result := waitSubmission(t, sub)
				if result.Submission.Status != "done" || calls.Load() != 2 || secondPrepared.Load() != 1 {
					t.Fatal(result, calls.Load())
				}
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				var generations, tools []TaskRecord
				for _, task := range state.Tasks {
					if task.Conversation != conversation.ID() {
						continue
					}
					record, found, err := h.Task(bg, task.ID)
					if err != nil || !found {
						t.Fatal("canonical round task missing", task.ID, err)
					}
					switch record.Kind {
					case "pi.generation":
						generations = append(generations, record)
					case "pi.tool":
						tools = append(tools, record)
					}
				}
				// Fresh and version-1 imported records use the same source boundary.
				if len(generations) != 2 || len(tools) != 2 {
					t.Fatal("reference tool round task cardinality", generations, tools)
				}
				for _, generation := range generations {
					if generation.Owner != 0 || generation.State.Status != "terminal" || generation.State.Outcome == nil || generation.State.Outcome.Status != "completed" || generation.State.Checkpoint != nil {
						t.Fatal("generation not conversation-owned/completed", generation)
					}
				}
				firstRequest, secondRequest := watchReceive(t, requests), watchReceive(t, requests)
				for _, request := range []TaskRecord{firstRequest, secondRequest} {
					cp := taskCheckpointMap(request.State.Checkpoint)
					if request.Version != 3 || submissionEntryID(cp["attempt"]) != 1 || request.Input == nil || !request.Input.Present || !equalJSONValue(request.Input.Value, JSON{}) {
						t.Fatal("request is not a fresh canonical generation", request, cp)
					}
				}
				if firstRequest.ID == secondRequest.ID {
					t.Fatal("tool round reused its generation", firstRequest.ID)
				}
				owner := tools[0].Owner
				if owner == 0 || tools[1].Owner != owner || state.Tasks[owner].Kind != "pi.generation" {
					t.Fatal("tool ownership", tools)
				}
				for _, tool := range tools {
					if tool.State.Status != "terminal" || tool.State.Outcome == nil || tool.State.Outcome.Status != "completed" {
						t.Fatal("tool not completed before final outcome", tool)
					}
				}
				spent, found, err := h.Task(bg, owner)
				if err != nil || !found || owner != firstRequest.ID || spent.State.Outcome == nil || spent.State.Outcome.Result == nil {
					t.Fatal("spent generation result missing", spent, err)
				}
				assistant := submissionEntryID(generationObject(spent.State.Outcome.Result.Value)["entryId"])
				entry, exists, err := h.Entry(bg, assistant)
				if err != nil || !exists || entry.Conversation != conversation.ID() || entry.Kind != "pi.assistant" || len(entry.Model) != 1 || len(entry.Model[0].Content) != 2 || entry.Model[0].Content[0].ID != "first-call" || entry.Model[0].Content[1].ID != "second-call" {
					t.Fatal("tool round assistant entry", entry, err)
				}
				select {
				case <-ended:
				case <-time.After(3 * time.Second):
					t.Fatal("committed run-end event missing")
				}
				eventMu.Lock()
				defer eventMu.Unlock()
				var turnEvents []AgentEvent
				var runs, ends int
				for _, event := range observed {
					if event.Type == "turn_start" || event.Type == "turn_end" {
						turnEvents = append(turnEvents, event)
					}
					if event.Type == "run_start" {
						runs++
					}
					if event.Type == "run_end" {
						ends++
					}
				}
				if len(turnEvents) != 4 || turnEvents[0].Type != "turn_start" || turnEvents[1].Type != "turn_end" || turnEvents[2].Type != "turn_start" || turnEvents[3].Type != "turn_end" || turnEvents[1].Task != owner || turnEvents[2].Task != secondRequest.ID || turnEvents[1].Seq != turnEvents[2].Seq || runs != 1 || ends != 1 {
					t.Fatal("reference handoff turn/run events", turnEvents, runs, ends)
				}
			})
		})
	}
}

// Source ToolTaskResult points to the model receipt entry, not a copied receipt
// in a private checkpoint. Tests read that canonical record through public APIs.
func canonicalModelToolReceipt(t *testing.T, h *Harness, id ID) MessageReceipt {
	t.Helper()
	record, found, err := h.Task(bg, id)
	if err != nil || !found || record.Kind != "pi.tool" || record.State.Outcome == nil || record.State.Outcome.Result == nil || !record.State.Outcome.Result.Present {
		t.Fatal("model tool result missing", record, err)
	}
	result := generationObject(record.State.Outcome.Result.Value)
	entry, found, err := h.Entry(bg, submissionEntryID(result["entryId"]))
	if err != nil || !found || result["kind"] != "model" || entry.Kind != "pi.tool-result" || len(entry.Model) != 1 || entry.Model[0].Role != goai.RoleToolResult {
		t.Fatal("canonical tool result reference", result, entry, err)
	}
	return entry.Model[0]
}

// Source harness-tools.test.ts reads the host mode after the model response,
// then keeps that decision even if an executing tool changes the setting.
func TestToolRoundReadsLiveHostModeAndKeepsRoundDecision(t *testing.T) {
	for _, round := range []string{"sequential", "parallel"} {
		t.Run(round, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				before := "parallel"
				if round == "parallel" {
					before = "sequential"
				}
				registry := NewRegistry()
				var h *Harness
				firstEntered, secondEntered, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
				var firstReturned atomic.Bool
				var prepared atomic.Int64
				for _, name := range []string{"first", "second"} {
					name := name
					reg := wrapRegistration(name)
					reg.PrepareArguments = func(_ context.Context, rawToolArguments any) (any, error) {
						args := rawToolArguments.(JSON)
						if name == "second" {
							prepared.Add(1)
							if round == "sequential" && !firstReturned.Load() {
								t.Error("later sequential call prepared before predecessor ended")
							}
						}
						return args, nil
					}
					reg.Execute = func(context.Context, any, *ToolAPI) (ToolResult, error) {
						if name == "first" {
							if err := h.SetSettings(bg, HarnessSettings{ToolExecution: before}); err != nil {
								return ToolResult{}, err
							}
							close(firstEntered)
							<-release
							firstReturned.Store(true)
						} else {
							close(secondEntered)
						}
						return ToolResult{Content: name}, nil
					}
					if err := registry.Register(reg); err != nil {
						t.Fatal(err)
					}
				}
				var requests atomic.Int64
				ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
					ch := make(chan goai.Event, 1)
					if requests.Add(1) == 1 {
						if err := h.SetSettings(bg, HarnessSettings{ToolExecution: round}); err != nil {
							t.Error(err)
						}
						answer := toolAnswer("first-call", "first", JSON{})
						answer.Message.Content = append(answer.Message.Content, goai.ContentBlock{Type: "toolCall", ID: "second-call", Name: "second", Arguments: map[string]any{}})
						ch <- answer
					} else {
						ch <- terminal("done")
					}
					close(ch)
					return ch
				})
				options.Registry = registry
				options.Settings = &HarnessSettings{ToolExecution: before}
				h = openHarness(t, b.store, options)
				cleanupTaskGates(t, release)
				c := root(t, h, ref)
				sub, err := c.Submit(bg, Input{Content: "round"})
				if err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, firstEntered)
				wantTools := 1
				if round == "parallel" {
					awaitTaskSignal(t, secondEntered)
					wantTools = 2
				}
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				tools := 0
				for id, task := range state.Tasks {
					if task.Kind != "pi.tool" {
						continue
					}
					tools++
					record, found, err := h.Task(bg, id)
					if err != nil || !found || record.Kind != "pi.tool" || record.Version != 4 || record.Owner == 0 {
						t.Fatal("canonical tool ownership", record, err)
					}
				}
				if tools != wantTools {
					t.Fatal("round did not use the post-response host mode", tools, wantTools)
				}
				releaseTaskGate(release)
				awaitTaskSignal(t, secondEntered)
				if result := waitSubmission(t, sub); result.Record.Status != "done" || requests.Load() != 2 || prepared.Load() != 1 {
					t.Fatal("round completion", result.Record, requests.Load(), prepared.Load())
				}
			})
		})
	}
}

// Capture the actual accepted task write rather than polling for a parked task.
type toolParkTestStorage struct {
	Storage
	parked  chan ID
	waiting chan TaskRecord
	onTool  func(TaskRecord)
}

func (s *toolParkTestStorage) Commit(ctx context.Context, batches [][]StorageWrite, receive func(int, uint64)) (CommitResult, error) {
	return s.Storage.Commit(ctx, batches, func(index int, seq uint64) {
		for _, write := range batches[index] {
			if record, ok := recordValue[TaskRecord](write.Value); write.Type == "task" && ok && record.Kind == "pi.tool" {
				if s.onTool != nil {
					s.onTool(record)
				}
				if record.State.Status == "sleeping" {
					s.parked <- record.ID
				}
				if record.State.Status == "waiting" && s.waiting != nil {
					s.waiting <- record
				}
			}
		}
		if receive != nil {
			receive(index, seq)
		}
	})
}

func TestSourceV160CanonicalToolSignalPark(t *testing.T) {
	t.Run("live", func(t *testing.T) { testV160CanonicalToolSignalPark(t, false) })
	t.Run("reopened", func(t *testing.T) { testV160CanonicalToolSignalPark(t, true) })
}

func testV160CanonicalToolSignalPark(t *testing.T, reopen bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, 8*time.Second)
	defer cancel()
	native, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	storage := &toolParkTestStorage{Storage: native, parked: make(chan ID, 4)}
	done, err := DefineSignal(SignalDefinition{Name: "test.done", Version: 1, Schema: JSON{"type": "string"}})
	if err != nil {
		t.Fatal(err)
	}
	key, err := done.Key("done")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	seen := make(chan JSON, 2)
	ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, conversation *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if calls.Add(1) == 1 {
			ch <- toolAnswer("ci", "ci", JSON{})
		} else {
			for _, message := range conversation.Messages {
				if message.Role == goai.RoleToolResult && message.ToolCallID == "ci" {
					var text string
					for _, block := range message.Content {
						text += block.Text
					}
					if message.IsError || !strings.Contains(text, "passed") || !strings.Contains(text, "run 1") || strings.Contains(text, "old progress") {
						t.Errorf("resumed tool result = %+v / %q", message, text)
					}
				}
			}
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	wakeEvents := make(chan Wake, 32)
	options.OnWake = func(w Wake) { wakeEvents <- w }
	options.Registry = NewRegistry()
	if err := options.Registry.Register(ToolRegistration{Definition: goai.Tool{Name: "ci", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "test.ci", Version: 1,
		Execute: func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
			seen <- JSON{"wake": api.Wake, "checkpoint": api.Checkpoint}
			if api.Wake == nil {
				if err := api.Output("old progress"); err != nil {
					return ToolResult{}, err
				}
				if err := api.Details(JSON{"build": 1}); err != nil {
					return ToolResult{}, err
				}
				if err := api.Diagnostic(ToolDiagnostic{Severity: "info", Message: "run 1"}); err != nil {
					return ToolResult{}, err
				}
				return ToolResult{Park: &ToolPark{Sleep: &ToolSleep{Signals: []SignalKey{key}, Checkpoint: JSON{"build": 1}}}}, nil
			}
			state, err := api.ReadSignal(ctx, done, "done")
			if err != nil || state == nil {
				return ToolResult{}, fmt.Errorf("signal state = %v: %v", state, err)
			}
			return ToolResult{Content: fmt.Sprint(state.Value)}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, storage, options)
	conversation := root(t, h, ref)
	sub, err := conversation.Submit(ctx, Input{Type: "input", Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	var id ID
	select {
	case id = <-storage.parked:
	case <-ctx.Done():
		t.Fatal("tool did not park", ctx.Err())
	}
	record, found, err := h.Task(ctx, id)
	if err != nil || !found || record.Version != 4 || record.State.Status != "sleeping" || record.State.Until != nil || !reflect.DeepEqual(record.State.Signals, []SignalKey{key}) || !equalJSONValue(record.State.Checkpoint, JSON{"phase": "parked", "arguments": JSON{}, "replay": "unsafe", "checkpoint": JSON{"build": 1}}) {
		t.Fatalf("parked task = %+v, %v", record, err)
	}
	state, err := h.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	live := mainContinueBoundaryDocument(t, state, conversation.ID(), "pi.live")
	slot := canonicalToolSlot(live, id)
	if slot["status"] != "sleeping" || !equalJSONValue(slot["details"], JSON{"build": 1}) || slot["output"] != nil || len(slot["diagnostics"].([]any)) != 1 {
		t.Fatalf("parked slot = %v", slot)
	}
	awaitWake140(t, wakeEvents, false, nil)
	if reopen {
		submissionID := sub.ID()
		if err := h.Close(ctx); err != nil {
			t.Fatal(err)
		}
		reopened := &toolParkTestStorage{Storage: reopenStoreAfterHarnessClose(t, native), parked: make(chan ID, 4)}
		wakeEvents = make(chan Wake, 32)
		options.OnWake = func(w Wake) { wakeEvents <- w }
		h = openHarness(t, reopened, options)
		if wake, err := h.Wake(ctx); err != nil || wake != (Wake{}) || len(seen) != 1 {
			t.Fatalf("reopened park = %+v, %v; runs=%d", wake, err, len(seen))
		}
		sub, err = h.Submission(ctx, submissionID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if result, err := h.Signal(ctx, id, done, "done", "passed"); err != nil || result != SignalDelivered {
		t.Fatalf("signal = %q, %v", result, err)
	}
	if result, err := sub.Wait(ctx); err != nil || result.Record.Status != "done" {
		t.Fatalf("submission = %+v, %v", result.Record, err)
	}
	if len(seen) != 2 {
		t.Fatalf("tool invocations = %d; want two", len(seen))
	}
	first, second := <-seen, <-seen
	if first["wake"] != (*WakeReason)(nil) || first["checkpoint"] != nil || !reflect.DeepEqual(second["wake"], &WakeReason{Reason: "signal", Signal: "test.done", Key: "done"}) || !equalJSONValue(second["checkpoint"], JSON{"build": 1}) || calls.Load() != 2 {
		t.Fatalf("calls = %v, %v; provider calls=%d", first, second, calls.Load())
	}
}

// harness-tool-parks.test.ts:248-289. The tool parks on its own child, not
// on a signal directly. Separate receipts identify the committed wait and sleep.
func TestSourceV160ParkedToolWakesOnOwnedChildAny(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 8*time.Second)
	defer cancel()
	native, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	storage := &toolParkTestStorage{Storage: native, waiting: make(chan TaskRecord, 2), parked: make(chan ID, 2)}
	token := testSignalAnswer(t)
	key, err := token.Key("child")
	if err != nil {
		t.Fatal(err)
	}
	sleeping := make(chan ID, 1)
	childDef, err := DefineTask(TaskDefinitionOptions{Kind: "test.child", Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "sleep"}, nil }, Abort: abortTestSignalTask,
		Phases: map[string]TaskPhase{
			"sleep": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "sleeping", Checkpoint: JSON{"phase": "done"}, Signals: []SignalKey{key}}, nil
				})
				if err == nil {
					sleeping <- task.ID
				}
				return err
			},
			"done": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("child done"), nil })
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			ch <- toolAnswer("c1", "spawn", JSON{})
		} else {
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	options.Registry = NewRegistry()
	if _, err := options.Registry.RegisterTask(childDef); err != nil {
		t.Fatal(err)
	}
	type attempt struct {
		id   ID
		wake *WakeReason
	}
	runs := make(chan attempt, 4)
	spawn := wrapRegistration("spawn") // Replay-safe, as in the pinned source.
	spawn.Execute = func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
		runs <- attempt{api.TaskID(), api.Wake}
		if api.Wake != nil {
			return ToolResult{Content: "joined"}, nil
		}
		child, err := api.CreateTask(ctx, childDef, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: api.TaskID()}})
		if err != nil {
			return ToolResult{}, err
		}
		return ToolResult{Park: &ToolPark{Wait: &ToolWait{On: []ID{child}, Policy: "any"}}}, nil
	}
	if err := options.Registry.Register(spawn); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, storage, options)
	c := root(t, h, ref)
	sub, err := c.Submit(ctx, Input{Type: "input", Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	waiting := watchReceive(t, storage.waiting)
	child := watchReceive(t, sleeping)
	if waiting.State.Status != "waiting" || waiting.State.Policy != "any" || !slices.Equal(waiting.State.On, []ID{child}) {
		t.Fatal("parked tool did not wait for its one child with any", waiting)
	}
	stored, found, err := native.Task(ctx, child)
	if err != nil || !found || stored.Owner != waiting.ID || stored.Conversation != c.ID() || stored.State.Status != "sleeping" || !slices.Equal(stored.State.Signals, []SignalKey{key}) {
		t.Fatal("wrong stored child owner or sleep", stored, found, err)
	}
	live, found, err := c.SnapshotDefinition(ctx, LiveDoc(), nil)
	if err != nil || !found {
		t.Fatal(live, found, err)
	}
	if slot := canonicalToolSlot(generationObject(live.Copy()), waiting.ID); slot["status"] != "waiting" {
		t.Fatalf("tool slot after parked receipt = %v, want waiting", slot)
	}
	if got, err := h.Signal(ctx, child, token, key.Key, JSON{"text": "go"}); err != nil || got != SignalDelivered {
		t.Fatalf("wake child = %q, %v", got, err)
	}
	if got, err := sub.Wait(ctx); err != nil || got.Record.Status != "done" {
		t.Fatalf("submission with owned child wait = %+v, %v", got.Record, err)
	}
	if len(runs) != 2 {
		t.Fatalf("tool wait ran %d times, want two", len(runs))
	}
	first, second := <-runs, <-runs
	if first.id != waiting.ID || second.id != waiting.ID || first.wake != nil || !reflect.DeepEqual(second.wake, &WakeReason{Reason: "tasks", Ended: []ID{child}}) {
		t.Fatalf("owned child wake: first=%+v second=%+v wake=%+v; want [%d]", first, second, second.wake, child)
	}
	if done, err := h.WaitForTask(ctx, child); err != nil || done.State.Status != "terminal" || done.State.Outcome == nil || done.State.Outcome.Status != "completed" || done.State.Outcome.Result == nil || !done.State.Outcome.Result.Present || done.State.Outcome.Result.Value != "child done" {
		t.Fatal("owned child outcome", done, err)
	}
	if done, err := h.WaitForTask(ctx, waiting.ID); err != nil || done.State.Status != "terminal" || done.State.Outcome == nil || done.State.Outcome.Status != "completed" {
		t.Fatal("parked tool outcome", done, err)
	}
	if result := v160ToolResult(t, ctx, c, "c1"); result.IsError || v160ToolResultText(result) != "joined" {
		t.Fatal("parked tool transcript result", result)
	}
}

// harness-tool-parks.test.ts:293-342 covers the invalid and reused cases;
// the timed case retains its separate success-path coverage.
func TestSourceV160CanonicalToolTimedAndInvalidParks(t *testing.T) {
	for _, mode := range []string{"time", "mixed", "result", "empty", "unowned", "reused"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(bg, 8*time.Second)
			defer cancel()
			native, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			storage := &toolParkTestStorage{Storage: native, parked: make(chan ID, 4)}
			token := testSignalAnswer(t)
			key, err := token.Key("k")
			if err != nil {
				t.Fatal(err)
			}
			var clock atomic.Int64
			clock.Store(1000)
			var providerCalls atomic.Int32
			observed := make(chan goai.Message, 2)
			name := mode
			if mode == "reused" {
				name = "again"
			}
			ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, conversation *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				if providerCalls.Add(1) == 1 {
					ch <- toolAnswer("call", name, JSON{})
				} else {
					for _, msg := range conversation.Messages {
						if msg.Role == goai.RoleToolResult && msg.ToolCallID == "call" {
							observed <- msg
						}
					}
					ch <- terminal("done")
				}
				close(ch)
				return ch
			})
			wakeEvents := make(chan Wake, 32)
			options.Now, options.OnWake, options.Registry = clock.Load, func(w Wake) { wakeEvents <- w }, NewRegistry()
			wakes, taskIDs := make(chan *WakeReason, 3), make(chan ID, 3)
			err = options.Registry.Register(ToolRegistration{Definition: goai.Tool{Name: name, Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "park." + mode, Version: 1,
				Execute: func(_ context.Context, _ any, api *ToolAPI) (ToolResult, error) {
					wakes <- api.Wake
					taskIDs <- api.TaskID()
					until := int64(1)
					switch mode {
					case "time":
						until = 1500
						if api.Wake != nil {
							return ToolResult{Blocks: []goai.ContentBlock{}}, nil
						}
						return ToolResult{Park: &ToolPark{Sleep: &ToolSleep{Until: &until}}}, nil
					case "mixed":
						return ToolResult{Park: &ToolPark{Sleep: &ToolSleep{Until: &until}, Wait: &ToolWait{On: []ID{}, Policy: "any"}}}, nil
					case "result":
						return ToolResult{Park: &ToolPark{Sleep: &ToolSleep{Until: &until}}, Blocks: []goai.ContentBlock{}}, nil
					case "empty":
						return ToolResult{Park: &ToolPark{Sleep: &ToolSleep{}}}, nil
					case "unowned":
						return ToolResult{Park: &ToolPark{Wait: &ToolWait{On: []ID{1}, Policy: "any"}}}, nil
					default:
						return ToolResult{Park: &ToolPark{Sleep: &ToolSleep{Signals: []SignalKey{key}}}}, nil
					}
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			type hookReceipt struct {
				invocation ToolInvocation
				result     ToolResult
			}
			hooks := make(chan hookReceipt, 3)
			if mode != "time" {
				if err := options.Registry.Install(&Extension{Name: "inspect-invalid-park", ToolHooks: ToolHooks{AfterTool: func(ctx context.Context, _ goai.ToolCall, result ToolResult, api *HookAPI) (*ToolResult, error) {
					invocation, err := api.ToolInvocation(ctx)
					if err != nil {
						return nil, err
					}
					hooks <- hookReceipt{invocation, result}
					result.Details = JSON{"by": "afterTool"}
					return &result, nil
				}}}); err != nil {
					t.Fatal(err)
				}
			}
			h := openHarness(t, storage, options)
			c := root(t, h, ref)
			sub, err := c.Submit(ctx, Input{Type: "input", Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "time" || mode == "reused" {
				var id ID
				select {
				case id = <-storage.parked:
				case <-ctx.Done():
					t.Fatal("tool did not park", ctx.Err())
				}
				if mode == "time" {
					until := int64(1500)
					awaitWake140(t, wakeEvents, false, &until)
					clock.Store(1500)
					if err := h.Resume(ctx); err != nil {
						t.Fatal(err)
					}
				} else {
					if got, err := h.Signal(ctx, id, token, key.Key, JSON{"text": "x"}); err != nil || got != SignalDelivered {
						t.Fatalf("signal = %v, %v", got, err)
					}
				}
			}
			if result, err := sub.Wait(ctx); err != nil || result.Record.Status != "done" {
				t.Fatalf("submission = %+v, %v", result.Record, err)
			}
			select {
			case msg := <-observed:
				if mode == "time" {
					if msg.IsError || msg.DurationMs == nil || *msg.DurationMs != 500 {
						t.Fatalf("timed result = %+v", msg)
					}
					if first, second := <-wakes, <-wakes; first != nil || !reflect.DeepEqual(second, &WakeReason{Reason: "time"}) {
						t.Fatalf("timed wakes = %+v, %+v", first, second)
					}
				} else {
					var text string
					for _, block := range msg.Content {
						text += block.Text
					}
					if !msg.IsError || !strings.Contains(text, "Tool "+name+" returned an invalid park") {
						t.Fatalf("invalid model result = %+v / %s", msg, text)
					}
				}
			case <-ctx.Done():
				t.Fatal("missing tool result", ctx.Err())
			}
			if mode == "time" {
				return
			}
			wantRuns := 1
			if mode == "reused" {
				wantRuns = 2
			}
			if len(taskIDs) != wantRuns {
				t.Fatalf("invalid park tool runs = %d, want %d", len(taskIDs), wantRuns)
			}
			id := <-taskIDs
			if mode == "reused" {
				if second := <-taskIDs; second != id {
					t.Fatalf("repark used another task: %d != %d", second, id)
				}
			}
			message := "Tool " + name + " returned an invalid park"
			result := v160ToolResult(t, ctx, c, "call")
			if !result.IsError || !equalJSONValue(result.Details, JSON{"by": "afterTool"}) || !strings.Contains(v160ToolResultText(result), message) {
				t.Fatalf("invalid park transcript result or hook details lost: %+v", result)
			}
			if mode == "reused" && !strings.Contains(v160ToolResultText(result), "it already woke a wait") {
				t.Fatalf("repark did not report consumed wake: %q", v160ToolResultText(result))
			}
			if len(hooks) != 1 {
				t.Fatalf("invalid park afterTool calls = %d, want one", len(hooks))
			}
			hook := <-hooks
			if hook.invocation.Task != id || hook.invocation.CallID != "call" || !hook.result.IsError || len(hook.result.Diagnostics) != 1 || hook.result.Diagnostics[0].Code != "invalid_park" || !strings.HasPrefix(hook.result.Diagnostics[0].Message, message+": ") {
				t.Fatalf("afterTool did not receive original invalid park on task %d: %+v", id, hook)
			}
			task, err := h.WaitForTask(ctx, id)
			outcome := task.State.Outcome
			if err != nil || task.State.Status != "terminal" || outcome == nil || outcome.Status != "failed" || outcome.Error == nil || outcome.Error.Message != message || outcome.Result == nil || !outcome.Result.Present {
				t.Fatalf("invalid park task = %+v, %v; want terminal failed", task, err)
			}
			entryID := submissionEntryID(generationObject(outcome.Result.Value)["entryId"])
			stored, found, err := native.Entry(ctx, entryID)
			diagnostics := []any{JSON{"severity": "error", "code": "invalid_park", "message": hook.result.Diagnostics[0].Message}}
			if err != nil || !found || stored.CommitSeq == 0 || stored.Entry.ByTask != id || !equalJSONValue(generationObject(stored.Entry.Data)["diagnostics"], diagnostics) {
				t.Fatal("invalid park stored diagnostic or task link", stored, found, err)
			}
			live, found, err := c.SnapshotDefinition(ctx, LiveDoc(), nil)
			if err != nil || !found {
				t.Fatal(live, found, err)
			}
			slots, _ := generationObject(live.Copy())["tools"].([]any)
			if len(slots) != 0 {
				t.Fatal("invalid park retained a live tool slot", slots)
			}
		})
	}
}

// harness-tool-parks.test.ts:345-490. A durable park receipt or Execute
// entry gates abort. Registry.Remove disposes the tool.
func TestSourceV160CanonicalToolCancel(t *testing.T) {
	for _, mode := range []string{"parked", "throws-at-limit", "woken", "removed", "racing-park"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(bg, 8*time.Second)
			defer cancel()
			native, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			storage := &toolParkTestStorage{Storage: native, parked: make(chan ID, 4)}
			token := testSignalAnswer(t)
			keyName := "done"
			if mode == "racing-park" {
				keyName = "k"
			}
			key, err := token.Key(keyName)
			if err != nil {
				t.Fatal(err)
			}
			var providerCalls atomic.Int32
			ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				if providerCalls.Add(1) == 1 {
					ch <- toolAnswer("c1", "ci", JSON{"job": "build"})
				} else {
					ch <- terminal("done")
				}
				close(ch)
				return ch
			})
			options.Registry = NewRegistry()
			options.Env = func(context.Context, EnvTarget) (ExecutionEnvironment, error) {
				return testEnvironment{cwd: "/work"}, nil
			}
			if mode == "throws-at-limit" {
				limit := uint64(1)
				options.Settings = &HarnessSettings{RestartLimit: &limit}
			}
			reports := make(chan error, 32)
			options.OnReport = func(err error) { reports <- err }
			type cancellation struct {
				wake             *WakeReason
				checkpoint, args any
				cwd              string
				err              error
			}
			cleanups := make(chan cancellation, 4)
			entered := make(chan ID, 4)
			cutOff := make(chan string, 1)
			cleanupStarted, releaseCleanup := make(chan struct{}, 1), make(chan struct{})
			abortMarked, releasePark := make(chan ID, 1), make(chan struct{})
			wantCheckpoint := any(JSON{"build": 1})
			switch mode {
			case "woken":
				wantCheckpoint = "first"
			case "racing-park":
				wantCheckpoint = "started"
				var once sync.Once
				storage.onTool = func(record TaskRecord) {
					if record.AbortRequested {
						once.Do(func() { abortMarked <- record.ID })
					}
				}
			}
			err = options.Registry.Register(ToolRegistration{
				Definition:     goai.Tool{Name: "ci", Parameters: json.RawMessage(`{"type":"object","properties":{"job":{"type":"string"}},"required":["job"]}`)},
				Implementation: "test.cancel", Version: 1,
				Execute: func(run context.Context, _ any, api *ToolAPI) (ToolResult, error) {
					if mode == "racing-park" {
						entered <- api.TaskID()
						// Ignore the run cancellation. Return the park only when the
						// test has read the actual abort-marked running receipt.
						select {
						case <-releasePark:
						case <-ctx.Done():
							return ToolResult{}, ctx.Err()
						}
						cutOff <- "execute"
					} else if api.Wake != nil {
						entered <- api.TaskID()
						select {
						case <-run.Done():
						case <-ctx.Done():
							return ToolResult{}, ctx.Err()
						}
						cutOff <- "execute"
						return ToolResult{}, run.Err()
					}
					return ToolResult{Park: &ToolPark{Sleep: &ToolSleep{Signals: []SignalKey{key}, Checkpoint: wantCheckpoint}}}, nil
				},
				Cancel: func(cleanup context.Context, args any, api *ToolAPI) error {
					if err := cleanup.Err(); err != nil {
						t.Errorf("park cleanup inherited a cancelled context: %v", err)
					}
					if mode == "woken" || mode == "racing-park" {
						select {
						case phase := <-cutOff:
							if phase != "execute" {
								t.Errorf("unexpected aborted phase: %q", phase)
							}
						default:
							t.Error("cancel ran before the cut-off execute returned")
						}
					}
					env, err := api.Environment(cleanup)
					var cwd string
					if err == nil && env != nil {
						cwd = env.Cwd()
					}
					cleanups <- cancellation{api.Wake, api.Checkpoint, args, cwd, err}
					message := "Tool ci cannot start nested calls or tasks in cancel"
					if _, err := api.StartToolAuto(cleanup, "ci", JSON{}); err == nil || err.Error() != message {
						t.Errorf("cancel nested start = %v", err)
					}
					if _, err := api.CallTool(cleanup, "key", "ci", JSON{}); err == nil || err.Error() != message {
						t.Errorf("cancel blocking nested call = %v", err)
					}
					if _, err := api.CallToolAuto(cleanup, "ci", JSON{}); err == nil || err.Error() != message {
						t.Errorf("cancel blocking automatic nested call = %v", err)
					}
					if _, err := api.CreateTask(cleanup, ToolTask(), JSON{}, TaskOptions{}); err == nil || err.Error() != message {
						t.Errorf("cancel task start = %v", err)
					}
					if mode == "parked" || mode == "throws-at-limit" {
						cleanupStarted <- struct{}{}
						select {
						case <-releaseCleanup:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
					if mode == "throws-at-limit" {
						panic("cancel failed")
					}
					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, storage, options)
			defer releaseTaskGate(releasePark)
			if mode == "throws-at-limit" && h.RestartLimit() != 1 {
				t.Fatalf("restart limit = %d, want 1", h.RestartLimit())
			}
			c := root(t, h, ref)
			sub, err := c.Submit(ctx, Input{Type: "input", Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			ready := storage.parked
			if mode == "racing-park" {
				ready = entered
			}
			var id ID
			select {
			case id = <-ready:
			case <-ctx.Done():
				t.Fatal("tool not ready", ctx.Err())
			}
			switch mode {
			case "woken":
				if result, err := h.Signal(ctx, id, token, "done", JSON{"text": "passed"}); err != nil || result != SignalDelivered {
					t.Fatalf("signal = %q, %v", result, err)
				}
				select {
				case woke := <-entered:
					if woke != id {
						t.Fatalf("woke task %d, want %d", woke, id)
					}
				case <-ctx.Done():
					t.Fatal("tool did not wake", ctx.Err())
				}
			case "removed":
				parked, found, err := h.Task(ctx, id)
				if err != nil || !found || parked.State.Status != "sleeping" || !slices.Equal(parked.State.Signals, []SignalKey{key}) {
					t.Fatal("parked before registry removal", parked, found, err)
				}
				options.Registry.Remove("ci")
			case "racing-park":
				type abortResult struct {
					mark string
					err  error
				}
				aborting := make(chan abortResult, 1)
				go func() {
					mark, err := h.AbortTask(ctx, id)
					aborting <- abortResult{mark, err}
				}()
				if marked := watchReceive(t, abortMarked); marked != id {
					t.Fatalf("abort marked task %d, want %d", marked, id)
				}
				marked, found, err := h.Task(ctx, id)
				if err != nil || !found || marked.State.Status != "running" || !marked.AbortRequested {
					t.Fatal("expected marked running task before park return", marked, found, err)
				}
				close(releasePark)
				if got := watchReceive(t, aborting); got.err != nil || got.mark != "marked" {
					t.Fatal("task abort result", got)
				}
				record, err := h.WaitForTask(ctx, id)
				if err != nil || record.State.Outcome == nil || record.State.Outcome.Status != "aborted" || len(storage.parked) != 0 {
					t.Fatalf("refused park persisted or task did not abort: %+v, %v; parked receipts=%d", record, err, len(storage.parked))
				}
			}
			if mode == "parked" || mode == "throws-at-limit" {
				defer releaseTaskGate(releaseCleanup)
				aborted := make(chan error, 1)
				go func() { aborted <- c.Abort(ctx) }()
				select {
				case <-cleanupStarted:
				case err := <-aborted:
					t.Fatalf("abort returned without entering park cleanup: %v", err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				record, found, err := h.Task(ctx, id)
				if err != nil || !found || !record.AbortRequested || record.State.Status == "terminal" {
					t.Fatal("parked task settled before cleanup returned", record, found, err)
				}
				select {
				case err := <-aborted:
					t.Fatalf("abort returned while cleanup was held: %v", err)
				default:
				}
				close(releaseCleanup)
				if err := watchReceive(t, aborted); err != nil {
					t.Fatal(err)
				}
			} else if err := c.Abort(ctx); err != nil {
				t.Fatal(err)
			}
			settled, err := sub.Wait(ctx)
			if err != nil || (mode != "racing-park" && settled.Record.Status != "unanswered") {
				t.Fatalf("submission = %+v, %v", settled.Record, err)
			}
			record, err := h.WaitForTask(ctx, id)
			if err != nil || record.State.Outcome == nil || record.State.Outcome.Status != "aborted" {
				t.Fatalf("task = %+v, %v", record, err)
			}
			if mode == "parked" {
				result := v160ToolResult(t, ctx, c, "c1")
				if !result.IsError || !strings.Contains(v160ToolResultText(result), "Tool ci was aborted") {
					t.Fatalf("tool result = %+v", result)
				}
			}
			if mode == "removed" {
				if len(cleanups) != 0 {
					t.Fatal("removed tool ran cancel")
				}
			} else {
				if len(cleanups) != 1 {
					t.Fatalf("cancel calls = %d; want one", len(cleanups))
				}
				got := <-cleanups
				if got.wake != nil || !equalJSONValue(got.checkpoint, wantCheckpoint) || !equalJSONValue(got.args, JSON{"job": "build"}) || got.err != nil || got.cwd != "/work" {
					t.Fatalf("cancel = %+v, want checkpoint %v and cwd /work", got, wantCheckpoint)
				}
				if (mode == "woken" || mode == "racing-park") && len(cutOff) != 0 {
					t.Fatal("execute did not clean up exactly once before park cancellation")
				}
			}
			wantReport := map[string]string{"removed": "Tool ci is not available: its parked call c1 was not cancelled", "throws-at-limit": "cancel failed"}[mode]
			if wantReport != "" {
				for {
					select {
					case got := <-reports:
						if got.Error() == wantReport {
							return
						}
					case <-ctx.Done():
						t.Fatalf("no report %q", wantReport)
					}
				}
			}
		})
	}
}

// Source: harness-park-children.test.ts, nonblocking nested calls and unsafe
// parents that were parked (rather than running) when the writer closed.
func TestSourceV160ParkedToolKeepsNestedChildren(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		t.Run(fmt.Sprintf("reopen=%v", reopen), func(t *testing.T) { testV160ParkedToolKeepsNestedChildren(t, reopen) })
	}
}

func testV160ParkedToolKeepsNestedChildren(t *testing.T, reopen bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, 10*time.Second)
	defer cancel()
	native, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	storage := &toolParkTestStorage{Storage: native, parked: make(chan ID, 12), waiting: make(chan TaskRecord, 12)}
	token := testSignalAnswer(t)
	key, err := token.Key("done")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if calls.Add(1) == 1 {
			ch <- toolAnswer("c1", "both", JSON{})
		} else {
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	wakes := make(chan *WakeReason, 6)
	idle := make(chan Wake, 64)
	options.OnWake = func(w Wake) { idle <- w }
	options.Registry = NewRegistry()
	if err := options.Registry.Register(ToolRegistration{Definition: goai.Tool{Name: "research", Parameters: json.RawMessage(`{"type":"object","properties":{"topic":{"type":"string"}},"required":["topic"]}`)}, Implementation: "park.child", Version: 1,
		Execute: func(run context.Context, args any, api *ToolAPI) (ToolResult, error) {
			state, err := api.ReadSignal(run, token, "done")
			if err != nil {
				return ToolResult{}, err
			}
			if state == nil {
				return ToolResult{Park: &ToolPark{Sleep: &ToolSleep{Signals: []SignalKey{key}}}}, nil
			}
			topic := generationObject(args)["topic"]
			text := generationObject(state.Value)["text"]
			return ToolResult{Content: fmt.Sprintf("%s: %s", topic, text)}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := options.Registry.Register(ToolRegistration{Definition: goai.Tool{Name: "both", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "park.both", Version: 1, ReplaySafe: !reopen,
		Execute: func(run context.Context, _ any, api *ToolAPI) (ToolResult, error) {
			wakes <- api.Wake
			var handles []*NestedToolHandle
			for _, key := range []string{"a", "b"} {
				if reopen && key == "b" {
					break
				}
				topic := key
				if reopen {
					topic = "x"
				}
				h, err := api.StartTool(run, key, "research", JSON{"topic": topic})
				if err != nil {
					return ToolResult{}, err
				}
				handles = append(handles, h)
			}
			done := generationTaskIDs(generationObject(api.Checkpoint)["done"])
			if api.Wake != nil {
				done = append(done, api.Wake.Ended...)
			}
			var on []ID
			for _, h := range handles {
				if !slices.Contains(done, h.ID()) {
					on = append(on, h.ID())
				}
			}
			if len(on) != 0 {
				values := make([]any, len(done))
				for i, id := range done {
					values[i] = id
				}
				return ToolResult{Park: &ToolPark{Wait: &ToolWait{On: on, Policy: "any", Checkpoint: JSON{"done": values}}}}, nil
			}
			var results []string
			for _, h := range handles {
				result, err := h.Wait(run)
				if err != nil {
					return ToolResult{}, err
				}
				if result.IsError {
					return ToolResult{}, fmt.Errorf("nested result failed: %v", result.Diagnostics)
				}
				results = append(results, fmt.Sprint(result.StructuredOutput))
			}
			return ToolResult{Content: strings.Join(results, " + ")}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, storage, options)
	c := root(t, h, ref)
	sub, err := c.Submit(ctx, Input{Type: "input", Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	waitParent := func() TaskRecord {
		select {
		case task := <-storage.waiting:
			return task
		case <-ctx.Done():
			t.Fatal("parent not parked", ctx.Err())
			return TaskRecord{}
		}
	}
	parent := waitParent()
	ids := append([]ID(nil), parent.State.On...)
	if len(ids) != map[bool]int{true: 1, false: 2}[reopen] {
		t.Fatalf("wait ids = %v", ids)
	}
	for range ids {
		select {
		case id := <-storage.parked:
			if !slices.Contains(ids, id) {
				t.Fatalf("unexpected child %d", id)
			}
		case <-ctx.Done():
			t.Fatal("child not parked", ctx.Err())
		}
	}
	awaitWake140(t, idle, false, nil)
	if reopen {
		id := sub.ID()
		if err := h.Close(ctx); err != nil {
			t.Fatal(err)
		}
		storage = &toolParkTestStorage{Storage: reopenStoreAfterHarnessClose(t, native), parked: make(chan ID, 12), waiting: make(chan TaskRecord, 12)}
		options.OnWake = nil
		h = openHarness(t, storage, options)
		sub, err = h.Submission(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		c, err = h.Conversation(ctx, c.ID())
		if err != nil {
			t.Fatal(err)
		}
		child, found, err := h.Task(ctx, ids[0])
		if err != nil || !found || child.Owner != parent.ID || child.AbortRequested || !child.AbandonOnRestart || child.State.Status != "sleeping" {
			t.Fatalf("retained child = %+v, %v", child, err)
		}
	} else {
		if got, err := h.Signal(ctx, ids[1], token, "done", JSON{"text": "bee"}); err != nil || got != SignalDelivered {
			t.Fatalf("signal b = %q, %v", got, err)
		}
		next := waitParent()
		if !reflect.DeepEqual(next.State.On, ids[:1]) {
			t.Fatalf("remaining children = %v", next.State.On)
		}
	}
	text := "ay"
	if reopen {
		text = "kept"
	}
	if got, err := h.Signal(ctx, ids[0], token, "done", JSON{"text": text}); err != nil || got != SignalDelivered {
		t.Fatalf("signal a = %q, %v", got, err)
	}
	if got, err := sub.Wait(ctx); err != nil || got.Record.Status != "done" {
		t.Fatalf("submission = %+v, %v", got.Record, err)
	}
	want := "a: ay + b: bee"
	if reopen {
		want = "x: kept"
	}
	result := v160ToolResult(t, ctx, c, "c1")
	if result.IsError || v160ToolResultText(result) != want {
		t.Fatalf("result = %q", v160ToolResultText(result))
	}
	var seen []*WakeReason
	for len(wakes) > 0 {
		seen = append(seen, <-wakes)
	}
	expected := []*WakeReason{nil}
	for i := len(ids) - 1; i >= 0; i-- {
		expected = append(expected, &WakeReason{Reason: "tasks", Ended: []ID{ids[i]}})
	}
	if !reflect.DeepEqual(seen, expected) {
		t.Fatalf("tool wakes = %+v, want %+v", seen, expected)
	}
}

// Source: harness-park-children.test.ts, explicit keys after wake and shared
// membership between keyed tasks and nested calls.
func TestSourceV160ToolChildKeys(t *testing.T) {
	for _, afterWake := range []bool{false, true} {
		t.Run(fmt.Sprintf("wake=%v", afterWake), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(bg, 8*time.Second)
			defer cancel()
			var calls, initialized atomic.Int32
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				if calls.Add(1) == 1 {
					ch <- toolAnswer("c1", "keyed", JSON{})
				} else {
					ch <- terminal("done")
				}
				close(ch)
				return ch
			})
			child, err := DefineTask(TaskDefinitionOptions{Kind: "test.keyed", Version: 1, Initial: func(any) (JSON, error) { initialized.Add(1); return JSON{"phase": "run"}, nil },
				Phases: map[string]TaskPhase{"run": func(run context.Context, _ TaskRecord, r *TaskRuntime) error {
					return r.Commit(run, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true}}}, nil
					})
				}},
				Abort: func(run context.Context, _ TaskRecord, r *TaskRuntime) error {
					return r.Commit(run, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
					})
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			options.Registry = NewRegistry()
			if _, err := options.Registry.RegisterTask(child); err != nil {
				t.Fatal(err)
			}
			seen := make(chan []any, 1)
			if err := options.Registry.Register(ToolRegistration{Definition: goai.Tool{Name: "keyed", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "keyed.child", Version: 1,
				Execute: func(run context.Context, _ any, api *ToolAPI) (ToolResult, error) {
					own := TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: api.TaskID()}}
					if afterWake {
						if api.Wake == nil {
							until := int64(0)
							return ToolResult{Park: &ToolPark{Sleep: &ToolSleep{Until: &until}}}, nil
						}
						_, toolErr := api.StartToolAuto(run, "research", JSON{})
						_, taskErr := api.CreateTask(run, child, nil, own)
						seen <- []any{fmt.Sprint(toolErr), fmt.Sprint(taskErr)}
						return ToolResult{Content: "done"}, nil
					}
					own.Key = "k"
					first, err := api.CreateTask(run, child, JSON{"n": 1}, own)
					if err != nil {
						return ToolResult{}, err
					}
					again, err := api.CreateTask(run, child, JSON{"n": 1}, own)
					if err != nil {
						return ToolResult{}, err
					}
					_, wrong := api.CreateTask(run, child, JSON{"n": 2}, own)
					if _, err := api.StartTool(run, "n", "research", JSON{}); err != nil {
						return ToolResult{}, err
					}
					own.Key = "n"
					_, nestedKey := api.CreateTask(run, child, JSON{"n": 1}, own)
					_, taskKey := api.StartTool(run, "k", "research", JSON{})
					seen <- []any{first, again, fmt.Sprint(wrong), fmt.Sprint(nestedKey), fmt.Sprint(taskKey)}
					return ToolResult{Content: "done"}, nil
				},
			}); err != nil {
				t.Fatal(err)
			}
			native, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, native, options)
			c := root(t, h, ref)
			sub, err := c.Submit(ctx, Input{Type: "input", Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			if got, err := sub.Wait(ctx); err != nil || got.Record.Status != "done" {
				t.Fatalf("submission = %+v, %v", got.Record, err)
			}
			if len(seen) != 1 {
				t.Fatal("tool did not finish observing its keys")
			}
			values := <-seen
			if afterWake {
				want := "Tool call c1 needs explicit keys after a wake"
				if !reflect.DeepEqual(values, []any{want, want}) || initialized.Load() != 0 {
					t.Fatalf("keyless requests = %v; initializations=%d", values, initialized.Load())
				}
				return
			}
			want := []any{values[0], values[0], "Key k of tool call c1 was already used for another call or task", "Key n of tool call c1 was already used for another call or task", "Nested call c1/k was already made with another tool or other arguments"}
			if values[0] == ID(0) || !reflect.DeepEqual(values, want) || initialized.Load() != 1 {
				t.Fatalf("keyed requests = %v; initializations=%d", values, initialized.Load())
			}
		})
	}
}
