package durable

import (
	"context"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Official f9a26365 harness-continue.test.ts:187-201. Registered provider and
// native receipts adapt Faux/AssistantEntry; every outcome/count assertion stays.
func TestMainContinueNothingToAnswerEmptyAndAnswered(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			calls.Add(1)
			ch := make(chan goai.Event, 1)
			ch <- terminal("answered")
			close(ch)
			return ch
		})
		h := openHarness(t, b.store, options)
		c := root(t, h, ref)
		empty, err := c.Submit(bg, Input{Type: "continue"})
		if err != nil {
			t.Fatal(err)
		}
		first := waitSubmission(t, empty)
		if first.Record.Type != "continue" || first.Record.Status != "unanswered" || first.Record.Reason != "nothing_to_answer" || first.Record.Entry != 0 || first.Record.Answer != 0 || first.Message != nil {
			t.Errorf("empty continue = %+v, message=%+v; want entryless unanswered/nothing_to_answer", first.Record, first.Message)
		}
		input, err := c.Submit(bg, Input{Content: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		if got := waitSubmission(t, input); got.Record.Status != "done" {
			t.Fatal(got)
		}
		before := calls.Load()
		after, err := c.Submit(bg, Input{Type: "continue"})
		if err != nil {
			t.Fatal(err)
		}
		got := waitSubmission(t, after)
		if got.Record.Status != "unanswered" || got.Record.Reason != "nothing_to_answer" || got.Record.Entry != 0 || got.Record.Answer != 0 || got.Message != nil || got.Record.Detail != nil {
			t.Errorf("answered continue = %+v, message=%+v; want unanswered/nothing_to_answer without synthetic receipt", got.Record, got.Message)
		}
		if calls.Load() != before || before != 1 {
			t.Errorf("provider requests=%d before=%d; want exactly one input request", calls.Load(), before)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if failure := generationObject(mainContinueOracleLive(t, state, c.ID())["runFailure"]); failure["reason"] != "nothing_to_answer" {
			t.Error("missing exact failure", failure)
		}
		mainContinueOracleOneUser(t, state, c.ID())
	})
}

// Official f9a26365 harness-continue.test.ts:221-231: a length-limited text
// answer is still an answer. No additional provider request is permitted.
func TestMainContinueDoesNotResumeLengthLimitedText(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			calls.Add(1)
			ch := make(chan goai.Event, 1)
			event := terminal("cut")
			event.Message.StopReason = goai.StopReasonLength
			ch <- event
			close(ch)
			return ch
		})
		h := openHarness(t, b.store, options)
		c := root(t, h, ref)
		first, err := c.Submit(bg, Input{Content: "write a lot"})
		if err != nil {
			t.Fatal(err)
		}
		if got := waitSubmission(t, first); got.Record.Status != "done" {
			t.Fatal(got)
		}
		continued, err := c.Submit(bg, Input{Type: "continue"})
		if err != nil {
			t.Fatal(err)
		}
		got := waitSubmission(t, continued)
		if got.Record.Status != "unanswered" || got.Record.Reason != "nothing_to_answer" || got.Record.Entry != 0 || got.Record.Answer != 0 || calls.Load() != 1 {
			t.Errorf("cut-off text continue = %+v; provider calls=%d, want unanswered/nothing_to_answer and one call", got.Record, calls.Load())
		}
	})
}

// Source-derived no-model precedence/entrylessness control (prepare resolves
// model availability before checking whether there is anything to answer).
func TestMainContinueNoModelStillHasNoUserEntry(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h := openHarness(t, b.store, Options{})
		c, err := h.Root(bg, AgentChange{})
		if err != nil {
			t.Fatal(err)
		}
		sub, err := c.Submit(bg, Input{Type: "continue"})
		if err != nil {
			t.Fatal(err)
		}
		got := waitSubmission(t, sub)
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if got.Record.Type != "continue" || got.Record.Status != "unanswered" || got.Record.Reason != "no_model" || got.Record.Entry != 0 || got.Record.Answer != 0 || got.Message != nil || len(state.Entries) != 0 {
			t.Errorf("no-model continue=%+v, entries=%v; want no_model without entry or assistant", got.Record, state.Entries)
		}
	})
}

// Official f9a26365 harness-continue.test.ts:204-219. An output-limit tool
// call is not executed; its actual context contains a synthesized missing result.
func TestMainContinueLengthLimitedToolCall(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var calls atomic.Int64
		seen := make(chan []goai.Message, 1)
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, request *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if calls.Add(1) == 1 {
				event := toolAnswer("c1", "echo", JSON{})
				event.Message.Content = append([]goai.ContentBlock{{Type: "text", Text: "calling"}}, event.Message.Content...)
				event.Reason, event.Message.StopReason = goai.StopReasonLength, goai.StopReasonLength
				ch <- event
			} else {
				seen <- request.Messages
				ch <- terminal("resumed")
			}
			close(ch)
			return ch
		})
		h := openHarness(t, b.store, options)
		c := root(t, h, ref)
		first, err := c.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		if got := waitSubmission(t, first); got.Record.Status != "done" {
			t.Fatal(got)
		}
		continued, err := c.Submit(bg, Input{Type: "continue"})
		if err != nil {
			t.Fatal(err)
		}
		got := waitSubmission(t, continued)
		if got.Record.Status != "done" || got.Message == nil || len(got.Message.Content) != 1 || got.Message.Content[0].Text != "resumed" {
			t.Fatal(got)
		}
		messages := watchReceive(t, seen)
		n := len(messages)
		if n < 2 || messages[n-2].Role != goai.RoleAssistant || messages[n-1].Role != goai.RoleToolResult || messages[n-1].ToolCallID != "c1" || !messages[n-1].IsError || calls.Load() != 2 {
			t.Fatal("continue must answer the real missing-tool-result context", messages, calls.Load())
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range state.Tasks {
			if task.Kind == "pi.tool" {
				t.Error("output-limited call must never be executed", task)
			}
		}
	})
}

// Official f9a26365 harness-continue.test.ts:153-170. The trailing-system
// precondition is checked on the real ContextView, not invented in a fake request.
func TestMainContinueSkipsTrailingSystem(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			switch calls.Add(1) {
			case 1:
				ch <- terminal("ok")
			case 2:
				ch <- mainContinueOwnedFailure()
			case 3:
				ch <- terminal("resumed")
			default:
				t.Error("unexpected request")
			}
			close(ch)
			return ch
		})
		h := openHarness(t, b.store, options)
		instructions := "first version"
		c, err := h.Root(bg, AgentChange{Model: ref, Instructions: &instructions})
		if err != nil {
			t.Fatal(err)
		}
		first, err := c.Submit(bg, Input{Content: "first"})
		if err != nil {
			t.Fatal(err)
		}
		if got := waitSubmission(t, first); got.Record.Status != "done" {
			t.Fatal(got)
		}
		instructions = "second version"
		if err := c.ConfigurePatch(bg, AgentPatch{Instructions: &instructions}); err != nil {
			t.Fatal(err)
		}
		second, err := c.Submit(bg, Input{Content: "second"})
		if err != nil {
			t.Fatal(err)
		}
		if got := waitSubmission(t, second); got.Record.Status != "unanswered" || got.Record.Reason != "model_error" {
			t.Fatal(got)
		}
		view, err := c.ContextView(bg, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(view.Messages) == 0 || view.Messages[len(view.Messages)-1].Role != goai.RoleSystem {
			t.Fatal("source trailing-system precondition", view.Messages)
		}
		continued, err := c.Submit(bg, Input{Type: "continue"})
		if err != nil {
			t.Fatal(err)
		}
		got := waitSubmission(t, continued)
		if got.Record.Status != "done" || got.Message == nil || len(got.Message.Content) != 1 || got.Message.Content[0].Text != "resumed" || calls.Load() != 3 {
			t.Fatal(got, calls.Load())
		}
	})
}

// Official f9a26365 harness-continue.test.ts:290-304. The native resolver gate
// replaces JS same-stack scheduling so the follow-up is demonstrably queued
// behind the entryless run, without modifying that run or seeding storage.
func TestMainContinueNothingStartsQueuedInput(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if calls.Add(1) == 1 {
				ch <- terminal("first answered")
			} else {
				ch <- terminal("second answered")
			}
			close(ch)
			return ch
		})
		preparing, release := make(chan struct{}), make(chan struct{})
		var armed atomic.Bool
		resolve := options.Models
		options.Models = func(provider goai.Provider, id string) *goai.Model {
			if armed.CompareAndSwap(true, false) {
				close(preparing)
				<-release
			}
			return resolve(provider, id)
		}
		options.Settings = &HarnessSettings{AfterFailedRun: "continue"}
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		c := root(t, h, ref)
		first, err := c.Submit(bg, Input{Content: "first"})
		if err != nil {
			t.Fatal(err)
		}
		if got := waitSubmission(t, first); got.Record.Status != "done" {
			t.Fatal(got)
		}
		armed.Store(true)
		continued, err := c.Submit(bg, Input{Type: "continue"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, preparing)
		followUp, err := c.Submit(bg, Input{Content: "second", WhenBusy: "follow-up"})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := followUp.Status(bg); err != nil || got.Status != "queued" {
			t.Fatal("source queued precondition", got, err)
		}
		releaseTaskGate(release)
		if got := waitSubmission(t, continued); got.Record.Status != "unanswered" || got.Record.Reason != "nothing_to_answer" {
			t.Fatal(got)
		}
		got := waitSubmission(t, followUp)
		if got.Record.Status != "done" || got.Message == nil || len(got.Message.Content) != 1 || got.Message.Content[0].Text != "second answered" || calls.Load() != 2 {
			t.Fatal(got, calls.Load())
		}
	})
}

// Official f9a26365 harness-continue.test.ts:262-287. Reset is queued during
// the real failed request and placed by explicit admission, never seeded.
func TestMainContinuePlacesKeptReset(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		started, release := make(chan struct{}), make(chan struct{})
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			if calls.Add(1) != 1 {
				t.Error("nothing-to-answer dispatched a provider request")
			}
			ch := make(chan goai.Event, 1)
			close(started)
			go func() {
				defer close(ch)
				select {
				case <-release:
					ch <- mainContinueOwnedFailure()
				case <-ctx.Done():
				}
			}()
			return ch
		})
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		c := root(t, h, ref)
		first, err := c.Submit(bg, Input{Content: "first"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, started)
		if err := c.Reset(bg, ""); err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(release)
		if got := waitSubmission(t, first); got.Record.Status != "unanswered" || got.Record.Reason != "model_error" {
			t.Fatal(got)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		inboxItems := func(state Snapshot) []any {
			for _, doc := range state.Documents {
				if !doc.Retired && doc.Kind == "pi.inbox" && doc.Owner == c.ID() {
					items, _ := doc.Value["items"].([]any)
					return items
				}
			}
			return nil
		}
		if len(inboxItems(state)) != 1 {
			t.Fatal("source retained-reset precondition", inboxItems(state))
		}
		continued, err := c.Submit(bg, Input{Type: "continue"})
		if err != nil {
			t.Fatal(err)
		}
		if got := waitSubmission(t, continued); got.Record.Status != "unanswered" || got.Record.Reason != "nothing_to_answer" {
			t.Fatal(got)
		}
		state, err = h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if len(inboxItems(state)) != 0 {
			t.Fatal("reset left in inbox", inboxItems(state))
		}
		view, err := c.ContextView(bg, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, message := range view.Messages {
			if message.Role != goai.RoleSystem {
				t.Error("reset retained non-system context", view.Messages)
			}
		}
		if calls.Load() != 1 {
			t.Error("unexpected provider calls", calls.Load())
		}
	})
}
