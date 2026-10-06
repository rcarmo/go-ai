package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFollowUpQueueModeOneAndAllShareOnlySelectedAnswers(t *testing.T) {
	for _, mode := range []string{"one", "one-at-a-time", "all"} {
		t.Run(mode, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				entered, release := make(chan struct{}), make(chan struct{})
				var calls atomic.Int64
				ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					number := calls.Add(1)
					ch := make(chan goai.Event, 1)
					if number == 1 {
						go func() { close(entered); <-release; ch <- terminal("first"); close(ch) }()
						return ch
					}
					users := []string{}
					for _, message := range input.Messages {
						if message.Role == goai.RoleUser {
							for _, block := range message.Content {
								users = append(users, block.Text)
							}
						}
					}
					if mode == "all" && strings.Join(users, ",") != "first,a,b" {
						t.Error("all followups missing", users)
					}
					ch <- terminal("next")
					close(ch)
					return ch
				})
				h := openHarness(t, b.store, options)
				cleanupTaskGates(t, release)
				conversation, err := h.Root(bg, AgentChange{Model: ref, Settings: RequestSettings{FollowUpMode: mode}})
				if err != nil {
					t.Fatal(err)
				}
				first, err := conversation.Submit(bg, Input{Content: "first"})
				if err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, entered)
				a, err := conversation.Submit(bg, Input{Content: "a"})
				if err != nil {
					t.Fatal(err)
				}
				second, err := conversation.Submit(bg, Input{Content: "b"})
				if err != nil {
					t.Fatal(err)
				}
				releaseTaskGate(release)
				waitSubmission(t, first)
				ra, rb := waitSubmission(t, a), waitSubmission(t, second)
				expected := int64(3)
				if mode == "all" {
					expected = 2
					if ra.Task.ID != rb.Task.ID {
						t.Fatal("all did not share run", ra, rb)
					}
				} else if ra.Task.ID == rb.Task.ID {
					t.Fatal("one mode merged answers")
				}
				if calls.Load() != expected {
					t.Fatal("queue requests", calls.Load(), expected)
				}
			})
		})
	}
}
