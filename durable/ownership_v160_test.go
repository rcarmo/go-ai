package durable

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// harness-ownership.test.ts:656-673. Use the public conversation abort,
// not a scheduler mark or a subsequent WaitForTask: Abort must have joined
// the foreground subtree when it returns.
func TestSourceV160ConversationAbortJoinsOnlyForeground(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		started := make(chan ID, 4)
		hold := taskDefinition(t, "v160.ownership-hold", func(ctx context.Context, task TaskRecord, _ *TaskRuntime) error {
			started <- task.ID
			<-ctx.Done()
			return ctx.Err()
		})
		h := taskTestHarness(t, b.store, hold)
		root, err := h.Root(bg, AgentChange{})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		type tree struct{ owner, inner ID }
		create := func(label string, background bool) tree {
			t.Helper()
			value, err := root.Commit(bg, func(tx *Tx) (any, error) {
				owner, err := tx.CreateTask(hold, JSON{"name": label}, TaskOptions{
					Ownership: TaskOwnership{Kind: "conversation"}, Background: background,
				})
				if err != nil {
					return nil, err
				}
				child, err := tx.CreateConversation(JSON{"ownership": JSON{"kind": "task", "taskId": owner}})
				if err != nil {
					return nil, err
				}
				inner, err := tx.CreateTask(hold, JSON{"name": label + ".inner"}, TaskOptions{
					Conversation: child.ID, Ownership: TaskOwnership{Kind: "conversation"},
				})
				return tree{owner, inner}, err
			})
			if err != nil {
				t.Fatal(err)
			}
			return value.(tree)
		}
		foreground := create("foreground", false)
		background := create("background", true)
		wantStart := map[ID]bool{foreground.owner: true, foreground.inner: true, background.owner: true, background.inner: true}
		for range 4 {
			id := watchReceive(t, started)
			if !wantStart[id] {
				t.Fatal("unexpected hold invocation", id)
			}
			delete(wantStart, id)
		}
		if _, err := root.Commit(bg, func(tx *Tx) (any, error) {
			doc, err := tx.AcquireDocument(LiveDoc(), root.ID(), nil, nil)
			if err != nil {
				return nil, err
			}
			live, err := doc.Get()
			if err != nil {
				return nil, err
			}
			live["run"] = JSON{"taskId": foreground.owner, "inputs": []any{}, "turns": 0}
			return nil, doc.Set(live)
		}); err != nil {
			t.Fatal(err)
		}
		input, err := root.Submit(bg, Input{Type: "input", Content: "later"})
		if err != nil {
			t.Fatal(err)
		}
		note := "w"
		write, err := root.Submit(bg, Input{Type: "write", Entry: &Entry{
			Kind: "note", Model: []MessageReceipt{{Role: goai.RoleUser, ContentText: &note}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		for _, sub := range []*SubmissionHandle{input, write} {
			if record, err := sub.Status(bg); err != nil || record.Status != "queued" {
				t.Fatalf("before abort: submission %d: %+v, %v; want queued", sub.ID(), record, err)
			}
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if err := root.Abort(ctx); err != nil {
			t.Fatal(err)
		}
		for _, id := range []ID{foreground.owner, foreground.inner} {
			record, found, err := h.Task(bg, id)
			if err != nil || !found || record.State.Status != "terminal" {
				t.Errorf("foreground task %d after Abort: found=%t status=%q err=%v; want terminal", id, found, record.State.Status, err)
			}
		}
		for _, member := range []struct {
			name string
			id   ID
		}{{"owner", background.owner}, {"inner", background.inner}} {
			record, found, err := h.Task(bg, member.id)
			if err != nil || !found || record.State.Status == "terminal" {
				t.Errorf("background %s task %d after Abort: found=%t status=%q err=%v; want nonterminal", member.name, member.id, found, record.State.Status, err)
			}
		}
		if record, err := input.Status(bg); err != nil || record.Reason != "aborted" {
			t.Errorf("input after Abort: %+v, %v; want aborted reason", record, err)
		}
		if record, err := write.Status(bg); err != nil || record.Status != "queued" {
			t.Errorf("write after Abort: %+v, %v; want queued", record, err)
		}
		if _, err := h.AbortTask(bg, background.owner); err != nil {
			t.Fatal(err)
		}
	})
}

// harness-ownership.test.ts:582. The spawner stages a conversation and its
// first task, then tries to end itself in that same commit.
func TestSourceV160ConversationCreationRejectsEndingOwner(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var childRuns atomic.Int64
		child := taskDefinition(t, "v160.owner-child", func(context.Context, TaskRecord, *TaskRuntime) error {
			childRuns.Add(1)
			return errors.New("rejected child must not run")
		})
		spawner := taskDefinition(t, "v160.spawner", func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				conversation, err := tx.CreateConversation(JSON{"ownership": JSON{"kind": "task", "taskId": task.ID}})
				if err != nil {
					return nil, err
				}
				if _, err := tx.CreateTask(child, JSON{"name": "spawned"}, TaskOptions{Conversation: conversation.ID, Ownership: TaskOwnership{Kind: "conversation"}}); err != nil {
					return nil, err
				}
				return taskDone(nil), nil
			})
		})
		h := taskTestHarness(t, b.store, child, spawner)
		id := createPublicTask(t, h, spawner, JSON{"name": "spawned"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		got := waitPublicTask(t, h, id).State.Outcome
		want := fmt.Sprintf("Conversation owner task %d is ", id)
		if got == nil || got.Status != "faulted" || got.Result != nil || got.Reason != "" || got.Error == nil || got.Error.Detail != nil {
			t.Fatalf("spawner outcome: %+v, want faulted", got)
		}
		if message := got.Error.Message; message != want+"completing" && message != want+"terminal" {
			t.Fatalf("spawner fault: got %q, want %q followed by completing or terminal", message, want)
		}
		state, err := h.Snapshot(bg)
		if err != nil || len(state.Conversations) != 1 || len(state.Tasks) != 1 || childRuns.Load() != 0 {
			t.Fatalf("refused commit leaked: conversations=%d tasks=%d phases=%d err=%v", len(state.Conversations), len(state.Tasks), childRuns.Load(), err)
		}
	})
}

type conversationOwnerStorage struct {
	Storage
	owner   ID
	mode    string
	failure error
	armed   atomic.Bool
	reads   atomic.Int64
}

func (s *conversationOwnerStorage) Task(ctx context.Context, id ID) (TaskRecord, bool, error) {
	if id == s.owner && s.armed.Load() {
		s.reads.Add(1)
		switch s.mode {
		case "not-found":
			return TaskRecord{}, false, nil
		case "failed":
			return TaskRecord{}, false, s.failure
		}
	}
	return s.Storage.Task(ctx, id)
}

// Supplemental to transaction.ts's conversation creation and #currentTask.
// The delegated owner lookup runs against the backend even when the validation
// image already has a task row. A miss or read error must not admit a create.
func TestSourceV160ConversationCreationConsultsDelegatedOwner(t *testing.T) {
	for _, mode := range []string{"found", "not-found", "failed"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				owner := mint(t, b.store)
				record := foundationTask(owner, nil)
				apply(t, b.store, Write{Op: "put-task", Task: &record})
				adapter := &conversationOwnerStorage{Storage: b.store, mode: mode, owner: owner, failure: errors.New("owner backend read failed")}
				s, err := OpenSession(adapter)
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close(bg)
				var created Conversation
				_, err = s.Commit(bg, func(tx *Tx) (any, error) {
					adapter.armed.Store(true)
					defer adapter.armed.Store(false)
					var createErr error
					created, createErr = tx.CreateConversation(JSON{"ownership": JSON{"kind": "task", "taskId": owner}})
					return nil, createErr
				})
				switch mode {
				case "found":
					if err != nil || created.ID == 0 || created.Owner != owner || created.OwnerConversation != 1 {
						t.Fatalf("created %+v, err %v; want task %d in conversation 1", created, err, owner)
					}
					if got, found, err := s.Conversation(bg, created.ID); err != nil || !found || got != created {
						t.Fatalf("published %+v, found %t, err %v; want %+v", got, found, err, created)
					}
				case "not-found":
					var reject *StorageRejected
					if !errors.As(err, &reject) || reject.Reason != fmt.Sprintf("Conversation owner task %d does not exist", owner) || created != (Conversation{}) {
						t.Fatalf("missing owner: created %+v, err %v", created, err)
					}
				case "failed":
					if !errors.Is(err, adapter.failure) || created != (Conversation{}) {
						t.Fatalf("backend failure: created %+v, err %v", created, err)
					}
					ctx, cancel := context.WithTimeout(bg, 3*time.Second)
					defer cancel()
					end, err := s.Closed(ctx)
					if err != nil || end.Reason != "failed" || end.Failure.AsError() != adapter.failure {
						t.Fatalf("closed: %+v, err %v", end, err)
					}
				}
				if n := adapter.reads.Load(); n != 1 {
					t.Fatalf("delegated owner lookups = %d; want one", n)
				}
			})
		})
	}
}
