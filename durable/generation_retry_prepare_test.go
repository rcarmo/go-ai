package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestGenerationRetryRepreparesCommittedAgentToolsPromptAfterReopen(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var now atomic.Int64
		now.Store(1000)
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(_ context.Context, model *goai.Model, input *goai.Context, stream *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if calls.Add(1) == 1 {
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, ErrorMessage: "503 overloaded", Content: []goai.ContentBlock{}}}
			} else {
				if model.ID != "changed-model" || input.SystemPrompt != "changed prompt" || len(input.Tools) != 1 || input.Tools[0].Name != "changed-tool" || stream.Temperature == nil || *stream.Temperature != 0.7 {
					t.Errorf("retry stale preparation: model=%s prompt=%s tools=%+v options=%+v", model.ID, input.SystemPrompt, input.Tools, stream)
				}
				users := 0
				for _, message := range input.Messages {
					if message.Role == goai.RoleUser {
						users++
						if len(message.Content) != 1 || message.Content[0].Text != "question" {
							t.Errorf("retry input changed %+v", message)
						}
					}
				}
				if users != 1 {
					t.Errorf("retry duplicated/lost input %+v", input.Messages)
				}
				ch <- terminal("retried")
			}
			close(ch)
			return ch
		})
		baseModels := options.Models
		options.Models = func(provider goai.Provider, id string) *goai.Model {
			model := baseModels(provider, ref.ID)
			if model == nil {
				return nil
			}
			copy := *model
			copy.ID = id
			return &copy
		}
		options.Now = now.Load
		registry := NewRegistry()
		tool := wrapRegistration("changed-tool")
		if err := registry.Register(tool); err != nil {
			t.Fatal(err)
		}
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conversation, err := h.CreateConversation(bg, AgentChange{Model: ref, SystemPrompt: "old prompt", Settings: RequestSettings{Retry: RetryPolicy{Enabled: true, MaxRetries: 1, BaseDelayMs: 1000, MaxDelayMs: 60000}}})
		if err != nil {
			t.Fatal(err)
		}
		sub, err := conversation.Submit(bg, Input{Content: "question"})
		if err != nil {
			t.Fatal(err)
		}
		deadline, cancel := context.WithTimeout(bg, 3*time.Second)
		for {
			_, cp, ok, err := h.nextTask(conversation.ID())
			if err != nil {
				t.Fatal(err)
			}
			if ok && cp.Phase == "retry" {
				break
			}
			select {
			case <-deadline.Done():
				t.Fatal("retry checkpoint deadline", deadline.Err())
			default:
				runtime.Gosched()
			}
		}
		cancel()
		prompt, temperature := "changed prompt", 0.7
		model := ModelRef{Provider: ref.Provider, ID: "changed-model"}
		tools := []string{"changed-tool"}
		if err := conversation.ConfigurePatch(bg, AgentPatch{Model: &model, SystemPrompt: &prompt, Tools: &tools, Settings: &RequestSettings{Temperature: &temperature}}); err != nil {
			t.Fatal(err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		now.Store(2000)
		h = openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		handle, err := h.Submission(bg, sub.ID())
		if err != nil {
			t.Fatal(err)
		}
		if result := waitSubmission(t, handle); result.Submission.Status != "done" || calls.Load() != 2 {
			t.Fatal(result, calls.Load())
		}
	})
}
