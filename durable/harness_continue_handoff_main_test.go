package durable

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Native public-path regression for an explicit, entryless continue crossing a
// real tool round. No checkpoint, live document or submission is seeded. This
// deliberately fails at Submit while the main continue admission is unsupported;
// it must not skip or substitute an empty user input for that missing baseline.
func TestMainContinueToolHandoffOrderedInputs(t *testing.T) {
	for _, queued := range []bool{false, true} {
		for _, late := range []bool{false, true} {
			for _, fail := range []bool{false, true} {
				t.Run(fmt.Sprintf("queued=%t/late=%t/failure=%t", queued, late, fail), func(t *testing.T) {
					backends(t, func(t *testing.T, b backend) {
						entered := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}
						release := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}
						afterTools, releaseTools := make(chan struct{}), make(chan struct{})
						requests := make(chan []goai.Message, 2)
						var calls, tools, hooks atomic.Int64
						ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, request *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
							ch := make(chan goai.Event, 1)
							n := calls.Add(1)
							if n > 3 {
								t.Errorf("unexpected provider request %d", n)
								close(ch)
								return ch
							}
							if n > 1 {
								requests <- request.Messages
							}
							close(entered[n-1])
							go func() {
								defer close(ch)
								select {
								case <-release[n-1]:
								case <-ctx.Done():
									return
								}
								switch {
								case n == 2:
									ch <- toolAnswer("continue-call", "continue-tool", JSON{})
								case n == 1 || fail:
									message := terminal("").Message
									message.Content = nil
									message.StopReason = goai.StopReasonError
									message.ErrorMessage = "successor failure"
									if n == 1 {
										message.ErrorMessage = "A failure"
									}
									ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: message}
								default:
									ch <- terminal("continued answer")
								}
							}()
							return ch
						})
						registry := NewRegistry()
						registration := wrapRegistration("continue-tool")
						registration.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
							tools.Add(1)
							return ToolResult{Content: "actual tool result"}, nil
						}
						if err := registry.Register(registration); err != nil {
							t.Fatal(err)
						}
						if err := registry.Install(&Extension{Name: "continue-handoff", Hooks: GenerationHooks{
							AfterTools: func(ctx context.Context, results []MessageReceipt, _ *HookAPI) error {
								hooks.Add(1)
								if len(results) != 1 || results[0].ToolCallID != "continue-call" || len(results[0].Content) != 1 || results[0].Content[0].Text != "actual tool result" {
									t.Errorf("afterTools did not receive the real tool receipt: %+v", results)
								}
								close(afterTools)
								select {
								case <-releaseTools:
									return nil
								case <-ctx.Done():
									return ctx.Err()
								}
							},
						}}); err != nil {
							t.Fatal(err)
						}
						options.Registry = registry
						options.Settings = &HarnessSettings{AfterFailedRun: "keep", SteeringMode: "all", FollowUpMode: "all"}
						options.OnReport = func(err error) { t.Error("scheduler", err) }
						h := openHarness(t, b.store, options)
						cleanupTaskGates(t, release[0], release[1], release[2], releaseTools)
						c := root(t, h, ref)
						snapshot := func() (Snapshot, JSON) {
							t.Helper()
							state, err := h.Snapshot(bg)
							if err != nil {
								t.Fatal(err)
							}
							return state, mainContinueOracleLive(t, state, c.ID())
						}
						requireJSON := func(label string, got any, want JSON) {
							t.Helper()
							encoded, err := dtoObject(want, h.session.limits)
							if err != nil {
								t.Fatal(err)
							}
							if !equalJSONValue(got, encoded) {
								t.Fatalf("%s = %v; want %v", label, got, encoded)
							}
						}
						checkpoint := func(task Task) generationCheckpoint {
							t.Helper()
							var cp generationCheckpoint
							if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
								t.Fatal(err)
							}
							return cp
						}
						requireUsers := func(messages []goai.Message, want []string) {
							t.Helper()
							var got []string
							for _, message := range messages {
								if message.Role == goai.RoleUser {
									if len(message.Content) != 1 || message.Content[0].Type != "text" {
										t.Fatalf("unexpected user message: %+v", message)
									}
									got = append(got, message.Content[0].Text)
								}
							}
							if !reflect.DeepEqual(got, want) {
								t.Fatalf("provider users = %q; want %q (no fabricated continue user)", got, want)
							}
						}
						a, err := c.Submit(bg, Input{Content: "A"})
						if err != nil {
							t.Fatal(err)
						}
						awaitTaskSignal(t, entered[0])
						var selected []*SubmissionHandle
						wantUsers := []string{"A"}
						if queued {
							for _, input := range []Input{{Type: "follow-up", Content: "B"}, {Type: "steer", Content: "C"}} {
								sub, err := c.Submit(bg, input)
								if err != nil {
									t.Fatal(err)
								}
								selected = append(selected, sub)
								wantUsers = append(wantUsers, input.Content)
							}
						}
						releaseTaskGate(release[0])
						failedA := waitSubmission(t, a)
						if failedA.Record.Status != "unanswered" || failedA.Record.Reason != "model_error" || failedA.Record.Answer != 0 || failedA.Record.Entry == 0 {
							t.Fatal("A must fail at its own placed input", failedA.Record)
						}
						state, live := snapshot()
						requireJSON("A runFailure", live["runFailure"], JSON{"taskId": failedA.Task.ID, "inputs": []any{a.ID()}, "first": failedA.Record.Entry, "reason": "model_error", "detail": "A failure"})
						for _, sub := range selected {
							if status := publicSubmission(state.Submissions[sub.ID()]); status.Status != "queued" || status.Entry != 0 || status.Answer != 0 {
								t.Fatal("selected users must still be genuinely queued", status)
							}
						}
						resumed, err := c.Submit(bg, Input{Type: "continue", RequestID: "continue-handoff"})
						if err != nil {
							t.Fatalf("explicit continue Submit (main admission baseline required): %v", err)
						}
						awaitTaskSignal(t, entered[1])
						requireUsers(<-requests, wantUsers)
						state, live = snapshot()
						run := generationObject(live["run"])
						currentID := submissionEntryID(run["task"])
						cp := checkpoint(state.Tasks[currentID])
						inputs, steered := []any{}, []ID{}
						placements := map[ID]ID{resumed.ID(): 0}
						// admitSubmission inherits live.runFailure.first; handOver
						// preserves it across tools, even with newly selected users.
						first := failedA.Record.Entry
						for _, sub := range selected {
							status := publicSubmission(state.Submissions[sub.ID()])
							if status.Status != "placed" || status.Entry == 0 || status.Answer != 0 {
								t.Fatal("continue must place selected queued users", status)
							}
							placements[sub.ID()] = status.Entry
							inputs, steered = append(inputs, sub.ID()), append(steered, sub.ID())
						}
						inputs = append(inputs, resumed.ID())
						wantRun := JSON{"task": currentID, "inputs": inputs}
						if first != 0 {
							wantRun["first"] = first
						}
						requireJSON("continue admission run", run, wantRun)
						if cp.Submission != resumed.ID() || cp.InputEntry != 0 || cp.Input != "" || len(cp.InputBlocks) != 0 || !reflect.DeepEqual(append([]ID{}, cp.Steered...), steered) {
							t.Fatal("entryless continue checkpoint must retain selected users, not replace the primary", cp)
						}
						if _, exists := live["runFailure"]; exists {
							t.Fatal("explicit continue must clear A failure", live)
						}
						releaseTaskGate(release[1])
						awaitTaskSignal(t, afterTools)
						var lateSub *SubmissionHandle
						if late {
							lateSub, err = c.Submit(bg, Input{Type: "steer", Content: "late"})
							if err != nil {
								t.Fatal(err)
							}
							status, err := lateSub.Status(bg)
							if err != nil || status.Status != "queued" || status.Entry != 0 {
								t.Fatal("late steer must queue behind the actual afterTools gate", status, err)
							}
							inputs, steered = append(inputs, lateSub.ID()), append(steered, lateSub.ID())
							wantUsers = append(wantUsers, "late")
						}
						releaseTaskGate(releaseTools)
						awaitTaskSignal(t, entered[2])
						messages := <-requests
						requireUsers(messages, wantUsers)
						toolCalls, results := 0, 0
						for _, message := range messages {
							for _, block := range message.Content {
								if message.Role == goai.RoleAssistant && block.Type == "toolCall" && block.ID == "continue-call" {
									toolCalls++
								}
							}
							if message.Role == goai.RoleToolResult && message.ToolCallID == "continue-call" && len(message.Content) == 1 && message.Content[0].Text == "actual tool result" {
								results++
							}
						}
						if toolCalls != 1 || results != 1 || tools.Load() != 1 || hooks.Load() != 1 || calls.Load() != 3 {
							t.Fatal("handoff must use one actual tool round without replay", toolCalls, results, tools.Load(), hooks.Load(), calls.Load())
						}
						state, live = snapshot()
						previous := checkpoint(state.Tasks[currentID])
						successorID := previous.Successor
						successor := state.Tasks[successorID]
						next := checkpoint(successor)
						assistant := state.Entries[previous.AssistantEntry]
						if successorID == 0 || successorID == currentID || successor.Owner != 0 || successor.Conversation != c.ID() || state.Tasks[currentID].Status != "done" || assistant.ID == 0 || assistant.ByTask != currentID || assistant.Conversation != c.ID() || assistant.Value["role"] != "assistant" || assistant.Value["stopReason"] != string(goai.StopReasonToolUse) {
							t.Fatal("handoff must transfer from the real tool-calling assistant", previous, successor, assistant)
						}
						if next.Submission != resumed.ID() || next.InputEntry != 0 || !reflect.DeepEqual(append([]ID{}, next.Steered...), steered) {
							t.Fatal("successor lost entryless continue or attached users", next)
						}
						// Exact order: selected users, continue, THEN the post-tools steer.
						requireJSON("successor run", live["run"], JSON{"task": successorID, "inputs": inputs, "first": first})
						if _, exists := live["tools"]; exists {
							t.Fatal("handoff retained spent live tools", live)
						}
						if lateSub != nil {
							status := publicSubmission(state.Submissions[lateSub.ID()])
							if status.Status != "placed" || status.Entry == 0 || status.Answer != 0 {
								t.Fatal("late steer was not placed at the tool boundary", status)
							}
							placements[lateSub.ID()] = status.Entry
							selected = append(selected, lateSub)
						}
						for _, sub := range append([]*SubmissionHandle{resumed}, selected...) {
							status := publicSubmission(state.Submissions[sub.ID()])
							if status.Status != "placed" || status.Entry != placements[sub.ID()] || status.Answer != 0 {
								t.Fatal("handoff must leave all inputs pending at their own placements", status)
							}
						}
						releaseTaskGate(release[2])
						settled := waitSubmission(t, resumed)
						answer := settled.Record.Answer
						for _, sub := range append([]*SubmissionHandle{resumed}, selected...) {
							result := waitSubmission(t, sub)
							status, err := sub.Status(bg)
							if err != nil || !reflect.DeepEqual(status, result.Record) || result.Task.ID != successorID || status.Entry != placements[sub.ID()] || status.Answer != answer {
								t.Fatal("Status/Wait must agree on successor settlement and original placement", result, status, err)
							}
							if fail {
								if status.Status != "unanswered" || status.Answer != 0 || status.Reason != "model_error" || status.Detail == nil || !status.Detail.Present || status.Detail.Value != "successor failure" {
									t.Fatal("exact shared failure settlement", status)
								}
							} else if status.Status != "done" || status.Answer == 0 || status.Reason != "" || status.Detail != nil || result.Message == nil || len(result.Message.Content) != 1 || result.Message.Content[0].Text != "continued answer" {
								t.Fatal("exact shared done settlement", status, result.Message)
							}
						}
						state, live = snapshot()
						if resumedStatus := publicSubmission(state.Submissions[resumed.ID()]); resumedStatus.Type != "continue" || resumedStatus.Entry != 0 {
							t.Fatal("continue must remain entryless through final settlement", resumedStatus)
						}
						if !reflect.DeepEqual(publicSubmission(state.Submissions[a.ID()]), failedA.Record) {
							t.Fatal("continue rewrote A's settled failure")
						}
						if fail {
							requireJSON("successor runFailure", live["runFailure"], JSON{"taskId": successorID, "inputs": inputs, "first": first, "reason": "model_error", "detail": "successor failure"})
						} else if _, exists := live["runFailure"]; exists {
							t.Fatal("successful continue retained a failure", live)
						}
						if _, exists := live["run"]; exists {
							t.Fatal("settlement retained an active run", live)
						}
						var persisted []goai.Message
						for _, entry := range state.Entries {
							if entry.Conversation == c.ID() && entry.Value["role"] == "user" {
								var receipt MessageReceipt
								if err := fromObject(entry.Value, &receipt, h.session.limits); err != nil {
									t.Fatal(err)
								}
								persisted = append(persisted, receiptMessage(receipt))
							}
						}
						// Entry map order is irrelevant here; count and nonempty content
						// complement the ordered real provider-context assertions above.
						if len(persisted) != len(wantUsers) || tools.Load() != 1 || calls.Load() != 3 {
							t.Fatal("continue fabricated a user or replayed a spent effect", persisted, tools.Load(), calls.Load())
						}
						for _, message := range persisted {
							if len(message.Content) != 1 || message.Content[0].Text == "" {
								t.Fatal("continue persisted an empty user", message)
							}
						}
					})
				})
			}
		}
	}
}
