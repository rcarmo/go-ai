package durable

import (
	"context"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// harness-after-run.test.ts:373–427. Use actual held yield hooks and child
// phases to distinguish a continued run from a queued steer starting a new run.
func TestSourceV160AfterRunWaitsForFinalYield(t *testing.T) {
	for _, tc := range []struct {
		name       string
		steer      bool
		turns, end int32
	}{{"continues", false, 3, 1}, {"queued-steer", true, 2, 2}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			var requestCount, yieldCount, hookCount, childCount atomic.Int32
			yieldGates := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}
			childGates := []chan struct{}{make(chan struct{}), make(chan struct{})}
			for _, gate := range append(slices.Clone(yieldGates), childGates...) {
				defer releaseTaskGate(gate)
			}
			yieldEntered := make(chan int32, len(yieldGates))
			childEntered := make(chan TaskRecord, len(childGates))
			asked := make(chan RunSummary, len(childGates))
			work := taskDefinition(t, "test.run-tests", func(run context.Context, task TaskRecord, r *TaskRuntime) error {
				n := childCount.Add(1)
				if n <= int32(len(childGates)) {
					childEntered <- task
					select {
					case <-childGates[n-1]:
					case <-run.Done():
						return run.Err()
					}
				}
				label := generationObject(task.Input.Value)["label"]
				return r.Commit(run, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					if _, err := tx.AppendEntry(r.ConversationID(), EntryDraft{"kind": "test.tests", "data": label}); err != nil {
						return nil, err
					}
					return taskDone(label), nil
				})
			})
			registry := NewRegistry()
			if err := registry.Install(&Extension{Name: "after-run-yield", Tasks: []*TaskDefinition{work}, Hooks: GenerationHooks{
				OnYield: func(run context.Context, _ MessageReceipt, _ *HookAPI) (*Input, error) {
					n := yieldCount.Add(1)
					if n > tc.turns {
						return nil, nil
					}
					yieldEntered <- n
					select {
					case <-yieldGates[n-1]:
					case <-run.Done():
						return nil, run.Err()
					}
					if n < tc.turns {
						return &Input{Content: "again"}, nil
					}
					return nil, nil
				},
				AfterRun: func(_ context.Context, summary RunSummary, _ *HookAPI) (*AfterRunResult, error) {
					n := hookCount.Add(1)
					if n > tc.end {
						return nil, nil
					}
					asked <- summary
					return &AfterRunResult{Tasks: []RunEndTask{{Task: work, Input: JSON{"label": fmt.Sprintf("run %d", n)}}}}, nil
				},
			}}); err != nil {
				t.Fatal(err)
			}
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				out := make(chan goai.Event, 1)
				out <- terminal(fmt.Sprint(requestCount.Add(1)))
				close(out)
				return out
			})
			options.Registry = registry
			h := openHarness(t, store, options)
			c := root(t, h, ref)
			orig, err := c.Submit(ctx, Input{Type: "input", Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			status := func(s *SubmissionHandle, want string) SubmissionRecord {
				t.Helper()
				record, err := s.Status(ctx)
				if err != nil || record.Status != want {
					t.Fatalf("submission=%+v err=%v; want %s", record, err, want)
				}
				return record
			}
			yield := func(want, hooks, children int32) {
				t.Helper()
				var got int32
				if hooks == 0 {
					select {
					case got = <-yieldEntered:
					case summary := <-asked:
						t.Fatal("afterRun asked before the expected continuation yield", summary)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				} else {
					got = watchReceive(t, yieldEntered)
				}
				if got != want || requestCount.Load() != want || hookCount.Load() != hooks || childCount.Load() != children {
					t.Fatalf("yield %d: provider/hook/child counts=%d/%d/%d; want %d/%d/%d", got, requestCount.Load(), hookCount.Load(), childCount.Load(), want, hooks, children)
				}
			}
			child := func(want string) TaskRecord {
				t.Helper()
				var entry TaskRecord
				select {
				case entry = <-childEntered:
				case turn := <-yieldEntered:
					t.Fatal("another yield ran before the required after-run task", turn)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				record, found, err := h.Task(ctx, entry.ID)
				if err != nil || !found || record.Owner == 0 || record.Conversation != c.ID() || record.State.Status != "running" || generationObject(record.Input.Value)["label"] != want || record.Owner != readOnlyHarnessRunID(t, c) {
					t.Fatal("wrong running child at its gate", record, found, err)
				}
				return record
			}
			childDone := func(id ID, want string) {
				t.Helper()
				record, found, err := h.Task(ctx, id)
				out := record.State.Outcome
				if err != nil || !found || record.State.Status != "terminal" || out == nil || out.Status != "completed" || out.Result == nil || !out.Result.Present || out.Result.Value != want {
					t.Fatal("after-run child did not commit its exact completion", record, found, err)
				}
			}
			generationDone := func(id, answer ID) {
				t.Helper()
				record, found, err := h.Task(ctx, id)
				out := record.State.Outcome
				if err != nil || !found || record.State.Status != "terminal" || out == nil || out.Status != "completed" || out.Result == nil || !out.Result.Present || !equalJSONValue(out.Result.Value, JSON{"entryId": float64(answer)}) {
					t.Fatal("generation did not retain its exact answer receipt", record, found, err)
				}
			}
			var steer *SubmissionHandle
			var children []TaskRecord
			yield(1, 0, 0)
			firstInput := status(orig, "placed").Entry
			if tc.steer {
				steer, err = c.Submit(ctx, Input{Type: "input", Content: "steer", WhenBusy: "steer"})
				if err != nil {
					t.Fatal(err)
				}
				if record := status(steer, "queued"); record.Entry != 0 {
					t.Fatal("steer entered history before the first yield resolved", record)
				}
				releaseTaskGate(yieldGates[0]) // proposes "again", but the steer wins
				firstChild := child("run 1")
				children = append(children, firstChild)
				if status(steer, "queued").Entry != 0 || status(orig, "done").Answer == 0 || requestCount.Load() != 1 {
					t.Fatal("steer placed or requested before the first child completed")
				}
				releaseTaskGate(childGates[0])
				yield(2, 1, 1)
				childDone(firstChild.ID, "run 1")
				if status(steer, "placed").Entry == 0 || readOnlyHarnessRunID(t, c) == firstChild.Owner {
					t.Fatal("queued steer did not start a distinct run after the first child")
				}
				releaseTaskGate(yieldGates[1])
				children = append(children, child("run 2"))
				releaseTaskGate(childGates[1])
			} else {
				for i := int32(2); i <= tc.turns; i++ {
					releaseTaskGate(yieldGates[i-2])
					yield(i, 0, 0)
					if original := status(orig, "placed"); original.Entry != firstInput || original.Answer != 0 {
						t.Fatal("a continuation settled or replaced the original input", original)
					}
				}
				releaseTaskGate(yieldGates[tc.turns-1])
				children = append(children, child("run 1"))
				releaseTaskGate(childGates[0])
			}
			if err := c.WaitForIdle(ctx); err != nil {
				t.Fatal(err)
			}
			if hookCount.Load() != tc.end || childCount.Load() != tc.end || requestCount.Load() != tc.turns || yieldCount.Load() != tc.turns {
				t.Fatal("wrong final hook, child, request or yield count", hookCount.Load(), childCount.Load(), requestCount.Load(), yieldCount.Load())
			}
			page, err := c.Entries(ctx, EntryQuery{Order: Ascending}, 16, nil)
			if err != nil || page.Next != nil {
				t.Fatal("expected the entire short transcript", page.Next, err)
			}
			var users, answers, labels, kinds []string
			var userIDs, answerIDs []ID
			for _, entry := range page.Items {
				if entry.Kind != "pi.system" {
					kinds = append(kinds, entry.Kind)
				}
				switch entry.Kind {
				case "pi.user":
					users, userIDs = append(users, afterRunYieldEntryText(t, entry)), append(userIDs, entry.ID)
				case "pi.assistant":
					answers, answerIDs = append(answers, afterRunYieldEntryText(t, entry)), append(answerIDs, entry.ID)
				case "test.tests":
					i := len(labels)
					if i >= len(children) || !entry.HasData || entry.ByTask != children[i].ID || entry.Data != fmt.Sprintf("run %d", i+1) {
						t.Fatal("unexpected child write", entry)
					}
					labels = append(labels, entry.Data.(string))
				}
			}
			if tc.steer {
				if !slices.Equal(users, []string{"go", "steer"}) || !slices.Equal(answers, []string{"1", "2"}) || !slices.Equal(labels, []string{"run 1", "run 2"}) || !slices.Equal(kinds, []string{"pi.user", "pi.assistant", "test.tests", "pi.user", "pi.assistant", "test.tests"}) {
					t.Fatal("queued-steer transcript", users, answers, labels, kinds)
				}
				for i, sub := range []*SubmissionHandle{orig, steer} {
					if record := status(sub, "done"); record.Entry != userIDs[i] || record.Answer != answerIDs[i] {
						t.Fatal("wrong settled answer for queued-steer branch", record)
					}
					childDone(children[i].ID, labels[i])
					generationDone(children[i].Owner, answerIDs[i])
					if got := watchReceive(t, asked); got != (RunSummary{Reason: "answer", First: userIDs[i], Answer: answerIDs[i], Last: answerIDs[i]}) {
						t.Fatal("wrong queued-steer run summary", got)
					}
				}
			} else {
				if !slices.Equal(users, []string{"go", "again", "again"}) || !slices.Equal(answers, []string{"1", "2", "3"}) || !slices.Equal(labels, []string{"run 1"}) || !slices.Equal(kinds, []string{"pi.user", "pi.assistant", "pi.user", "pi.assistant", "pi.user", "pi.assistant", "test.tests"}) {
					t.Fatal("continued-run transcript", users, answers, labels, kinds)
				}
				last := answerIDs[len(answerIDs)-1]
				if got := watchReceive(t, asked); got != (RunSummary{Reason: "answer", First: userIDs[0], Answer: last, Last: last}) {
					t.Fatal("only final answer should have asked afterRun", got)
				}
				if record := status(orig, "done"); record.Entry != firstInput || record.Answer != last {
					t.Fatal("continued run did not settle on its final answer", record)
				}
				childDone(children[0].ID, labels[0])
				generationDone(children[0].Owner, last)
			}
			if err := h.Close(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func afterRunYieldEntryText(t *testing.T, entry Entry) string {
	t.Helper()
	if len(entry.Model) != 1 {
		t.Fatal("expected one model message", entry)
	}
	msg := entry.Model[0]
	if msg.ContentText != nil {
		return *msg.ContentText
	}
	if len(msg.Content) != 1 || msg.Content[0].Type != "text" {
		t.Fatal("expected one text block", msg.Content)
	}
	return msg.Content[0].Text
}
