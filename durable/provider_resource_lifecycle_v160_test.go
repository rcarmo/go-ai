package durable

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

type resourceFailureStorage struct {
	Storage
	fail  atomic.Bool
	cause error
}

func (s *resourceFailureStorage) Commit(ctx context.Context, batches [][]StorageWrite, receive func(int, uint64)) (CommitResult, error) {
	if s.fail.Load() {
		return CommitResult{}, s.cause
	}
	return s.Storage.Commit(ctx, batches, receive)
}

// Source harness-lifecycle.test.ts:233, :262 and :280. Real public streams
// allocate fake resources under the actual durable provider identities.
func TestSourceV160HarnessProviderResourceLifecycle(t *testing.T) {
	for _, mode := range []string{"join", "failure", "scope"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(bg, 8*time.Second)
			defer cancel()
			var mu sync.Mutex
			live := map[string]bool{}
			var cleaned []string
			allocated := make(chan string, 4)
			ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, _ *goai.Context, options *goai.StreamOptions) <-chan goai.Event {
				mu.Lock()
				live[options.SessionID] = true
				mu.Unlock()
				allocated <- options.SessionID
				ch := make(chan goai.Event, 1)
				ch <- terminal("ok")
				close(ch)
				return ch
			})
			unregister := goai.RegisterSessionResourceCleanup(func(id string) error {
				mu.Lock()
				defer mu.Unlock()
				cleaned = append(cleaned, id)
				delete(live, id)
				return nil
			})
			defer unregister()
			state := func(wantCleaned []string, wantLive map[string]bool) {
				t.Helper()
				mu.Lock()
				defer mu.Unlock()
				if !reflect.DeepEqual(cleaned, wantCleaned) || !reflect.DeepEqual(live, wantLive) {
					t.Fatalf("provider resources: cleaned %v live %v; want cleaned %v live %v", cleaned, live, wantCleaned, wantLive)
				}
			}
			allocate := func(conversation *ConversationHandle) (string, Settlement) {
				t.Helper()
				sub, err := conversation.Submit(ctx, Input{Content: "allocate"})
				if err != nil {
					t.Fatal(err)
				}
				result, err := sub.Wait(ctx)
				if err != nil || result.Record.Status != "done" {
					t.Fatalf("allocation: %+v, %v", result.Record, err)
				}
				select {
				case id := <-allocated:
					if id == "" {
						t.Fatal("empty provider session ID")
					}
					return id, result
				case <-ctx.Done():
					t.Fatal(ctx.Err())
					return "", Settlement{}
				}
			}
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			failure := &resourceFailureStorage{Storage: store, cause: errors.New("disk gone")}
			h := openHarness(t, failure, options)
			conversation := root(t, h, ref)
			first, result := allocate(conversation)
			switch mode {
			case "join":
				entered, signalled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
				cleanupTaskGates(t, release)
				def := taskDefinition(t, "test.provider-cleanup-order", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
					close(entered)
					<-ctx.Done()
					close(signalled)
					<-release
					return ctx.Err()
				})
				if _, err := h.options.Registry.RegisterTask(def); err != nil {
					t.Fatal(err)
				}
				createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				awaitTaskSignal(t, entered)
				closing := make(chan error, 1)
				go func() { closing <- h.Close(ctx) }()
				awaitTaskSignal(t, signalled)
				select {
				case err := <-closing:
					t.Fatalf("close returned before actual host join: %v", err)
				default:
				}
				state(nil, map[string]bool{first: true})
				releaseTaskGate(release)
				if err := <-closing; err != nil {
					t.Fatal(err)
				}
				state([]string{first}, map[string]bool{})
			case "failure":
				failure.fail.Store(true)
				_, err := conversation.Commit(ctx, func(tx *Tx) (any, error) { return tx.AppendEntry(conversation.ID(), EntryDraft{"kind": "note"}) })
				if err != failure.cause {
					t.Fatalf("failing write: %v", err)
				}
				end, err := h.Closed(ctx)
				if err != nil || end.Reason != "failed" || end.Failure.AsError() != failure.cause {
					t.Fatalf("automatic close after failure: %+v, %v", end, err)
				}
				state([]string{first}, map[string]bool{})
			case "scope":
				extra, err := newTestSQLite(t)
				if err != nil {
					t.Fatal(err)
				}
				other := openHarness(t, extra, options)
				second, _ := allocate(root(t, other, ref))
				child, err := conversation.Fork(ctx, result.Record.Answer)
				if err != nil {
					t.Fatal(err)
				}
				childID, _ := allocate(child)
				if repeated, _ := allocate(conversation); repeated != first {
					t.Fatal("reused conversation changed provider session ID")
				}
				if _, err := h.CreateConversation(ctx, AgentChange{}); err != nil {
					t.Fatal(err)
				}
				if first == second || first == childID || second == childID {
					t.Fatal("distinct provider scopes share a session identifier")
				}
				if err := h.Close(ctx); err != nil {
					t.Fatal(err)
				}
				state([]string{first, childID}, map[string]bool{second: true})
				if err := other.Close(ctx); err != nil {
					t.Fatal(err)
				}
				state([]string{first, childID, second}, map[string]bool{})
			}
		})
	}
}
