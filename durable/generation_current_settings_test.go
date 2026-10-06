package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
)

func TestGenerationToolUseWithoutCallsIsFinalAnswer(t *testing.T) {
	for _, text := range []string{"", "text without calls"} {
		backends(t, func(t *testing.T, b backend) {
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				blocks := []goai.ContentBlock{}
				if text != "" {
					blocks = append(blocks, goai.ContentBlock{Type: "text", Text: text})
				}
				ch <- &goai.DoneEvent{Reason: goai.StopReasonToolUse, Message: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: blocks}}
				close(ch)
				return ch
			})
			h := openHarness(t, b.store, options)
			sub, err := root(t, h, ref).Submit(bg, Input{Content: "question"})
			if err != nil {
				t.Fatal(err)
			}
			result := waitSubmission(t, sub)
			if result.Submission.Status != "done" || result.Message == nil || result.Message.StopReason != goai.StopReasonToolUse {
				t.Fatal(result)
			}
			state, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			if len(state.Tasks) != 1 {
				t.Fatal("empty tool round created work", state.Tasks)
			}
		})
	}
}

func TestGenerationRetryDecisionReadsCurrentSettingsAfterResponse(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		cleanupTaskGates(t, release)
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			calls.Add(1)
			ch := make(chan goai.Event, 1)
			close(entered)
			go func() {
				<-release
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, ErrorMessage: "503 overloaded", Content: []goai.ContentBlock{}}}
				close(ch)
			}()
			return ch
		})
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		sub, err := conversation.Submit(bg, Input{Content: "question"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		settings := RequestSettings{Retry: RetryPolicy{Enabled: false, MaxRetries: 3, BaseDelayMs: 2000, MaxDelayMs: 60000}}
		if err := conversation.ConfigurePatch(bg, AgentPatch{Settings: &settings}); err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(release)
		if result := waitSubmission(t, sub); result.Submission.Status != "failed" || calls.Load() != 1 {
			t.Fatal(result, calls.Load())
		}
	})
}
