package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// harness-submit-in-commit.test.ts:583–616. An ordinary conversation-owned
// task admits an idle input in the same commit that returns its submission ID.
func TestSourceV161TaskTerminalCommitAdmitsAttributedInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	const kind, text = "test.admitting", "from the task"
	release := make(chan struct{})
	defer releaseTaskGate(release)
	staged := make(chan ID, 1)
	var callerID atomic.Uint64
	task := taskDefinition(t, kind, func(run context.Context, record TaskRecord, runtime *TaskRuntime) error {
		callerID.Store(uint64(record.ID))
		bound, err := runtime.Conversation(run, runtime.ConversationID())
		if err != nil {
			return err
		}
		return runtime.Commit(run, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
			id, err := bound.SubmitIn(tx, Input{Type: "input", Content: text})
			if err != nil {
				return nil, err
			}
			select {
			case staged <- id:
			case <-run.Done():
				return nil, run.Err()
			}
			select {
			case <-release:
				return taskDone(id), nil
			case <-run.Done():
				return nil, run.Err()
			}
		})
	})
	type requestReceipt struct {
		task  TaskRecord
		found bool
		err   error
		text  string
	}
	started := make(chan requestReceipt, 1)
	var requests atomic.Int64
	ref, options := setupFakeAPI(t, func(run context.Context, _ *goai.Model, request *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		if requests.Add(1) == 1 {
			stored, found, err := store.Task(run, ID(callerID.Load()))
			var input string
			if len(request.Messages) == 1 {
				m := request.Messages[0]
				if m.Role == goai.RoleUser && len(m.Content) == 1 && m.Content[0].Type == "text" {
					input = m.Content[0].Text
				}
			}
			started <- requestReceipt{stored, found, err, input}
		}
		out := make(chan goai.Event, 1)
		out <- terminal("answer")
		close(out)
		return out
	})
	options.Registry = NewRegistry()
	if _, err := options.Registry.RegisterTask(task); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, options)
	c := root(t, h, ref)
	_, observer, err := h.SubscribeCommits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Stop()
	published := make(chan PublicationFrame, 1)
	if err := observer.Start(func(_ context.Context, frame PublicationFrame) error {
		for _, w := range frame.Publication.Tables {
			if w.Task != nil && w.Task.Kind == kind && terminalStatus(w.Task.Status) {
				select {
				case published <- frame:
				default:
				}
				break
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	created, err := c.Commit(ctx, func(tx *Tx) (any, error) {
		return tx.CreateTask(task, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	})
	taskID, ok := created.(ID)
	if err != nil || !ok || taskID == 0 {
		t.Fatal("could not create the admitting task", created, err)
	}
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	id := watchReceive(t, staged)
	if before, found, err := store.Task(ctx, taskID); err != nil || !found || before.State.Status != "running" {
		t.Fatal("task was not still running with its callback staged", before, found, err)
	}
	if premature, found, err := store.Submission(ctx, id); err != nil || found || requests.Load() != 0 {
		t.Fatal("task admission escaped its uncommitted callback", premature, found, requests.Load(), err)
	}
	releaseTaskGate(release)

	request := watchReceive(t, started) // No submission wait has requested progress.
	outcome := request.task.State.Outcome
	if request.err != nil || !request.found || request.task.ID != taskID || request.task.State.Status != "terminal" ||
		outcome == nil || outcome.Status != "completed" || outcome.Result == nil || !outcome.Result.Present || submissionEntryID(outcome.Result.Value) != id || request.text != text {
		t.Fatal("provider did not see the task's committed admission", request.task, request.text, id, request.found, request.err)
	}
	settled, err := h.WaitForTask(ctx, taskID)
	if err != nil || settled.State.Status != "terminal" || settled.State.Outcome == nil || settled.State.Outcome.Status != "completed" ||
		settled.State.Outcome.Result == nil || !settled.State.Outcome.Result.Present || submissionEntryID(settled.State.Outcome.Result.Value) != id {
		t.Fatal("wrong admitting task result", settled, id, err)
	}
	handle, err := h.Submission(ctx, id)
	if err != nil || handle == nil {
		t.Fatal("no handle for the admitted submission", id, err)
	}
	if err := h.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := handle.Status(ctx)
	if err != nil || status.Status != "done" || status.Type != "input" || status.ID != id || status.Entry == 0 || status.Answer == 0 || requests.Load() != 1 {
		t.Fatal("wrong admitted input settlement", status, requests.Load(), err)
	}
	userEntry, err := DefineEntry("pi.user")
	if err != nil {
		t.Fatal(err)
	}
	value, err := c.Commit(ctx, func(tx *Tx) (any, error) {
		entry, found, err := tx.TypedEntry(userEntry, status.Entry)
		if !found {
			return nil, err
		}
		return entry, err
	})
	entry, ok := value.(Entry)
	if err != nil || !ok || entry.ByTask != taskID || len(entry.Model) != 1 {
		t.Fatal("placed input not attributed to the admitting task", entry, taskID, err)
	}
	user := receiptMessage(entry.Model[0])
	if user.Role != goai.RoleUser || len(user.Content) != 1 || user.Content[0].Type != "text" || user.Content[0].Text != text {
		t.Fatal("wrong placed user message", user)
	}
	stored, found, err := store.Entry(ctx, status.Entry)
	if err != nil || !found || stored.Entry.ID != entry.ID || stored.CommitSeq == 0 {
		t.Fatal("placed input has no SQLite commit receipt", stored, found, err)
	}
	frame := watchReceive(t, published)
	var taskWrites, inputWrites, admissions int
	for _, w := range frame.Publication.Tables {
		if w.Task != nil && w.Task.ID == taskID && w.Task.Status == "done" {
			taskWrites++
		}
		if w.Entry != nil && w.Entry.ID == status.Entry && w.Entry.ByTask == taskID {
			inputWrites++
		}
		if w.Submission != nil && w.Submission.ID == status.ID {
			if now := publicSubmission(*w.Submission); now.Status == "placed" && now.Entry == status.Entry {
				admissions++
			}
		}
	}
	if frame.Publication.Seq != stored.CommitSeq || taskWrites != 1 || inputWrites != 1 || admissions != 1 {
		t.Fatal("task, submission and attributed input did not share one commit", frame.Publication.Seq, stored.CommitSeq, taskWrites, inputWrites, admissions)
	}
}
