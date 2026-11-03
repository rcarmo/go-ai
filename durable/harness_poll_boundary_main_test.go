package durable

import (
	"context"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type mainPollFetch struct {
	*mainRecoveryStream
	ctx context.Context
}

type mainPollBoundary struct {
	h                          *Harness
	c                          *ConversationHandle
	store                      Storage
	options                    Options
	ref                        ModelRef
	clock                      atomic.Int64
	requests, fetches, cancels atomic.Int64
	streams                    chan *mainPollFetch
	cancelling                 chan struct{}
	blockCancel                atomic.Bool
	frames                     <-chan upstreamWindowFrame
	reports                    *mainAfterRecoveryProbe
	sub, task                  ID
}

func mainPollBoundarySetup(t *testing.T, store Storage, limit uint64, delay int64) *mainPollBoundary {
	t.Helper()
	f := &mainPollBoundary{store: store, streams: make(chan *mainPollFetch, 8), cancelling: make(chan struct{}, 8), reports: &mainAfterRecoveryProbe{}}
	f.clock.Store(1000)
	api := goai.Api("main-poll-boundary-" + strings.ReplaceAll(t.Name(), "/", "-"))
	goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		f.requests.Add(1)
		events := make(chan goai.Event, 1)
		events <- &goai.DoneEvent{Reason: goai.StopReasonDeferred, Message: &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{}, StopReason: goai.StopReasonDeferred, Deferred: &goai.DeferredHandle{ID: "job", PollAfterMs: int(delay)}}}
		close(events)
		return events
	}, FetchDeferred: func(ctx context.Context, _ *goai.Model, handle goai.DeferredHandle, _ *goai.StreamOptions) <-chan goai.Event {
		if handle.ID != "job" {
			t.Error("wrong fetched handle", handle)
		}
		f.fetches.Add(1)
		s := &mainPollFetch{mainRecoveryStream: &mainRecoveryStream{events: make(chan goai.Event)}, ctx: ctx}
		f.streams <- s
		return s.events
	}, CancelDeferred: func(ctx context.Context, _ *goai.Model, handle goai.DeferredHandle, _ *goai.StreamOptions) error {
		if handle.ID != "job" {
			t.Error("wrong cancelled handle", handle)
		}
		f.cancels.Add(1)
		f.cancelling <- struct{}{}
		if f.blockCancel.Load() {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}})
	t.Cleanup(func() { goai.UnregisterApi(api) })
	f.ref = ModelRef{Provider: goai.ProviderOpenAI, ID: "durable-test"}
	zero := int64(0)
	window := float64(0)
	disabled := false
	f.options = Options{Models: func(goai.Provider, string) *goai.Model { return fakeModel(api) }, Now: f.clock.Load, Settings: &HarnessSettings{RestartLimit: &limit, Retry: &RetrySettings{Enabled: &disabled}, Progress: &ProgressSettings{PartialIntervalMs: &zero, PersistWithinMs: &window}}}
	mainCutoffReports(f.reports, &f.options)
	f.h = openHarness(t, store, f.options)
	f.c = root(t, f.h, f.ref)
	if err := f.c.Configure(bg, AgentChange{Model: f.ref, Settings: RequestSettings{Deferred: &goai.DeferredOptions{PollAfterMs: int(delay)}}}); err != nil {
		t.Fatal(err)
	}
	_, f.frames = recordUpstreamWindow(t, f.c)
	sub, err := f.c.Submit(bg, Input{Content: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	f.sub = sub.ID()
	for {
		frame := watchReceive(t, f.frames)
		g := generationObject(frame.value.Docs["pi.live"]["generation"])
		if g["deferred"] != nil {
			break
		}
	}
	task, cp, ok, err := f.h.nextTask(f.c.ID())
	if err != nil || !ok || cp.Phase != "poll" || cp.Deferred == nil || cp.Deferred.ID != "job" || cp.PollAt != 1000+delay {
		t.Fatal("poll fixture", task, cp, err)
	}
	f.task = task.ID
	record, _, err := f.h.Task(bg, task.ID)
	if err != nil || record.State.Status != "sleeping" {
		t.Fatal("poll must sleep before fetch/abort", record, err)
	}
	return f
}
func (f *mainPollBoundary) fetch(t *testing.T) *mainPollFetch {
	t.Helper()
	f.clock.Store(1010)
	if err := f.h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	return watchReceive(t, f.streams)
}
func (f *mainPollBoundary) crashOpen(t *testing.T) {
	t.Helper()
	f.store = mainAfterRecoveryCrash(t, f.h, f.store)
	f.h, f.c = mainSalvageOpen(t, f.store, f.options, f.c.ID())
}
func (f *mainPollBoundary) noPartial(t *testing.T, state Snapshot) {
	t.Helper()
	live := mainAbortFailureLive(t, state, f.c.ID())
	if generationObject(live["generation"])["message"] != nil {
		t.Error("poll created a harness partial", live)
	}
	for _, entry := range state.Entries {
		if entry.Conversation == f.c.ID() && entry.Value["role"] == string(goai.RoleAssistant) {
			for _, block := range generationObject(entry.Value)["content"].([]any) {
				if generationObject(block)["text"] == "ignored poll delta" {
					t.Error("poll delta became transcript", entry)
				}
			}
		}
	}
}
func (f *mainPollBoundary) failure(t *testing.T, reason, detail string) {
	t.Helper()
	state, err := f.h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	live := mainAbortFailureLive(t, state, f.c.ID())
	failure := generationObject(live["runFailure"])
	status := mainCutoffStatus(t, f.h, f.sub)
	inputs, _ := failure["inputs"].([]any)
	if status.Status != "unanswered" || status.Reason != reason || status.Detail == nil || !equalJSONValue(status.Detail.Value, detail) {
		t.Error("input settlement", status)
	}
	// Native failedRun retains queued inputs after policy failures; explicit
	// aborts clear this extra compatibility field.
	wantLive := JSON{"runFailure": live["runFailure"]}
	if reason != "aborted" {
		wantLive["failedRun"] = true
	}
	if !equalJSONValue(live, wantLive) || len(failure) != 5 || failure["reason"] != reason || failure["detail"] != detail || submissionEntryID(failure["taskId"]) != f.task || len(inputs) != 1 || submissionEntryID(inputs[0]) != f.sub || submissionEntryID(failure["first"]) != status.Entry {
		t.Error("native failure shape/ownership", live, status)
	}
	if roles := mainSalvageRoles(t, state, f.c.ID()); !reflect.DeepEqual(roles, []goai.Role{goai.RoleUser}) {
		t.Error("unexpected partial transcript", roles)
	}
}

// Source-derived generation.ts332/handleResponse: fetchDeferred returns one
// message, unlike request's streamRequest. Native provider channels may contain
// deltas, but they must not create harness progress or recovered partials.
func TestMainPollTerminalOnlyBoundaryDerived(t *testing.T) {
	for _, mode := range []string{"terminal", "crash"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				f := mainPollBoundarySetup(t, b.store, 2, 10)
				stream := f.fetch(t)
				drained := make(chan struct{})
				go func() {
					defer close(drained)
					for _, event := range []goai.Event{&goai.TextDeltaEvent{Partial: &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "ignored poll delta"}}}}, &goai.StartEvent{}} {
						select {
						case stream.events <- event:
						case <-stream.ctx.Done():
							return
						}
					}
				}()
				watchReceive(t, drained) // The second receive witnesses processing the delta.
				state, err := f.h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				f.noPartial(t, state)
				stored, err := f.store.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				f.noPartial(t, stored)
				if mode == "terminal" {
					select {
					case stream.events <- terminal("terminal answer"):
					case <-stream.ctx.Done():
						t.Fatal("poll returned before terminal event")
					}
					stream.close()
					task := mainCutoffTask(t, f.h, f.task)
					if task.State.Outcome == nil || task.State.Outcome.Status != "completed" {
						t.Error("terminal poll task", task)
					}
					status := mainCutoffStatus(t, f.h, f.sub)
					if status.Status != "done" {
						t.Error(status)
					}
					state, err = f.h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					f.noPartial(t, state)
					roles := mainSalvageRoles(t, state, f.c.ID())
					if !reflect.DeepEqual(roles, []goai.Role{goai.RoleUser, goai.RoleAssistant}) {
						t.Error(roles)
					}
					found := false
					for _, entry := range state.Entries {
						if entry.Value["role"] == string(goai.RoleAssistant) {
							var m MessageReceipt
							if err := fromObject(entry.Value, &m, f.h.session.limits); err != nil {
								t.Fatal(err)
							}
							found = len(m.Content) == 1 && m.Content[0].Text == "terminal answer"
						}
					}
					if !found {
						t.Error("terminal answer missing")
					}
					if f.cancels.Load() != 0 {
						t.Error("successful fetch cancelled")
					}
				} else {
					f.crashOpen(t)
					if err := f.h.Resume(bg); err != nil {
						t.Fatal(err)
					}
					_ = mainCutoffTask(t, f.h, f.task)
					f.failure(t, "interrupted", "The host stopped while the deferred answer was fetched, on attempt 1")
					if f.cancels.Load() != 1 {
						t.Error("interrupted fetch cancellation", f.cancels.Load())
					}
				}
				if f.requests.Load() != 1 || f.fetches.Load() != 1 {
					t.Error("repeated request/fetch", f.requests.Load(), f.fetches.Load())
				}
			})
		})
	}
}

// Actual f9 harness-restart-failure.test.ts671: abort a sleeping deferred job,
// kill the host twice inside cancelDeferred, then fault the abort handler at2.
// Go callbacks return on context cancellation so the failed host can join.
func TestMainAbortedPollCancelHandlerCutoff(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		f := mainPollBoundarySetup(t, b.store, 2, 3600000)
		f.blockCancel.Store(true)
		abortDone := make(chan error, 1)
		go func() { abortDone <- f.c.Abort(bg) }()
		for crash := uint64(1); crash <= 2; crash++ {
			watchReceive(t, f.cancelling)
			f.crashOpen(t)
			record, _, err := f.h.Task(bg, f.task)
			if err != nil || record.Restarts != crash {
				t.Fatal("real abort-handler crash", record, err)
			}
			if err := f.h.Resume(bg); err != nil {
				t.Fatal(err)
			}
		}
		task := mainCutoffTask(t, f.h, f.task)
		detail := fmt.Sprintf("Abort handler of task %d (pi.generation) was cut off 2 times", f.task)
		f.failure(t, "aborted", detail)
		if task.State.Outcome == nil || task.State.Outcome.Status != "faulted" || task.State.Outcome.Error == nil || task.State.Outcome.Error.Message != detail || task.State.Outcome.Error.Detail == nil || !equalJSONValue(task.State.Outcome.Error.Detail.Value, JSON{"reason": "restart_limit"}) {
			t.Error("abort cutoff task", task)
		}
		if f.requests.Load() != 1 || f.fetches.Load() != 0 || f.cancels.Load() != 2 {
			t.Error("abort callback counts", f.requests.Load(), f.fetches.Load(), f.cancels.Load())
		}
		select {
		case <-abortDone:
		case <-time.After(3 * time.Second):
			t.Error("original abort caller did not join")
		}
		if err := f.h.Close(bg); err != nil {
			t.Fatal(err)
		}
		f.reports.mu.Lock()
		defer f.reports.mu.Unlock()
		found := false
		for _, info := range f.reports.infos {
			if info.Type == "task_faulted" && info.Reason == "restart_limit" && info.Task != nil && info.Task.ID == f.task {
				found = true
			}
		}
		if !found {
			t.Error("missing fault report", f.reports.infos)
		}
	})
}

// Source-derived endCutOff/salvageable: another host loss inside the normal
// generation cutoff's cancellation must skip that callback past the limit.
func TestMainPollCutoffSkipsRepeatedCancellationDerived(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		f := mainPollBoundarySetup(t, b.store, 1, 10)
		_ = f.fetch(t)
		f.blockCancel.Store(true)
		f.crashOpen(t)
		if err := f.h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		watchReceive(t, f.cancelling)
		f.crashOpen(t)
		record, _, err := f.h.Task(bg, f.task)
		if err != nil || record.Restarts != 2 {
			t.Fatal("real cutoff-handler crash", record, err)
		}
		mainSalvageCheck(t, f.h, f.c, f.sub, f.task, "The generation was interrupted 2 times in a row in its poll phase", false)
		if f.requests.Load() != 1 || f.fetches.Load() != 1 || f.cancels.Load() != 1 {
			t.Error("cutoff repeated callback", f.requests.Load(), f.fetches.Load(), f.cancels.Load())
		}
	})
}
