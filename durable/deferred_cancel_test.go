package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeferredAbortCancelsRemoteOnceAndReportsFailure(t *testing.T) {
	for _, mode := range []string{"success", "error", "panic"} {
		t.Run(mode, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				api := goai.Api("durable-cancel-" + strings.ReplaceAll(t.Name(), "/", "-"))
				var cancels, polls, reports atomic.Int64
				provider := &goai.ApiProvider{Api: api, Stream: func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
					ch := make(chan goai.Event, 1)
					ch <- &goai.DoneEvent{Reason: goai.StopReasonDeferred, Message: &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{}, StopReason: goai.StopReasonDeferred, Deferred: &goai.DeferredHandle{ID: "job", PollAfterMs: 100000}}}
					close(ch)
					return ch
				}, FetchDeferred: func(context.Context, *goai.Model, goai.DeferredHandle, *goai.StreamOptions) <-chan goai.Event {
					polls.Add(1)
					ch := make(chan goai.Event)
					close(ch)
					return ch
				}, CancelDeferred: func(_ context.Context, _ *goai.Model, handle goai.DeferredHandle, _ *goai.StreamOptions) error {
					cancels.Add(1)
					if handle.ID != "job" {
						t.Error("remote cancellation handle", handle)
					}
					if mode == "panic" {
						panic("host callback")
					}
					if mode == "error" {
						return errors.New("cancel failed")
					}
					return nil
				}}
				goai.RegisterApi(provider)
				t.Cleanup(func() { goai.UnregisterApi(api) })
				var reportMu sync.Mutex
				var reportErrors []error
				h := openHarness(t, b.store, Options{Models: func(goai.Provider, string) *goai.Model { return fakeModel(api) }, OnReport: func(err error) {
					reportMu.Lock()
					reportErrors = append(reportErrors, err)
					reportMu.Unlock()
					reports.Add(1)
				}})
				conversation := root(t, h, ModelRef{Provider: goai.ProviderOpenAI, ID: "durable-test"})
				sub, err := conversation.Submit(bg, Input{Content: "go"})
				if err != nil {
					t.Fatal(err)
				}
				deadline, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				var id ID
				for id == 0 {
					state, err := h.Snapshot(deadline)
					if err != nil {
						t.Fatal(err)
					}
					for _, task := range state.Tasks {
						if task.Kind == "pi.generation" {
							var cp generationCheckpoint
							if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
								t.Fatal(err)
							}
							if cp.Phase == "poll" {
								id = task.ID
							}
						}
					}
				}
				if status, err := h.AbortTask(deadline, id); err != nil || status != "marked" {
					t.Fatal(status, err)
				}
				result := waitSubmission(t, sub)
				if result.Submission.Status != "aborted" || cancels.Load() != 1 || polls.Load() != 0 {
					t.Fatal(result, cancels.Load(), polls.Load())
				}
				want := int64(0)
				if mode != "success" {
					want = 1
				}
				if reports.Load() != want {
					reportMu.Lock()
					t.Errorf("cancellation failure report got=%d want=%d errors=%v", reports.Load(), want, reportErrors)
					reportMu.Unlock()
					t.FailNow()
				}
				if status, err := h.AbortTask(bg, id); err != nil || status != "terminal" || cancels.Load() != 1 {
					t.Fatal("terminal repeated cancellation", status, err, cancels.Load())
				}
			})
		})
	}
}
