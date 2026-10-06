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
	for _, mode := range []string{"parallel", "sequential", "tool-sequential", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
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
					if mode != "parallel" && mode != "legacy" && !firstReturned.Load() {
						t.Error("sequential tool overlapped")
					}
					close(secondEntered)
					return ToolResult{Content: "second result"}, nil
				}}); err != nil {
					t.Fatal(err)
				}
				var calls atomic.Int64
				ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					ch := make(chan goai.Event, 1)
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
				h := openHarness(t, b.store, options)
				cleanupTaskGates(t, releaseFirst)
				setting := mode
				if mode == "tool-sequential" || mode == "legacy" {
					setting = "parallel"
				}
				conversation, err := h.Root(bg, AgentChange{Model: ref, Settings: RequestSettings{ToolExecution: setting}})
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
				sub, err := conversation.Submit(bg, Input{Content: "round"})
				if err != nil {
					t.Fatal(err)
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
				if mode == "legacy" {
					// A persisted old-format checkpoint has no successor metadata.
					// It must resume into the same reference-shaped handoff.
					_, err := h.CommitTasks(bg, conversation.ID(), func(tx *Tx) error {
						for _, task := range tx.state.Tasks {
							if task.Kind != "pi.generation" || terminalStatus(task.Status) {
								continue
							}
							var cp generationCheckpoint
							if err := fromObject(task.Checkpoint, &cp, tx.limits); err != nil {
								return err
							}
							cp.Successor = 0
							checkpoint, err := dtoObject(cp, tx.limits)
							if err != nil {
								return err
							}
							owned, err := copyTask(task, tx.limits)
							if err != nil {
								return err
							}
							owned.Checkpoint = checkpoint
							return tx.stage(Write{Op: "put-task", Task: &owned})
						}
						return reject("legacy generation unavailable")
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				if mode == "parallel" || mode == "legacy" {
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
				var generations, tools []Task
				for _, task := range state.Tasks {
					if task.Conversation != conversation.ID() {
						continue
					}
					switch task.Kind {
					case "pi.generation":
						generations = append(generations, task)
					case "pi.tool":
						tools = append(tools, task)
					}
				}
				// Both new and resumed checkpoints use the reference boundary.
				if len(generations) != 2 || len(tools) != 2 {
					t.Fatal("reference tool round task cardinality", generations, tools)
				}
				for _, generation := range generations {
					if generation.Owner != 0 || generation.Status != "done" {
						t.Fatal("generation not conversation-owned/completed", generation)
					}
					var cp generationCheckpoint
					if err := fromObject(generation.Checkpoint, &cp, h.session.limits); err != nil || cp.Attempt != 1 {
						t.Fatal("generation attempt is not fresh", cp, err)
					}
				}
				owner := tools[0].Owner
				if owner == 0 || tools[1].Owner != owner || state.Tasks[owner].Kind != "pi.generation" {
					t.Fatal("tool ownership", tools)
				}
				for _, tool := range tools {
					if tool.Status != "done" {
						t.Fatal("tool not completed before final outcome", tool)
					}
				}
				var checkpoint generationCheckpoint
				if err := fromObject(state.Tasks[owner].Checkpoint, &checkpoint, h.session.limits); err != nil || checkpoint.AssistantEntry == 0 {
					t.Fatal("spent assistant entry missing", checkpoint, err)
				}
				entry, exists := state.Entries[checkpoint.AssistantEntry]
				if !exists || entry.Conversation != conversation.ID() {
					t.Fatal("tool round assistant entry", entry)
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
				if len(turnEvents) != 4 || turnEvents[0].Type != "turn_start" || turnEvents[1].Type != "turn_end" || turnEvents[2].Type != "turn_start" || turnEvents[3].Type != "turn_end" || turnEvents[1].Task != owner || turnEvents[2].Task != checkpoint.Successor || turnEvents[1].Seq != turnEvents[2].Seq || runs != 1 || ends != 1 {
					t.Fatal("reference handoff turn/run events", turnEvents, runs, ends)
				}
			})
		})
	}
}
