package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBackgroundSubagentIdleBoundaryAndReopenDedup(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered := make(chan struct{})
		var calls atomic.Int64
		var complete atomic.Bool
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			calls.Add(1)
			ch := make(chan goai.Event, 1)
			if complete.Load() {
				ch <- terminal("finished")
				close(ch)
				return ch
			}
			go func() { close(entered); <-ctx.Done(); close(ch) }()
			return ch
		})
		first := openHarness(t, b.store, options)
		parent := root(t, first, ref)
		child, err := parent.CreateBackgroundConversation(bg, "worker", nil)
		if err != nil {
			t.Fatal(err)
		}
		repeated, err := parent.CreateBackgroundConversation(bg, "worker", nil)
		if err != nil || repeated.ID() != child.ID() {
			t.Fatal("background dedup", err)
		}
		sub, err := child.Submit(bg, Input{Content: "work", RequestID: "work"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		deadline, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if err := parent.WaitForIdle(deadline); err != nil {
			t.Fatal("parent idle waited background", err)
		}
		if err := parent.Abort(deadline); err != nil {
			t.Fatal("parent abort waited background", err)
		}
		state, err := first.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range state.Tasks {
			if task.Conversation == child.ID() && taskAborted(task) {
				t.Fatal("parent aborted subagent", task)
			}
		}
		if err := first.Close(bg); err != nil {
			t.Fatal(err)
		}
		complete.Store(true)
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		reopenedParent, err := second.Conversation(bg, parent.ID())
		if err != nil {
			t.Fatal(err)
		}
		found, err := reopenedParent.CreateBackgroundConversation(bg, "worker", nil)
		if err != nil || found.ID() != child.ID() {
			t.Fatal("reopen subagent duplicate", found, err)
		}
		handle, err := second.Submission(bg, sub.ID())
		if err != nil {
			t.Fatal(err)
		}
		result := waitSubmission(t, handle)
		if result.Submission.Status != "done" || calls.Load() != 2 {
			t.Fatal(result, calls.Load())
		}
	})
}

func TestBackgroundReporterRecoversDeliveryAndReportsOnce(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered := make(chan struct{})
		var complete atomic.Bool
		var childCalls, parentCalls atomic.Int64
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			last := input.Messages[len(input.Messages)-1].Content[0].Text
			if strings.Contains(last, "[subagent worker answered") {
				parentCalls.Add(1)
				if !strings.Contains(last, "child report") {
					t.Error("report text missing", last)
				}
				ch <- terminal("report acknowledged")
				close(ch)
			} else {
				childCalls.Add(1)
				if complete.Load() {
					ch <- terminal("child report")
					close(ch)
				} else {
					go func() { close(entered); <-ctx.Done(); close(ch) }()
				}
			}
			return ch
		})
		first := openHarness(t, b.store, options)
		parent := root(t, first, ref)
		child, err := parent.CreateBackgroundConversation(bg, "worker", nil)
		if err != nil {
			t.Fatal(err)
		}
		reporter, err := parent.SendBackground(bg, "worker", Input{Content: "work", RequestID: "job"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		duplicate, err := parent.SendBackground(bg, "worker", Input{Content: "ignored", RequestID: "job"})
		if err != nil || duplicate != reporter {
			t.Fatal("reporter dedup", duplicate, reporter, err)
		}
		deadline, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if err := parent.WaitForIdle(deadline); err != nil {
			t.Fatal("reporter crossed idle boundary", err)
		}
		if err := first.Close(bg); err != nil {
			t.Fatal(err)
		}
		complete.Store(true)
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		reopened, err := second.Conversation(bg, parent.ID())
		if err != nil {
			t.Fatal(err)
		}
		again, err := reopened.SendBackground(bg, "worker", Input{Content: "ignored after reopen", RequestID: "job"})
		if err != nil || again != reporter {
			t.Fatal("reopen reporter duplicate", again, err)
		}
		if record := waitPublicTask(t, second, reporter); record.State.Outcome.Status != "completed" {
			t.Fatal(record)
		}
		state, err := second.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		var report ID
		deliveries, reports := 0, 0
		for _, sub := range state.Submissions {
			if sub.Conversation == child.ID() {
				deliveries++
			}
			if strings.HasPrefix(sub.RequestID, "subagent-report:") {
				reports++
				report = sub.ID
			}
		}
		if reports != 1 || deliveries != 1 {
			t.Fatal("duplicate delivery/report", deliveries, reports)
		}
		handle, err := second.Submission(bg, report)
		if err != nil {
			t.Fatal(err)
		}
		if result := waitSubmission(t, handle); result.Submission.Status != "done" {
			t.Fatal(result)
		}
		if childCalls.Load() != 2 || parentCalls.Load() != 1 {
			t.Fatal("reporter effects", childCalls.Load(), parentCalls.Load())
		}
	})
}

func TestBackgroundReportersCoalescedSteersReportSharedAnswerOnce(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		entered, release := make(chan struct{}), make(chan struct{})
		if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "pause", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "reporter.pause", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			close(entered)
			<-release
			return ToolResult{Content: "done"}, nil
		}}); err != nil {
			t.Fatal(err)
		}
		var childRequests, parentRequests atomic.Int64
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			isReport := false
			for _, message := range input.Messages {
				for _, block := range message.Content {
					if strings.HasPrefix(block.Text, "[subagent worker answered") {
						isReport = true
					}
				}
			}
			if isReport {
				parentRequests.Add(1)
				ch <- terminal("ack")
			} else if childRequests.Add(1) == 1 {
				ch <- toolAnswer("pause-call", "pause", JSON{})
			} else {
				ch <- terminal("shared answer")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		options.OnReport = func(err error) { t.Log("reporter error", err) }
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		parent := root(t, h, ref)
		child, err := parent.CreateBackgroundConversation(bg, "worker", &AgentChange{Model: ref, Settings: RequestSettings{SteeringMode: "all"}})
		if err != nil {
			t.Fatal(err)
		}
		initial, err := child.Submit(bg, Input{Content: "first"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		a, err := parent.SendBackground(bg, "worker", Input{Content: "a", RequestID: "a"})
		if err != nil {
			t.Fatal(err)
		}
		bID, err := parent.SendBackground(bg, "worker", Input{Content: "b", RequestID: "b"})
		if err != nil {
			t.Fatal(err)
		}
		deadline, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for {
			state, err := h.Snapshot(deadline)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, sub := range state.Submissions {
				if sub.Conversation == child.ID() {
					count++
				}
			}
			if count == 3 {
				break
			}
		}
		releaseTaskGate(release)
		waitSubmission(t, initial)
		for _, id := range []ID{a, bID} {
			if record := waitPublicTask(t, h, id); record.State.Outcome.Status != "completed" {
				t.Fatal("coalesced reporter failed", record, record.State.Outcome.Error)
			}
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		reports := []ID{}
		for _, sub := range state.Submissions {
			if strings.HasPrefix(sub.RequestID, "subagent-report:") {
				reports = append(reports, sub.ID)
			}
		}
		if len(reports) != 1 {
			t.Fatal("shared answer reported more than once", reports)
		}
		handle, err := h.Submission(bg, reports[0])
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, handle)
		if childRequests.Load() != 2 || parentRequests.Load() != 1 {
			t.Fatal(childRequests.Load(), parentRequests.Load())
		}
	})
}

func TestStopBackgroundSuppressesReportAndChildStaysUsable(t *testing.T) {
	store, _ := NewMemory()
	entered := make(chan struct{})
	var calls atomic.Int64
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		n := calls.Add(1)
		ch := make(chan goai.Event, 1)
		if n == 1 {
			go func() { close(entered); <-ctx.Done(); close(ch) }()
		} else {
			ch <- terminal("fresh answer")
			close(ch)
		}
		return ch
	})
	h := openHarness(t, store, options)
	parent := root(t, h, ref)
	child, err := parent.CreateBackgroundConversation(bg, "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	reporter, err := parent.SendBackground(bg, "worker", Input{Content: "old", RequestID: "old"})
	if err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, entered)
	agents, err := parent.BackgroundAgents(bg)
	if err != nil || len(agents) != 1 || !agents[0].Busy || agents[0].Conversation != child.ID() {
		t.Fatal(agents, err)
	}
	deadline, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	if err := parent.StopBackground(deadline, "worker"); err != nil {
		t.Fatal(err)
	}
	if record := waitPublicTask(t, h, reporter); record.State.Outcome.Status != "completed" {
		t.Fatal(record)
	}
	state, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range state.Submissions {
		if strings.HasPrefix(sub.RequestID, "subagent-report:") {
			t.Fatal("aborted child report posted", sub)
		}
	}
	agents, err = parent.BackgroundAgents(bg)
	if err != nil || agents[0].Busy {
		t.Fatal("stopped child busy", agents, err)
	}
	again, err := parent.SendBackground(bg, "worker", Input{Content: "fresh", RequestID: "fresh"})
	if err != nil {
		t.Fatal(err)
	}
	if record := waitPublicTask(t, h, again); record.State.Outcome.Status != "completed" {
		t.Fatal(record)
	}
	state, err = h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	reports := 0
	for _, sub := range state.Submissions {
		if strings.HasPrefix(sub.RequestID, "subagent-report:") {
			reports++
			handle, err := h.Submission(bg, sub.ID)
			if err != nil {
				t.Fatal(err)
			}
			waitSubmission(t, handle)
		}
	}
	if reports != 1 || calls.Load() != 3 {
		t.Fatal("subagent did not recover after stop", reports, calls.Load())
	}
}
