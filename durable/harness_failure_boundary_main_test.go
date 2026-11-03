package durable

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source-derived control, official v1.5.0 f9a26365 generation.ts
// endFailedRun/postTools/startRun and live.ts endRun. Existing queue controls
// cover an immediate successor; here every input belongs to the ending run.
// A real tool handoff must preserve all attached IDs and the primary first,
// but neither continue nor reset may reserve failure history for later input.
func TestMainRunFailureAttachedInputsWithoutQueuedSuccessor(t *testing.T) {
	for _, reason := range []string{"model_error", "reset"} {
		t.Run(reason, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				toolEntered := []chan struct{}{make(chan struct{}), make(chan struct{})}
				toolRelease := []chan struct{}{make(chan struct{}), make(chan struct{})}
				failureEntered, failureRelease := make(chan struct{}), make(chan struct{})
				freshEntered, freshRelease := make(chan struct{}), make(chan struct{})
				var calls, tools atomic.Int64
				registry := NewRegistry()
				reg := wrapRegistration("hold")
				reg.Execute = func(ctx context.Context, _ JSON, _ *ToolAPI) (ToolResult, error) {
					n := tools.Add(1)
					if n > 2 {
						t.Errorf("unexpected tool call %d", n)
						return ToolResult{Content: "unexpected"}, nil
					}
					close(toolEntered[n-1])
					select {
					case <-toolRelease[n-1]:
						return ToolResult{Content: "tool result"}, nil
					case <-ctx.Done():
						return ToolResult{}, ctx.Err()
					}
				}
				if err := registry.Register(reg); err != nil {
					t.Fatal(err)
				}
				ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					ch := make(chan goai.Event, 1)
					n := calls.Add(1)
					go func() {
						defer close(ch)
						switch n {
						case 1:
							ch <- toolAnswer("attach-call", "hold", JSON{})
						case 2:
							var users []string
							for _, message := range input.Messages {
								if message.Role == goai.RoleUser {
									for _, block := range message.Content {
										users = append(users, block.Text)
									}
								}
							}
							if got := strings.Join(users, ","); got != "primary,steer-one,steer-two" {
								t.Errorf("actual post-tools inputs = %q", got)
							}
							close(failureEntered)
							select {
							case <-failureRelease:
							case <-ctx.Done():
								return
							}
							if reason == "reset" {
								ch <- toolAnswer("reset-call", "hold", JSON{})
							} else {
								msg := terminal("failed").Message
								msg.StopReason, msg.ErrorMessage = goai.StopReasonError, "attached failure"
								ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: msg}
							}
						case 3:
							close(freshEntered)
							select {
							case <-freshRelease:
								ch <- terminal("fresh answer")
							case <-ctx.Done():
							}
						default:
							t.Errorf("unexpected provider call %d", n)
							ch <- terminal("unexpected")
						}
					}()
					return ch
				})
				policy := "continue"
				if reason == "reset" {
					policy = "keep" // Reset must not depend on afterFailedRun=continue.
				}
				options.Registry = registry
				options.Settings = &HarnessSettings{AfterFailedRun: policy, SteeringMode: "all", FollowUpMode: "one-at-a-time"}
				options.OnReport = func(err error) { t.Error("scheduler", err) }
				h := openHarness(t, b.store, options)
				cleanupTaskGates(t, toolRelease[0], toolRelease[1], failureRelease, freshRelease)
				c := root(t, h, ref)
				liveSnapshot := func() (Snapshot, JSON) {
					t.Helper()
					state, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					var live JSON
					count := 0
					for _, doc := range state.Documents {
						if !doc.Retired && doc.Scope == "conversation" && doc.Kind == "pi.live" && doc.Owner == c.ID() {
							live, count = doc.Value, count+1
						}
					}
					if count != 1 {
						t.Fatalf("expected one actual live document, got %d", count)
					}
					return state, live
				}
				primary, err := c.Submit(bg, Input{Content: "primary"})
				if err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, toolEntered[0])
				_, live := liveSnapshot()
				firstRun := generationObject(live["run"])
				ancestor, first := submissionEntryID(firstRun["task"]), submissionEntryID(firstRun["first"])
				inputs := []*SubmissionHandle{primary}
				inputIDs := []any{primary.ID()}
				for _, text := range []string{"steer-one", "steer-two"} {
					steer, err := c.Submit(bg, Input{Type: "steer", Content: text})
					if err != nil {
						t.Fatal(err)
					}
					status, err := steer.Status(bg)
					if err != nil || status.Status != "queued" || status.Entry != 0 {
						t.Fatalf("steer must be unplaced before tools return: %+v, %v", status, err)
					}
					inputs, inputIDs = append(inputs, steer), append(inputIDs, steer.ID())
				}
				releaseTaskGate(toolRelease[0])
				awaitTaskSignal(t, failureEntered)
				state, live := liveSnapshot()
				run := generationObject(live["run"])
				owner := submissionEntryID(run["task"])
				var handoff generationCheckpoint
				if err := fromObject(state.Tasks[ancestor].Checkpoint, &handoff, h.session.limits); err != nil {
					t.Fatal(err)
				}
				if ancestor == 0 || owner == 0 || owner == ancestor || handoff.Successor != owner || state.Tasks[ancestor].Status != "done" || state.Tasks[owner].Conversation != c.ID() {
					t.Fatal("expected actual committed tool handoff", ancestor, owner, handoff)
				}
				wantRun, err := dtoObject(JSON{"task": owner, "inputs": inputIDs, "first": first}, h.session.limits)
				if err != nil {
					t.Fatal(err)
				}
				if !equalJSONValue(run, wantRun) {
					t.Fatalf("handoff must preserve exact inputs/primary first: got %v; want %v", run, wantRun)
				}
				entries := make(map[ID]ID)
				seen := make(map[ID]bool)
				for _, sub := range inputs {
					status := publicSubmission(state.Submissions[sub.ID()])
					entry, exists := state.Entries[status.Entry]
					if status.Status != "placed" || status.Entry == 0 || seen[status.Entry] || !exists || entry.Conversation != c.ID() || entry.Value["role"] != "user" || sub == primary && status.Entry != first {
						t.Fatalf("attached input must own its distinct persisted user entry: %+v, %+v", status, entry)
					}
					entries[sub.ID()], seen[status.Entry] = status.Entry, true
				}
				releaseTaskGate(failureRelease)
				if reason == "reset" {
					awaitTaskSignal(t, toolEntered[1])
					if err := c.Reset(bg, "handoff"); err != nil {
						t.Fatal(err)
					}
					releaseTaskGate(toolRelease[1])
				}
				failed := waitSubmission(t, primary)
				for _, sub := range inputs {
					result := waitSubmission(t, sub)
					detailOK := result.Record.Detail == nil
					if reason == "model_error" {
						detailOK = result.Record.Detail != nil && result.Record.Detail.Present && result.Record.Detail.Value == "attached failure"
					}
					if result.Record.Status != "unanswered" || result.Record.Reason != reason || !detailOK || result.Task.ID != owner || result.Record.Entry != entries[sub.ID()] || result.Record.Answer != 0 {
						t.Fatalf("attached input settled outside its ending run: %+v", result)
					}
				}
				idleFailed140(t, h)
				var cp generationCheckpoint
				if err := fromObject(failed.Task.Checkpoint, &cp, h.session.limits); err != nil {
					t.Fatal(err)
				}
				if cp.FailureCut != 0 || reason == "reset" && !cp.Reset || reason == "model_error" && cp.AfterFailedRun != "continue" {
					t.Fatalf("no unattached successor may be reserved: %+v", cp)
				}
				state, live = liveSnapshot()
				for _, task := range state.Tasks {
					if task.Kind == "pi.generation" && task.Conversation == c.ID() && !terminalStatus(task.Status) {
						t.Fatal("unexpected queued/active successor before fresh admission", task)
					}
				}
				failure := JSON{"taskId": owner, "inputs": inputIDs, "first": first, "reason": reason}
				if reason == "model_error" {
					failure["detail"] = "attached failure"
				}
				wantLive, err := dtoObject(JSON{"runFailure": failure}, h.session.limits)
				if err != nil {
					t.Fatal(err)
				}
				if !equalJSONValue(live, wantLive) {
					t.Errorf("ending run failure must retain exact attached IDs/first: got %v; want %v", live, wantLive)
				}
				fresh, err := c.Submit(bg, Input{Content: "fresh", WhenBusy: "reject"})
				if err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, freshEntered)
				state, live = liveSnapshot()
				freshStatus := publicSubmission(state.Submissions[fresh.ID()])
				run = generationObject(live["run"])
				if freshStatus.Status != "placed" || freshStatus.Entry == 0 || seen[freshStatus.Entry] || submissionEntryID(run["first"]) != freshStatus.Entry || submissionEntryID(run["task"]) == owner {
					t.Fatal("fresh admission must own a new run/first", freshStatus, run)
				}
				if failure, exists := live["runFailure"]; exists {
					t.Errorf("fresh in-progress input must clear failure when no successor was queued: %v", failure)
				}
				releaseTaskGate(freshRelease)
				if result := waitSubmission(t, fresh); result.Record.Status != "done" || result.Record.Answer == 0 {
					t.Fatal("fresh run did not answer", result)
				}
				idleFailed140(t, h)
				wantTools := int64(1)
				if reason == "reset" {
					wantTools = 2
				}
				if calls.Load() != 3 || tools.Load() != wantTools {
					t.Fatal("unexpected replay/dispatch", calls.Load(), tools.Load())
				}
			})
		})
	}
}
