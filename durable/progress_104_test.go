package durable

import (
	"context"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestGeneration104ConfiguredPartialIntervalProductionPath(t *testing.T) {
	for _, interval := range []int64{0, 5000} {
		t.Run(map[int64]string{0: "immediate", 5000: "delayed"}[interval], func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
				stream := make(chan goai.Event, 2)
				stream <- &goai.StartEvent{Partial: &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "partial"}}}}
				close(entered)
				go func() {
					select {
					case <-release:
						stream <- terminal("final")
					case <-ctx.Done():
					}
					close(stream)
				}()
				return stream
			})
			options.Settings = &HarnessSettings{Progress: &ProgressSettings{PartialIntervalMs: &interval}}
			store, _ := NewMemory()
			h := openHarness(t, store, options)
			resolved := h.resolvedSettings(RequestSettings{})
			if *resolved.Progress.PartialIntervalMs != interval {
				t.Fatalf("Open lost progress settings: got %d want %d", *resolved.Progress.PartialIntervalMs, interval)
			}
			cleanupTaskGates(t, release)
			published := make(chan AgentEvent, 1)
			watcher, err := h.WatchEvents(bg, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer watcher.Stop()
			if err := watcher.Start(func(_ context.Context, events []AgentEvent) error {
				for _, event := range events {
					if event.Type == "message_start" && event.Task != 0 && event.Message != nil && event.Message.Role == goai.RoleAssistant {
						select {
						case published <- event:
						default:
						}
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			sub, err := root(t, h, ref).Submit(bg, Input{Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			awaitTaskSignal(t, entered)
			if interval == 0 {
				select {
				case <-published:
				case <-time.After(3 * time.Second):
					t.Fatal("zero interval did not publish partial")
				}
			} else {
				// The stream is live for longer than the old 100ms default, then terminal
				// shutdown drops the pending trailing update before the configured 5s timer.
				select {
				case <-published:
					t.Fatal("configured delay ignored")
				case <-time.After(200 * time.Millisecond):
				}
			}
			releaseTaskGate(release)
			waitSubmission(t, sub)
		})
	}
}
