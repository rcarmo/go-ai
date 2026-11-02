package durable

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Deterministic adaptation of f9a26365 harness-deferred-progress.test.ts:100-115.
// Twenty 32-character prefixes reproduce its 640-character answer and ~20
// publication windows. The scripted provider waits for real watch delivery;
// no publication or stored receipt is fabricated by the test.
func TestMainHarnessDeferredProgressBatching(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		var clock atomic.Int64
		clock.Store(1000)
		type timer struct {
			due       int64
			fire      func()
			cancelled bool
		}
		var mu sync.Mutex
		var timers []*timer
		advance := func() {
			now := clock.Add(100)
			var jobs []func()
			mu.Lock()
			for _, timer := range timers {
				if !timer.cancelled && timer.due <= now {
					timer.cancelled = true
					jobs = append(jobs, timer.fire)
				}
			}
			mu.Unlock()
			for _, job := range jobs {
				job()
			}
		}
		events := make(chan goai.Event, 1)
		started := make(chan struct{})
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			close(started)
			return events
		})
		zero := int64(0)
		window := float64(400)
		options.Now = clock.Load
		options.Settings = &HarnessSettings{Progress: &ProgressSettings{PartialIntervalMs: &zero, PersistWithinMs: &window}}
		options.SetTimer = func(fire func(), delay time.Duration, opts TimerOptions) func() {
			if delay <= 0 {
				fire()
				return func() {}
			}
			timer := &timer{due: clock.Load() + delay.Milliseconds(), fire: fire}
			if !opts.Background {
				mu.Lock()
				timers = append(timers, timer)
				mu.Unlock()
			}
			return func() { mu.Lock(); timer.cancelled = true; mu.Unlock() }
		}
		h := openHarness(t, b.store, options)
		c := root(t, h, ref)
		_, frames := recordUpstreamWindow(t, c)
		var stores atomic.Int64
		core := h.session.core
		original := core.appendFrame
		installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
			changed := false
			if kind == 2 {
				var commit commitRecord
				if err := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &commit); err != nil {
					return err
				}
				for _, write := range commit.Writes {
					if write.Delta != nil && len(write.Delta.Ops) > 0 {
						changed = true
					}
					if write.Document != nil {
						if old, ok := core.state.Documents[write.Document.ID]; ok && !equalJSONValue(old.Value, write.Document.Value) {
							changed = true
						}
					}
				}
			}
			if original != nil {
				if err := original(kind, ordinal, high, payload); err != nil {
					return err
				}
			}
			if changed {
				stores.Add(1)
			}
			return nil
		})
		sub, err := c.Submit(bg, Input{Content: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		watchReceive(t, started)
		answer := strings.Repeat("streamed ", 400)[:640]
		memory := []string{}
		var previous JSON
		observe := func(frame upstreamWindowFrame) {
			live := frame.value.Docs["pi.live"]
			if !equalJSONValue(previous, live) {
				if text, ok := mainHarnessProgressText(live); ok {
					memory = append(memory, text)
				}
			}
			previous = live
		}
		for i := 1; i <= 20; i++ {
			advance()
			prefix := answer[:i*32]
			events <- &goai.TextDeltaEvent{Partial: &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: prefix}}}}
			for {
				frame := watchReceive(t, frames)
				observe(frame)
				if text, ok := mainHarnessProgressText(frame.value.Docs["pi.live"]); ok && text == prefix {
					break
				}
			}
			if i == 1 {
				stored, err := b.store.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				for _, doc := range stored.Documents {
					if doc.Kind == "pi.live" && doc.Owner == c.ID() {
						if text, ok := mainHarnessProgressText(doc.Value); ok {
							t.Errorf("first memory-only partial already stored: %q", text)
						}
					}
				}
			}
		}
		events <- terminal(answer)
		close(events)
		waitSubmission(t, sub)
		if err := h.WaitForIdle(bg); err != nil {
			t.Fatal(err)
		}
		fresh, err := c.View(bg)
		if err != nil {
			t.Fatal(err)
		}
		for !equalJSONValue(previous, fresh.Docs["pi.live"]) {
			observe(watchReceive(t, frames))
		}
		// Preserve both literal upstream assertions; count actual changed-document
		// batches across all documents, not only a filtered pi.live approximation.
		if len(memory) <= 12 {
			t.Errorf("memory partials=%d, want >12", len(memory))
		}
		if float64(stores.Load()) >= float64(len(memory))/1.5 {
			t.Errorf("stored document batches=%d memory partials=%d, want stores < memory/1.5", stores.Load(), len(memory))
		}
		t.Logf("source100: memory partials=%d stored document batches=%d", len(memory), stores.Load())
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}

func mainHarnessProgressText(live JSON) (string, bool) {
	generation, _ := live["generation"].(map[string]any)
	message, _ := generation["message"].(map[string]any)
	content, _ := message["content"].([]any)
	for _, block := range content {
		if part, ok := block.(map[string]any); ok && part["type"] == "text" {
			text, ok := part["text"].(string)
			return text, ok
		}
	}
	return "", false
}
