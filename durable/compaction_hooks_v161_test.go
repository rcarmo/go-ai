package durable

import (
	"context"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// v1.6.1 harness-compaction.test.ts:801–811,872–882,1511–1537. The
// queued-owned case additionally holds a busy generation to see the same
// placement receipt before and after the conversation drains its inbox.
func TestSourceV161CompactionHookUsageAndOwnedWork(t *testing.T) {
	for _, mode := range []string{"decline", "summary", "placed-owned", "queued-owned"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			const summary = "HOOK"
			owned := mode == "placed-owned" || mode == "queued-owned"
			queued := mode == "queued-owned"
			childEntered, releaseChild := make(chan TaskRecord, 1), make(chan struct{})
			busyEntered, releaseBusy := make(chan struct{}, 1), make(chan struct{})
			defer releaseTaskGate(releaseChild)
			defer releaseTaskGate(releaseBusy)
			var genCalls, summaryCalls, hooks atomic.Int32
			work := taskDefinition(t, "test.compaction-child", func(run context.Context, task TaskRecord, r *TaskRuntime) error {
				childEntered <- task
				select {
				case <-releaseChild:
				case <-run.Done():
					return run.Err()
				}
				return r.Commit(run, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			})
			registry := NewRegistry()
			if err := registry.Install(&Extension{Name: "test.compaction-hook", Tasks: []*TaskDefinition{work}, CompactionHooks: CompactionHooks{
				BeforeCompact: func(run context.Context, _ BeforeCompactInput, api *HookAPI) (*CompactionDecision, error) {
					hooks.Add(1)
					if mode == "decline" {
						return &CompactionDecision{Decline: true}, nil
					}
					if owned {
						if err := api.Commit(run, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
							_, err := tx.CreateTask(work, nil, TaskOptions{Conversation: api.ConversationID(), Ownership: TaskOwnership{Kind: "task", Task: api.TaskID()}})
							return nil, err
						}); err != nil {
							return nil, err
						}
					}
					text := summary
					return &CompactionDecision{Summary: &text}, nil
				},
			}}); err != nil {
				t.Fatal(err)
			}
			ref, options := setupFakeAPI(t, func(run context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
				out := make(chan goai.Event, 1)
				if strings.HasPrefix(input.SystemPrompt, "You are a context summarization assistant") {
					summaryCalls.Add(1)
					out <- terminal("PROVIDER")
					close(out)
				} else if genCalls.Add(1) == 4 && queued {
					busyEntered <- struct{}{}
					go func() {
						defer close(out)
						select {
						case <-releaseBusy:
							out <- terminal("busy answer")
						case <-run.Done():
						}
					}()
				} else {
					out <- terminal("history answer")
					close(out)
				}
				return out
			})
			options.Registry = registry
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, store, options)
			c := root(t, h, ref)
			submit := func(text string) *SubmissionHandle {
				t.Helper()
				sub, err := c.Submit(ctx, Input{Content: text})
				if err != nil {
					t.Fatal(err)
				}
				return sub
			}
			for _, text := range []string{"old one", "old two", "keep recent"} {
				if answered, err := submit(text).Wait(ctx); err != nil || answered.Record.Status != "done" {
					t.Fatal("history did not settle", answered.Record, err)
				}
			}
			configureCompactionTestPolicy(t, c, compactionTestInt(goai.EstimateMessageTokens(receiptMessage(userReceipt("keep recent")))), nil)
			var busy *SubmissionHandle
			if queued {
				busy = submit("busy question")
				watchReceive(t, busyEntered)
			}
			usage, err := c.Usage(ctx)
			if err != nil || len(usage.Models) != 1 || usage.Models[string(ref.Provider)+"/"+ref.ID].Input != 9 {
				t.Fatal("expected actual provider usage from three history turns", usage, err)
			}
			checkUsage := func() {
				t.Helper()
				if after, err := c.Usage(ctx); err != nil || !reflect.DeepEqual(after, usage) || summaryCalls.Load() != 0 {
					t.Fatal("hook decision added provider usage or dispatched a summary", usage, after, summaryCalls.Load(), err)
				}
			}
			decided := make(chan string, 2)
			observer, err := h.ObserveCommits(CommitObserver{Advance: func(_ context.Context, publication CommitPublication) error {
				for i := range publication.Changes.Len() {
					change, _ := publication.Changes.Index(i)
					item := generationObject(change.Copy())
					record := generationObject(item["value"])
					if item["type"] != "task" || record["kind"] != "pi.compaction" {
						continue
					}
					status, _ := generationObject(record["state"])["status"].(string)
					if status == "completing" || status == "terminal" {
						select {
						case decided <- status:
						default:
						}
					}
				}
				return nil
			}, End: func(WatchEnd) error { return nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer observer.Detach()
			id, err := c.Compact(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			var placement, childID ID
			checkPlaced := func(handle *SubmissionHandle) {
				t.Helper()
				placed, err := handle.Status(ctx)
				if err != nil || placed.Status != "done" || placed.Entry == 0 {
					t.Fatal("hook summary did not place", placed, err)
				}
				page, err := c.Entries(ctx, EntryQuery{Order: Ascending}, 24, nil)
				if err != nil || page.Next != nil || len(page.Items) == 0 {
					t.Fatal("missing short transcript", page, err)
				}
				last := page.Items[len(page.Items)-1]
				if last.Kind != "pi.compaction" || last.ID != placed.Entry || len(last.Model) != 1 || len(last.Model[0].Content) != 1 || last.Model[0].Content[0].Text != compactionSummaryPrefix+summary+"\n</summary>" {
					t.Fatal("wrong placed hook summary receipt", last)
				}
			}
			if owned {
				if got := watchReceive(t, decided); got != "completing" {
					t.Fatal("compaction did not hold for its owned child", got)
				}
				child := watchReceive(t, childEntered)
				if child.Owner != id || child.State.Status != "running" {
					t.Fatal("wrong child at execution gate", child)
				}
				childID = child.ID
				held, found, err := h.Task(ctx, id)
				outcome := held.State.Outcome
				if err != nil || !found || held.State.Status != "completing" || outcome == nil || outcome.Status != "completed" || outcome.Result == nil || !outcome.Result.Present {
					t.Fatal("wrong durable held compaction outcome", held, found, err)
				}
				placement = submissionEntryID(generationObject(outcome.Result.Value)["submissionId"])
				write, err := h.Submission(ctx, placement)
				if err != nil || write == nil {
					t.Fatal("missing held placement handle", write, err)
				}
				if queued {
					if s, err := write.Status(ctx); err != nil || s.Status != "queued" || s.Entry != 0 {
						t.Fatal("busy hook summary placed before generation boundary", s, err)
					}
				} else {
					checkPlaced(write)
				}
				if live, found, err := c.SnapshotDefinition(ctx, LiveDoc(), nil); err != nil || !found || generationObject(live.Copy())["compactions"] != nil {
					t.Fatal("settled compaction still appears live while held", live, found, err)
				}
				checkUsage()
				releaseTaskGate(releaseChild)
			}
			done, err := h.WaitForTask(ctx, id)
			outcome := done.State.Outcome
			if err != nil || done.State.Status != "terminal" || outcome == nil || outcome.Status != "completed" || outcome.Result == nil || !outcome.Result.Present || hooks.Load() != 1 {
				t.Fatal("wrong normal compaction hook completion", done, hooks.Load(), err)
			}
			checkUsage()
			if owned {
				child, found, err := h.Task(ctx, childID)
				if err != nil || !found || child.Owner != id || child.State.Status != "terminal" || child.State.Outcome == nil || child.State.Outcome.Status != "completed" {
					t.Fatal("compaction ended without its original child completing", child, found, err)
				}
			}
			if mode == "decline" {
				if !equalJSONValue(outcome.Result.Value, JSON{}) {
					t.Fatal("decline produced a placement", outcome.Result.Value)
				}
				if view, err := c.ContextView(ctx, 0); err != nil || view.Head != nil {
					t.Fatal("decline changed the model context head", view, err)
				}
				return
			}
			result := generationObject(outcome.Result.Value)
			finalID := submissionEntryID(result["submissionId"])
			if len(result) != 1 || finalID == 0 || (owned && finalID != placement) {
				t.Fatal("completion changed the decided placement", result, placement)
			}
			write, err := h.Submission(ctx, finalID)
			if err != nil || write == nil {
				t.Fatal("missing final placement handle", write, err)
			}
			if queued {
				if pending, err := write.Status(ctx); err != nil || pending.Status != "queued" || pending.Entry != 0 {
					t.Fatal("child completion prematurely placed the busy summary", pending, err)
				}
				releaseTaskGate(releaseBusy)
				if settled, err := busy.Wait(ctx); err != nil || settled.Record.Status != "done" {
					t.Fatal("busy generation did not finish normally", settled.Record, err)
				}
			}
			if settled, err := write.Wait(ctx); err != nil || settled.Record.Status != "done" {
				t.Fatal("summary write did not settle", settled.Record, err)
			}
			checkPlaced(write)
		})
	}
}
