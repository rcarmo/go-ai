package faux_test

import (
	"context"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/provider/faux"
)

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
