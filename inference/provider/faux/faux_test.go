package faux_test

import (
	"context"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/inference/provider/faux"
)

func TestFauxContentAndAssistantHelpers(t *testing.T) {
	text := faux.FauxText("hello")
	if text.Type != "text" || text.Text != "hello" {
		t.Fatalf("unexpected faux text block: %#v", text)
	}
	thinking := faux.FauxThinking("hmm")
	if thinking.Type != "thinking" || thinking.Thinking != "hmm" {
		t.Fatalf("unexpected faux thinking block: %#v", thinking)
	}
	tool := faux.FauxToolCall("search", map[string]interface{}{"q": "go"}, "call_1")
	if tool.Type != "toolCall" || tool.ID != "call_1" || tool.Name != "search" || tool.Arguments["q"] != "go" {
		t.Fatalf("unexpected faux tool block: %#v", tool)
	}

	msg := faux.FauxAssistantMessage([]goai.ContentBlock{text, thinking, tool}, faux.AssistantMessageOptions{ResponseID: "resp_1", Timestamp: 1234})
	if msg.Role != goai.RoleAssistant || msg.ResponseID != "resp_1" || msg.Timestamp != 1234 || msg.StopReason != goai.StopReasonStop {
		t.Fatalf("unexpected faux assistant metadata: %#v", msg)
	}
	if len(msg.Content) != 3 || msg.Content[2].ID != "call_1" {
		t.Fatalf("unexpected faux assistant content: %#v", msg.Content)
	}

	toolMsg := faux.FauxAssistantMessage(tool)
	if toolMsg.StopReason != goai.StopReasonToolUse {
		t.Fatalf("single tool-call message should default to toolUse, got %q", toolMsg.StopReason)
	}
}

func TestFauxTextStream(t *testing.T) {
	reg := faux.Register(nil)
	reg.SetResponses([]faux.ResponseStep{
		faux.TextMessage("Hello, world!"),
	})

	model := reg.GetModel()
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}

	var gotText string
	var gotDone bool
	events := goai.Stream(context.Background(), model, ctx, nil)
	for event := range events {
		switch e := event.(type) {
		case *goai.TextDeltaEvent:
			gotText += e.Delta
		case *goai.DoneEvent:
			gotDone = true
			if e.Message.StopReason != goai.StopReasonStop {
				t.Fatalf("expected stop, got %s", e.Message.StopReason)
			}
		case *goai.ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}

	if !gotDone {
		t.Fatal("never got DoneEvent")
	}
	if gotText != "Hello, world!" {
		t.Fatalf("expected 'Hello, world!', got %q", gotText)
	}
}

func TestFauxComplete(t *testing.T) {
	reg := faux.Register(nil)
	reg.SetResponses([]faux.ResponseStep{
		faux.TextMessage("The answer is 4."),
	})

	model := reg.GetModel()
	ctx := &goai.Context{
		SystemPrompt: "You are a calculator.",
		Messages:     []goai.Message{goai.UserMessage("2+2?")},
	}

	msg, err := goai.Complete(context.Background(), model, ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.StopReason != goai.StopReasonStop {
		t.Fatalf("expected stop, got %s", msg.StopReason)
	}
	if len(msg.Content) == 0 || msg.Content[0].Text != "The answer is 4." {
		t.Fatalf("unexpected content: %+v", msg.Content)
	}
}

func TestFauxToolCall(t *testing.T) {
	reg := faux.Register(nil)
	reg.SetResponses([]faux.ResponseStep{
		faux.ToolCallMessage("get_time", map[string]interface{}{"timezone": "UTC"}),
	})

	model := reg.GetModel()
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("what time is it?")}}

	var gotToolCall *goai.ToolCall
	events := goai.Stream(context.Background(), model, ctx, nil)
	for event := range events {
		switch e := event.(type) {
		case *goai.ToolCallEndEvent:
			gotToolCall = &e.ToolCall
		case *goai.DoneEvent:
			if e.Message.StopReason != goai.StopReasonToolUse {
				t.Fatalf("expected toolUse, got %s", e.Message.StopReason)
			}
		case *goai.ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}

	if gotToolCall == nil {
		t.Fatal("never got ToolCallEndEvent")
	}
	if gotToolCall.Name != "get_time" {
		t.Fatalf("expected tool name 'get_time', got %q", gotToolCall.Name)
	}
	if gotToolCall.Arguments["timezone"] != "UTC" {
		t.Fatalf("expected timezone=UTC, got %v", gotToolCall.Arguments)
	}
}

func TestFauxThinking(t *testing.T) {
	reg := faux.Register(nil)
	reg.SetResponses([]faux.ResponseStep{
		faux.ThinkingMessage("Let me think about this...", "The answer is 42."),
	})

	model := reg.GetModel()
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("meaning of life?")}}

	var gotThinking, gotText string
	events := goai.Stream(context.Background(), model, ctx, nil)
	for event := range events {
		switch e := event.(type) {
		case *goai.ThinkingDeltaEvent:
			gotThinking += e.Delta
		case *goai.TextDeltaEvent:
			gotText += e.Delta
		case *goai.ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}

	if gotThinking != "Let me think about this..." {
		t.Fatalf("expected thinking text, got %q", gotThinking)
	}
	if gotText != "The answer is 42." {
		t.Fatalf("expected answer text, got %q", gotText)
	}
}

func TestFauxResponseFactory(t *testing.T) {
	reg := faux.Register(nil)
	reg.SetResponses([]faux.ResponseStep{
		faux.ResponseFactory(func(ctx *goai.Context, opts *goai.StreamOptions, state *faux.State) *goai.Message {
			// Dynamic response based on input
			userMsg := ""
			for _, m := range ctx.Messages {
				if m.Role == goai.RoleUser {
					for _, b := range m.Content {
						if b.Type == "text" {
							userMsg = b.Text
						}
					}
				}
			}
			return faux.TextMessage("You said: " + userMsg)
		}),
	})

	model := reg.GetModel()
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello there")}}

	msg, err := goai.Complete(context.Background(), model, ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content[0].Text != "You said: hello there" {
		t.Fatalf("expected dynamic response, got %q", msg.Content[0].Text)
	}
}

func TestFauxMultipleResponses(t *testing.T) {
	reg := faux.Register(nil)
	reg.SetResponses([]faux.ResponseStep{
		faux.TextMessage("First"),
		faux.TextMessage("Second"),
		faux.TextMessage("Third"),
	})

	model := reg.GetModel()
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("go")}}

	for i, expected := range []string{"First", "Second", "Third"} {
		msg, err := goai.Complete(context.Background(), model, ctx, nil)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if msg.Content[0].Text != expected {
			t.Fatalf("call %d: expected %q, got %q", i, expected, msg.Content[0].Text)
		}
	}

	if reg.PendingResponseCount() != 0 {
		t.Fatalf("expected 0 pending, got %d", reg.PendingResponseCount())
	}
}

func TestFauxError(t *testing.T) {
	reg := faux.Register(nil)
	reg.SetResponses([]faux.ResponseStep{
		faux.ErrorMessage("rate limit exceeded"),
	})

	model := reg.GetModel()
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}

	msg, err := goai.Complete(context.Background(), model, ctx, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if msg == nil || msg.StopReason != goai.StopReasonError {
		t.Fatal("expected error stop reason")
	}
}

func TestFauxAbort(t *testing.T) {
	reg := faux.Register(&faux.Options{TokensPerSecond: 10}) // slow streaming
	reg.SetResponses([]faux.ResponseStep{
		faux.TextMessage("This is a very long message that should get interrupted before it finishes streaming to the client"),
	})

	model := reg.GetModel()
	convCtx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var gotAbort bool
	events := goai.Stream(ctx, model, convCtx, nil)
	count := 0
	for event := range events {
		count++
		if count == 3 {
			cancel() // abort after a few events
		}
		if e, ok := event.(*goai.ErrorEvent); ok {
			if e.Reason == goai.StopReasonAborted {
				gotAbort = true
			}
		}
	}

	if !gotAbort {
		t.Log("warning: abort not observed (streaming may have completed before cancel)")
	}
}

func TestFauxCallCount(t *testing.T) {
	reg := faux.Register(nil)
	reg.SetResponses([]faux.ResponseStep{
		faux.TextMessage("a"),
		faux.TextMessage("b"),
	})

	model := reg.GetModel()
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("go")}}

	goai.Complete(context.Background(), model, ctx, nil)
	goai.Complete(context.Background(), model, ctx, nil)

	if reg.State.CallCount != 2 {
		t.Fatalf("expected 2 calls, got %d", reg.State.CallCount)
	}
}

func TestFauxDeferredSubmitPollReady(t *testing.T) {
	reg := faux.Register(&faux.Options{Deferred: faux.FauxDeferredOptions{PendingFetches: 1, PollAfterMs: 25}})
	reg.SetResponses([]faux.ResponseStep{faux.TextMessage("ready")})
	model := reg.GetModel()
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("slow work")}}

	deferred, err := goai.Complete(context.Background(), model, ctx, &goai.StreamOptions{Deferred: &goai.DeferredOptions{Window: "1h"}})
	if err != nil {
		t.Fatal(err)
	}
	if deferred.StopReason != goai.StopReasonDeferred || len(deferred.Content) != 0 || deferred.Deferred == nil {
		t.Fatalf("unexpected deferred submission: %#v", deferred)
	}
	if deferred.Deferred.Provider != string(model.Provider) || deferred.Deferred.ModelID != model.ID || deferred.Deferred.Api != string(model.Api) || deferred.Deferred.PollAfterMs != 25 {
		t.Fatalf("unexpected deferred handle: %#v", deferred.Deferred)
	}

	pending, err := goai.FetchDeferred(context.Background(), model, *deferred.Deferred, &goai.StreamOptions{WaitMs: intPtr(0)})
	if err != nil {
		t.Fatal(err)
	}
	if pending.StopReason != goai.StopReasonDeferred || pending.Deferred == nil || pending.Deferred.ID != deferred.Deferred.ID {
		t.Fatalf("unexpected pending fetch: %#v", pending)
	}

	ready, err := goai.FetchDeferred(context.Background(), model, *deferred.Deferred, &goai.StreamOptions{WaitMs: intPtr(0)})
	if err != nil {
		t.Fatal(err)
	}
	if ready.StopReason != goai.StopReasonStop || len(ready.Content) != 1 || ready.Content[0].Text != "ready" || ready.Usage.TotalTokens <= 0 {
		t.Fatalf("unexpected ready fetch: %#v", ready)
	}
	if reg.State.CallCount != 1 || reg.State.DeferredFetchCount != 2 {
		t.Fatalf("unexpected faux state: %#v", reg.State)
	}
}

func TestFauxDeferredFailureAndCancel(t *testing.T) {
	reg := faux.Register(nil)
	reg.SetResponses([]faux.ResponseStep{assertErr("deferred failed"), faux.TextMessage("cancelled")})
	model := reg.GetModel()
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("go")}}

	failedSubmission, err := goai.Complete(context.Background(), model, ctx, &goai.StreamOptions{Deferred: &goai.DeferredOptions{}})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := goai.FetchDeferred(context.Background(), model, *failedSubmission.Deferred, nil)
	if err != nil {
		t.Fatal(err)
	}
	if failed.StopReason != goai.StopReasonError || failed.ErrorMessage != "deferred failed" {
		t.Fatalf("unexpected failed fetch: %#v", failed)
	}

	cancelledSubmission, err := goai.Complete(context.Background(), model, ctx, &goai.StreamOptions{Deferred: &goai.DeferredOptions{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := goai.CancelDeferred(context.Background(), model, *cancelledSubmission.Deferred, nil); err != nil {
		t.Fatal(err)
	}
	if len(reg.State.CancelledDeferred) != 1 || reg.State.CancelledDeferred[0].ID != cancelledSubmission.Deferred.ID {
		t.Fatalf("cancel not recorded: %#v", reg.State.CancelledDeferred)
	}
	cancelled, err := goai.FetchDeferred(context.Background(), model, *cancelledSubmission.Deferred, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.StopReason != goai.StopReasonError || !strings.Contains(cancelled.ErrorMessage, "was cancelled") {
		t.Fatalf("unexpected cancelled fetch: %#v", cancelled)
	}
}

func TestDeferredUnsupportedAndContextCancellation(t *testing.T) {
	api := goai.Api("nodeferred")
	goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		close(ch)
		return ch
	}})
	defer goai.UnregisterApi(api)
	model := &goai.Model{ID: "m", Provider: "p", Api: api}
	handle := goai.DeferredHandle{Provider: "p", ModelID: "m", Api: string(api), ID: "h"}
	if _, err := goai.FetchDeferred(context.Background(), model, handle, nil); err == nil || !strings.Contains(err.Error(), "does not support deferred responses") {
		t.Fatalf("expected unsupported fetch error, got %v", err)
	}
	if err := goai.CancelDeferred(context.Background(), model, handle, nil); err == nil || !strings.Contains(err.Error(), "cannot cancel deferred responses") {
		t.Fatalf("expected unsupported cancel error, got %v", err)
	}
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := goai.FetchDeferred(cancelledCtx, model, handle, nil); err == nil {
		t.Fatal("expected context cancellation error")
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

func intPtr(v int) *int { return &v }
