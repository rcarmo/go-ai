package durable

import (
	"context"
	"testing"
)

func TestTaskOwnershipPinnedDecidedAndMarkedOwnersRejectNewWorkAtomically(t *testing.T) {
	for _, ending := range []string{"missing", "held", "terminal", "marked"} {
		t.Run(ending, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				runEntered, runRelease, childEntered, childRelease := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
				cleanupTaskGates(t, runRelease, childRelease)
				child := taskDefinition(t, "task.decided.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					close(childEntered)
					<-childRelease
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("child"), nil })
				})
				parent := taskDefinition(t, "task.decided.parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					if ending == "marked" {
						close(runEntered)
						<-runRelease
						return ctx.Err()
					}
					if ending == "held" {
						if err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
							_, err := tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
							return nil, err
						}); err != nil {
							return err
						}
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("parent"), nil })
				})
				h := taskTestHarness(t, b.store, child, parent)
				parentID := ID(MaxID)
				if ending != "missing" {
					parentID = createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				}
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				var abortDone chan error
				var callerCancel context.CancelFunc
				switch ending {
				case "terminal":
					waitPublicTask(t, h, parentID)
				case "held":
					awaitTaskSignal(t, childEntered)
				case "marked":
					awaitTaskSignal(t, runEntered)
					caller, cancel := context.WithCancel(bg)
					callerCancel = cancel
					abortDone = make(chan error, 1)
					go func() { _, err := h.AbortTask(caller, parentID); abortDone <- err }()
					for {
						state, err := h.Snapshot(bg)
						if err != nil {
							t.Fatal(err)
						}
						if taskAborted(state.Tasks[parentID]) {
							break
						}
					}
				}
				for _, creation := range []string{"child", "conversation"} {
					t.Run(creation, func(t *testing.T) {
						// Native MintID reservations are durable even on rejected
						// commits. Reserve before the no-write/no-highwater witness.
						var conversationID ID
						if creation == "conversation" {
							var err error
							conversationID, err = h.session.MintID(bg)
							if err != nil {
								t.Fatal(err)
							}
						}
						before, err := h.Snapshot(bg)
						if err != nil {
							t.Fatal(err)
						}
						_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
							if creation == "child" {
								_, err := tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: parentID}})
								return err
							}
							return tx.CreateConversation(Conversation{ID: conversationID, Owner: parentID})
						})
						if err == nil {
							t.Fatal("new work below decided owner", ending, creation)
						}
						after, err := h.Snapshot(bg)
						if err != nil || after.Seq != before.Seq || after.HighWater != before.HighWater || len(after.Tasks) != len(before.Tasks) || len(after.Conversations) != len(before.Conversations) {
							t.Fatal("rejected work changed state", err, before.Seq, after.Seq)
						}
					})
				}
				if ending == "held" {
					close(childRelease)
					waitPublicTask(t, h, parentID)
				}
				if ending == "marked" {
					callerCancel()
					close(runRelease)
					awaitTaskSignal(t, runEntered)
					<-abortDone
					waitPublicTask(t, h, parentID)
				}
			})
		})
	}
}
