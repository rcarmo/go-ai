package durable

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source-derived oracle, not a full upstream test-body port. Official v1.5.0
// f9a26365568127461c47a07a37dcb42c140a3ac5: inbox.ts applyBoundary records
// first for writes before users; generation.ts answer/startRun and submissions.ts
// admitSubmission carry that first into the run, and live.ts endRun preserves it
// in runFailure. Native adaptations: run.task names the generation, and its
// checkpoint InputEntry/settlement Entry still identify B, not the boundary's W.
func TestMainBoundaryFirstIncludesPassiveWrite(t *testing.T) {
	for _, scenario := range []string{"answerA", "keptfailureA"} {
		t.Run(scenario, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				entered := []chan struct{}{make(chan struct{}), make(chan struct{})}
				release := []chan struct{}{make(chan struct{}), make(chan struct{})}
				requestB := make(chan []string, 1)
				var calls atomic.Int64
				ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					ch := make(chan goai.Event, 1)
					n := calls.Add(1)
					go func() {
						defer close(ch)
						if n > 2 {
							t.Errorf("unexpected provider call %d: C must stay queued", n)
							ch <- terminal("unexpected answer")
							return
						}
						if n == 2 {
							var users []string
							for _, message := range input.Messages {
								if message.Role == goai.RoleUser {
									for _, block := range message.Content {
										users = append(users, block.Text)
									}
								}
							}
							requestB <- users
						}
						close(entered[n-1])
						select {
						case <-release[n-1]:
						case <-ctx.Done():
							return
						}
						if n == 1 && scenario == "answerA" {
							ch <- terminal("A answer")
							return
						}
						detail := "B literal failure"
						if n == 1 {
							detail = "A literal failure"
						}
						message := terminal("failed").Message
						message.StopReason, message.ErrorMessage = goai.StopReasonError, detail
						ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: message}
					}()
					return ch
				})
				options.Settings = &HarnessSettings{AfterFailedRun: "keep", FollowUpMode: "one-at-a-time"}
				options.OnReport = func(err error) { t.Error("scheduler", err) }
				h := openHarness(t, b.store, options)
				cleanupTaskGates(t, release...)
				c := root(t, h, ref)

				// These helpers only read actual committed state; they never seed or
				// edit live documents, checkpoints, inboxes, or settlement records.
				snapshot := func() (Snapshot, JSON) {
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
				requireJSON := func(label string, got any, want JSON) {
					t.Helper()
					encoded, err := dtoObject(want, h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					if !equalJSONValue(got, encoded) {
						t.Errorf("%s: got %v; want %v", label, got, encoded)
					}
				}
				requireQueued := func(state Snapshot, subs ...*SubmissionHandle) {
					t.Helper()
					var items []any
					count := 0
					for _, doc := range state.Documents {
						if !doc.Retired && doc.Scope == "conversation" && doc.Kind == "pi.inbox" && doc.Owner == c.ID() {
							var ok bool
							items, ok = doc.Value["items"].([]any)
							if !ok {
								t.Fatal("actual inbox has no items array", doc.Value)
							}
							count++
						}
					}
					if count != 1 || len(items) != len(subs) {
						t.Fatalf("expected exactly %d queued inbox items, got %v (documents=%d)", len(subs), items, count)
					}
					for i, sub := range subs {
						record, exists := state.Submissions[sub.ID()]
						status := publicSubmission(record)
						item := generationObject(items[i])
						mode := "followUp"
						if status.Type == "write" {
							mode = "write"
						}
						if !exists || status.Status != "queued" || status.Entry != 0 || status.Answer != 0 || submissionEntryID(item["id"]) != sub.ID() || item["mode"] != mode {
							t.Fatalf("submission must remain unplaced in its own inbox slot: %+v, item=%v", status, item)
						}
					}
				}
				requireFailure := func(label string, live JSON, owner, input, first ID, detail string) {
					t.Helper()
					requireJSON(label, live["runFailure"], JSON{"taskId": owner, "inputs": []any{input}, "first": first, "reason": "model_error", "detail": detail})
				}

				a, err := c.Submit(bg, Input{Content: "A"})
				if err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, entered[0])
				state, live := snapshot()
				aStatus := publicSubmission(state.Submissions[a.ID()])
				aRun := generationObject(live["run"])
				aOwner := submissionEntryID(aRun["task"])
				if aStatus.Status != "placed" || aStatus.Entry == 0 || aOwner == 0 || state.Tasks[aOwner].Kind != "pi.generation" {
					t.Fatal("A must own a real placed run while gated", aStatus, aRun)
				}
				requireJSON("A active run", aRun, JSON{"task": aOwner, "inputs": []any{a.ID()}, "first": aStatus.Entry})
				bSub, err := c.Submit(bg, Input{Type: "follow-up", Content: "B"})
				if err != nil {
					t.Fatal(err)
				}
				write, err := c.Submit(bg, Input{Type: "write", Entry: &Entry{Kind: "app.note", HasData: true, Data: JSON{"text": "W"}, Model: []MessageReceipt{userReceipt("W")}}})
				if err != nil {
					t.Fatal(err)
				}
				state, _ = snapshot()
				requireQueued(state, bSub, write) // B was admitted first, but W must be placed first.
				for _, entry := range state.Entries {
					if entry.Kind == "app.note" {
						t.Fatal("model-contributing W was placed while A was gated", entry)
					}
				}
				releaseTaskGate(release[0])
				aResult := waitSubmission(t, a)
				if aResult.Task.ID != aOwner || aResult.Record.Entry != aStatus.Entry {
					t.Fatal("A settled outside its own run/input", aResult)
				}
				var fresh *SubmissionHandle
				if scenario == "keptfailureA" {
					if aResult.Record.Status != "unanswered" || aResult.Record.Reason != "model_error" || aResult.Record.Answer != 0 || aResult.Record.Detail == nil || !aResult.Record.Detail.Present || aResult.Record.Detail.Value != "A literal failure" {
						t.Fatal("A must fail with its literal model detail", aResult.Record)
					}
					idleFailed140(t, h)
					state, live = snapshot()
					requireQueued(state, bSub, write)
					requireFailure("kept A failure", live, aOwner, a.ID(), aStatus.Entry, "A literal failure")
					if _, exists := live["run"]; exists || calls.Load() != 1 {
						t.Fatal("kept A failure must leave no active run or automatic successor", live, calls.Load())
					}
					// A genuinely new input, not a duplicate or continue, releases the
					// held queue. One-at-a-time selects B and leaves this C unplaced.
					fresh, err = c.Submit(bg, Input{Content: "C", WhenBusy: "reject"})
					if err != nil {
						t.Fatal(err)
					}
				} else if aResult.Record.Status != "done" || aResult.Record.Answer == 0 {
					t.Fatal("A must answer before its final boundary starts B", aResult.Record)
				}

				awaitTaskSignal(t, entered[1])
				if got := strings.Join(<-requestB, ","); got != "A,W,B" {
					t.Errorf("actual B provider request must include W before B and exclude C: got %q; want A,W,B", got)
				}
				state, live = snapshot()
				wStatus := publicSubmission(state.Submissions[write.ID()])
				bStatus := publicSubmission(state.Submissions[bSub.ID()])
				wEntry, wExists := state.Entries[wStatus.Entry]
				bEntry, bExists := state.Entries[bStatus.Entry]
				if wStatus.Status != "done" || wStatus.Entry == 0 || wStatus.Answer != 0 || !wExists || wEntry.Conversation != c.ID() || wEntry.Kind != "app.note" || generationObject(wEntry.Data)["text"] != "W" || len(wEntry.Model) != 1 || wEntry.Model[0].Role != goai.RoleUser || len(wEntry.Model[0].Content) != 1 || wEntry.Model[0].Content[0].Text != "W" {
					t.Fatal("W must settle done at its own persisted model-contributing entry", wStatus, wEntry)
				}
				if bStatus.Status != "placed" || bStatus.Entry == 0 || bStatus.Entry == wStatus.Entry || bStatus.Entry == aStatus.Entry || bStatus.Answer != 0 || !bExists || bEntry.Conversation != c.ID() || !isMessageEntry(bEntry) {
					t.Fatal("gated B must be placed at its own distinct persisted input entry", bStatus, bEntry)
				}
				var bMessage MessageReceipt
				if err := decodeEntryMessage(bEntry, h.session.limits, &bMessage); err != nil {
					t.Fatal(err)
				}
				if bMessage.Role != goai.RoleUser || bMessage.ContentText == nil || *bMessage.ContentText != "B" {
					t.Fatal("B entry must contain B's actual user contribution", bMessage)
				}
				if wEntry.Seq == 0 || bEntry.Seq == 0 || !(wEntry.Seq < bEntry.Seq || wEntry.Seq == bEntry.Seq && wEntry.Position < bEntry.Position) {
					t.Errorf("persisted boundary order must be W before B by (Seq,Position): W=(%d,%d), B=(%d,%d)", wEntry.Seq, wEntry.Position, bEntry.Seq, bEntry.Position)
				}
				run := generationObject(live["run"])
				owner := submissionEntryID(run["task"])
				task, exists := state.Tasks[owner]
				if !exists || owner == aOwner || task.Kind != "pi.generation" || task.Conversation != c.ID() || terminalStatus(task.Status) {
					t.Fatal("B must own the real active generation", run, task)
				}
				var checkpoint generationCheckpoint
				if err := fromObject(task.Checkpoint, &checkpoint, h.session.limits); err != nil {
					t.Fatal(err)
				}
				if checkpoint.Phase != "intent" || checkpoint.Submission != bSub.ID() || checkpoint.InputEntry != bEntry.ID || len(checkpoint.Steered) != 0 {
					t.Fatalf("B checkpoint must keep B's InputEntry, not boundary W: %+v", checkpoint)
				}
				requireJSON("B active run must start at W while owning only B (W is not an input)", run, JSON{"task": owner, "inputs": []any{bSub.ID()}, "first": wEntry.ID})
				if failure, exists := live["runFailure"]; exists {
					t.Errorf("B starts in a later admission/answer commit and must not retain A failure: %v", failure)
				}
				if fresh != nil {
					requireQueued(state, fresh)
				}
				if calls.Load() != 2 {
					t.Fatal("expected only A and gated B provider calls", calls.Load())
				}

				releaseTaskGate(release[1])
				failed := waitSubmission(t, bSub)
				if failed.Task.ID != owner || failed.Record.Status != "unanswered" || failed.Record.Reason != "model_error" || failed.Record.Entry != bEntry.ID || failed.Record.Answer != 0 || failed.Record.Detail == nil || !failed.Record.Detail.Present || failed.Record.Detail.Value != "B literal failure" {
					t.Fatal("B failure must retain its own settlement Entry and literal detail", failed)
				}
				idleFailed140(t, h)
				state, live = snapshot()
				requireFailure("B runFailure must preserve boundary W as first", live, owner, bSub.ID(), wEntry.ID, "B literal failure")
				if _, exists := live["run"]; exists {
					t.Error("failed B left an active run", live)
				}
				if fresh != nil {
					requireQueued(state, fresh)
				}
				if got := publicSubmission(state.Submissions[write.ID()]); got.Status != "done" || got.Entry != wEntry.ID || got.Answer != 0 {
					t.Fatal("B failure must not turn W into a run input or resettle its write", got)
				}
				if calls.Load() != 2 {
					t.Error("B failure must keep C queued without another provider call", calls.Load())
				}
			})
		})
	}
}
