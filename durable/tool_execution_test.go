package durable

import (
	"context"
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
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
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "first", Parameters: schema}, Implementation: "round.first", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
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
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "second", Parameters: schema}, Implementation: "round.second", Version: 1, ExecutionMode: toolMode, PrepareArguments: func(_ context.Context, args JSON) (JSON, error) {
					secondPrepared.Add(1)
					if (mode == "sequential" || mode == "tool-sequential") && !firstReturned.Load() {
						t.Error("sequential pending call prepared before predecessor returned")
					}
					return args, nil
				}, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
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
					if request.Version != 2 || submissionEntryID(cp["attempt"]) != 1 || request.Input == nil || !request.Input.Present || !equalJSONValue(request.Input.Value, JSON{}) {
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
					reg.PrepareArguments = func(_ context.Context, args JSON) (JSON, error) {
						if name == "second" {
							prepared.Add(1)
							if round == "sequential" && !firstReturned.Load() {
								t.Error("later sequential call prepared before predecessor ended")
							}
						}
						return args, nil
					}
					reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
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
					if err != nil || !found || record.Kind != "pi.tool" || record.Version != 3 || record.Owner == 0 {
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
