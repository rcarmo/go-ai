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

func TestInboxSteerAfterToolsSharesAnswerAndFollowUpStartsNextRun(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		registry := NewRegistry()
		if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "hold", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "steer.hold", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			close(entered)
			<-release
			return ToolResult{Content: "tool result"}, nil
		}}); err != nil {
			t.Fatal(err)
		}
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, conv *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			call := calls.Add(1)
			ch := make(chan goai.Event, 1)
			switch call {
			case 1:
				ch <- toolAnswer("hold-call", "hold", JSON{})
			case 2:
				users := []string{}
				for _, message := range conv.Messages {
					if message.Role == goai.RoleUser {
						for _, block := range message.Content {
							users = append(users, block.Text)
						}
					}
				}
				if strings.Join(users, ",") != "first,steer" {
					t.Error("post-tools boundary inputs", users)
				}
				ch <- terminal("after tools")
			default:
				ch <- terminal("follow-up")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		conversation := root(t, h, ref)
		first, err := conversation.Submit(bg, Input{Content: "first"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		steer, err := conversation.Submit(bg, Input{Content: "steer", Type: "steer", RequestID: "s"})
		if err != nil {
			t.Fatal(err)
		}
		follow, err := conversation.Submit(bg, Input{Content: "follow"})
		if err != nil {
			t.Fatal(err)
		}
		state, err := h.Snapshot(bg)
		if err != nil || state.Submissions[steer.ID()].Status != "pending" {
			t.Fatal(state, err)
		}
		releaseTaskGate(release)
		a := waitSubmission(t, first)
		s := waitSubmission(t, steer)
		f := waitSubmission(t, follow)
		if a.Task.ID != s.Task.ID || a.Message == nil || s.Message == nil || a.Message.Content[0].Text != "after tools" || s.Message.Content[0].Text != "after tools" || f.Message.Content[0].Text != "follow-up" || calls.Load() != 3 {
			t.Fatal("steering settlements", a, s, f, calls.Load())
		}
		if err := h.WaitForIdle(bg); err != nil {
			t.Fatal(err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		recovered, err := second.Submission(bg, steer.ID())
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		result, err := recovered.Wait(ctx)
		if err != nil || result.Task.ID != a.Task.ID || calls.Load() != 3 {
			t.Fatal("steer reopen replay", result, err, calls.Load())
		}
	})
}
