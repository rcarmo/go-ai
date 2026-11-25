package durable

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// harness-after-run.test.ts:819–856. The owned task admits the follow-up in
// the same commit as its own entry and completion. Hold both that commit and
// the next provider's response so the causal boundaries can be read publicly.
func TestSourceV160AfterRunTaskAdmitsFixFollowUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	const label, fix = "run 1", "fix the tests"
	releaseChild, releaseFix := make(chan struct{}), make(chan struct{})
	defer releaseTaskGate(releaseChild)
	defer releaseTaskGate(releaseFix)
	type admission struct {
		id  ID
		err error
	}
	type requestReceipt struct {
		child TaskRecord
		found bool
		err   error
	}
	entered, admitted, fixStarted := make(chan TaskRecord, 1), make(chan admission, 1), make(chan requestReceipt, 1)
	var childCalls, requests, asks atomic.Int32
	var childID atomic.Uint64
	summaries := make(chan RunSummary, 2)
	childDef := taskDefinition(t, "test.run-tests", func(run context.Context, task TaskRecord, r *TaskRuntime) (runErr error) {
		var id ID
		if childCalls.Add(1) == 1 {
			entered <- task
			defer func() { admitted <- admission{id, runErr} }()
		}
		select {
		case <-releaseChild:
		case <-run.Done():
			return run.Err()
		}
		conversation, err := r.Conversation(run, r.ConversationID())
		if err != nil {
			return err
		}
		return r.Commit(run, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
			id, err = conversation.SubmitIn(tx, Input{Type: "input", Content: fix, RequestID: "fix-1", WhenBusy: "followUp"})
			if err != nil {
				return nil, err
			}
			input := generationObject(task.Input.Value)
			if _, err := tx.AppendEntry(conversation.ID(), EntryDraft{"kind": "test.tests", "data": input["label"]}); err != nil {
				return nil, err
			}
			return taskDone(input["label"]), nil
		})
	})
	registry := NewRegistry()
	if err := registry.Install(&Extension{Name: "after-run-fix", Tasks: []*TaskDefinition{childDef}, Hooks: GenerationHooks{
		AfterRun: func(_ context.Context, summary RunSummary, _ *HookAPI) (*AfterRunResult, error) {
			n := asks.Add(1)
			if n <= 2 {
				summaries <- summary
			}
			if n == 1 {
				return &AfterRunResult{Tasks: []RunEndTask{{Task: childDef, Input: JSON{"label": label}}}}, nil
			}
			return nil, nil
		},
	}}); err != nil {
		t.Fatal(err)
	}
	ref, options := setupFakeAPI(t, func(run context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		out := make(chan goai.Event, 1)
		switch requests.Add(1) {
		case 1:
			out <- terminal("done")
			close(out)
		case 2:
			child, found, err := store.Task(run, ID(childID.Load()))
			fixStarted <- requestReceipt{child, found, err}
			go func() {
				defer close(out)
				select {
				case <-releaseFix:
					out <- terminal("fixed")
				case <-run.Done():
				}
			}()
		default:
			close(out)
		}
		return out
	})
	options.Registry = registry
	h := openHarness(t, store, options)
	c := root(t, h, ref)
	original, err := c.Submit(ctx, Input{Type: "input", Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := original.Wait(ctx)
	if err != nil || first.Record.Status != "done" || first.Record.Answer == 0 {
		t.Fatal("original answer did not commit", first.Record, err)
	}
	start := watchReceive(t, entered)
	child, found, err := h.Task(ctx, start.ID)
	if err != nil || !found || child.Owner == 0 || child.Conversation != c.ID() || child.State.Status != "running" {
		t.Fatal("expected actual held after-run child", child, found, err)
	}
	read := func(want ...string) []Entry {
		t.Helper()
		page, err := c.Entries(ctx, EntryQuery{Order: Ascending}, 16, nil)
		if err != nil || page.Next != nil {
			t.Fatal("expected the entire short transcript", page.Next, err)
		}
		entries := slices.DeleteFunc(page.Items, func(entry Entry) bool { return entry.Kind == "pi.system" })
		kinds := make([]string, len(entries))
		for i, entry := range entries {
			kinds[i] = entry.Kind
		}
		if !slices.Equal(kinds, want) {
			t.Fatalf("transcript kinds=%q, want %q", kinds, want)
		}
		return entries
	}
	childID.Store(uint64(child.ID))
	initial := read("pi.user", "pi.assistant")
	if initial[1].ID != first.Record.Answer || initial[1].ByTask != child.Owner || requests.Load() != 1 {
		t.Fatal("follow-up started before its child wrote or original answer lost its owner", initial[1], child.Owner, requests.Load())
	}
	releaseTaskGate(releaseChild)
	receipt := watchReceive(t, admitted)
	if receipt.err != nil || receipt.id == 0 {
		t.Fatal("after-run task could not admit follow-up within its completion", receipt)
	}
	request := watchReceive(t, fixStarted)
	mid := read("pi.user", "pi.assistant", "test.tests", "pi.user")
	if mid[2].ByTask != child.ID || !mid[2].HasData || mid[2].Data != label || mid[2].Seq >= mid[3].Seq {
		t.Fatal("follow-up input did not follow the child's committed write", mid[2], mid[3])
	}
	ended := request.child
	if request.err != nil || !request.found || ended.ID != child.ID || ended.Owner != child.Owner || ended.State.Status != "terminal" || ended.State.Outcome == nil || ended.State.Outcome.Status != "completed" || ended.State.Outcome.Result == nil || !ended.State.Outcome.Result.Present || ended.State.Outcome.Result.Value != label {
		t.Fatal("fix provider started before the actual child completed", ended, request.found, request.err)
	}
	follow, err := h.Submission(ctx, receipt.id)
	if err != nil || follow == nil {
		t.Fatal("missing task-admitted follow-up handle", follow, err)
	}
	if status, err := follow.Status(ctx); err != nil || status.Status != "placed" || status.Entry != mid[3].ID || status.RequestID != "fix-1" || status.Answer != 0 {
		t.Fatal("wrong placed follow-up at fix request boundary", status, err)
	}
	fixGeneration := readOnlyHarnessRunID(t, c)
	if fixGeneration == child.Owner {
		t.Fatal("fix used original generation", fixGeneration)
	}

	releaseTaskGate(releaseFix)
	if err := c.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	entries := read("pi.user", "pi.assistant", "test.tests", "pi.user", "pi.assistant")
	text := func(entry Entry) string {
		t.Helper()
		if len(entry.Model) != 1 {
			t.Fatal("missing entry model message", entry)
		}
		m := entry.Model[0]
		if m.ContentText != nil {
			return *m.ContentText
		}
		if len(m.Content) != 1 || m.Content[0].Type != "text" {
			t.Fatal("expected one text block", m.Content)
		}
		return m.Content[0].Text
	}
	if got := []string{text(entries[0]), text(entries[3])}; !slices.Equal(got, []string{"go", fix}) {
		t.Fatal("wrong ordered user input texts", got)
	}
	if got := []string{text(entries[1]), text(entries[4])}; !slices.Equal(got, []string{"done", "fixed"}) {
		t.Fatal("wrong original and fix answers", got)
	}
	if asks.Load() != 2 || requests.Load() != 2 || childCalls.Load() != 1 {
		t.Fatal("wrong hook, provider, or child count", asks.Load(), requests.Load(), childCalls.Load())
	}
	if summary := watchReceive(t, summaries); summary != (RunSummary{Reason: "answer", First: entries[0].ID, Answer: entries[1].ID, Last: entries[1].ID}) {
		t.Fatal("wrong original hook summary", summary)
	}
	if summary := watchReceive(t, summaries); summary != (RunSummary{Reason: "answer", First: entries[3].ID, Answer: entries[4].ID, Last: entries[4].ID}) {
		t.Fatal("fix run summary did not start at its own input", summary)
	}
	if status, err := follow.Status(ctx); err != nil || status.Status != "done" || status.Entry != entries[3].ID || status.Answer != entries[4].ID {
		t.Fatal("wrong settled fix submission", status, err)
	}
	if status, err := original.Status(ctx); err != nil || status.Status != "done" || status.Answer != first.Record.Answer {
		t.Fatal("fix changed the original answer", status, err)
	}
	for _, want := range []struct{ generation, answer ID }{{child.Owner, entries[1].ID}, {fixGeneration, entries[4].ID}} {
		task, found, err := h.Task(ctx, want.generation)
		outcome := task.State.Outcome
		if err != nil || !found || task.State.Status != "terminal" || outcome == nil || outcome.Status != "completed" || outcome.Result == nil || !outcome.Result.Present || !equalJSONValue(outcome.Result.Value, JSON{"entryId": float64(want.answer)}) {
			t.Fatal("wrong completed generation receipt", task, found, err)
		}
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
}
