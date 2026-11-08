package durable

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Value/operation subset of pinned harness-view.test.ts:128-158. The complete
// stored-touch+1 persistence oracle remains a known failure, retained verbatim
// under testdata/upstream-main/pending and run explicitly through an overlay.
func TestMainViewUpstreamGenerationValueOperations(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		release := make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		defer unblock()
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			go func() {
				defer close(ch)
				select {
				case <-release:
					ch <- terminal("a longer answer")
				case <-ctx.Done():
				}
			}()
			return ch
		})
		h := openHarness(t, b.store, options)
		c := root(t, h, ref)
		initial, ch := recordUpstreamWindow(t, c)
		core := h.session.core
		var touching atomic.Int64
		original := core.appendFrame
		installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
			touched := false
			if kind == 2 {
				var commit commitRecord
				if err := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &commit); err != nil {
					return err
				}
				for _, w := range commit.Writes {
					if delta := w.Delta; delta != nil && len(delta.Ops) > 0 {
						d := core.state.Documents[delta.ID]
						if d.Scope == "conversation" && d.Owner == c.ID() {
							switch d.Kind {
							case "pi.agent", "pi.inbox", "pi.live", "pi.provider", "pi.usage":
								touched = true
							}
						}
					}
					if w.Entry != nil && w.Entry.Conversation == c.ID() {
						touched = true
					}
					if d := w.Document; d != nil && d.Owner == c.ID() && d.Scope == "conversation" {
						switch d.Kind {
						case "pi.agent", "pi.inbox", "pi.live", "pi.provider", "pi.usage":
							old, exists := core.state.Documents[d.ID]
							if !exists || !equalJSONValue(old.Value, d.Value) {
								touched = true
							}
						}
					}
				}
			}
			if original != nil {
				if err := original(kind, ordinal, high, payload); err != nil {
					return err
				}
			}
			if touched {
				touching.Add(1)
			}
			return nil
		})
		sub, err := c.Submit(bg, Input{Content: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		var frames []upstreamWindowFrame
		for {
			f := watchReceive(t, ch)
			frames = append(frames, f)
			if f.value.Docs["pi.live"]["generation"] != nil {
				break
			}
		}
		unblock()
		waitSubmission(t, sub)
		if err = h.WaitForIdle(bg); err != nil {
			t.Fatal(err)
		}
		fresh, err := c.View(bg)
		if err != nil {
			t.Fatal(err)
		}
		// Match the complete independently captured idle revision, not only
		// the last entry ID (later cleanup frames can retain that same ID).
		wantFinal, err := dtoObject(fresh, h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		for {
			f := watchReceive(t, ch)
			frames = append(frames, f)
			got, err := dtoObject(f.value, h.session.limits)
			if err != nil {
				t.Fatal(err)
			}
			if equalDeltaJSON(map[string]any(got), map[string]any(wantFinal)) {
				break
			}
		}
		replayUpstreamWindow(t, h.session, initial, frames)
		t.Logf("pending persistence oracle: frames=%d stored touches=%d; source requires touches+1", len(frames), touching.Load())
		assertUpstreamWindowFresh(t, h.session, c, frames[len(frames)-1].value, len(frames[len(frames)-1].value.Entries))
		foundUser, foundGeneration := false, false
		for _, op := range frames[0].ops {
			if len(op) == 5 && op[0] == "p" && equalJSONValue(op[1], []any{"entries"}) && equalJSONValue(op[2], 0) && equalJSONValue(op[3], 0) {
				owned, err := ownOperations([]Operation{op}, h.session.limits)
				if err != nil {
					t.Fatal(err)
				}
				items := owned[0][4].([]any)
				if len(items) == 1 {
					e := items[0].(map[string]any)
					foundUser = opItemIsUserMessage(e)
				}
			}
		}
		for _, f := range frames {
			for _, op := range f.ops {
				owned, err := ownOperations([]Operation{{"s", []any{"docs", "pi.live", "generation"}, JSON{"attempt": 1}}}, h.session.limits)
				if err != nil {
					t.Fatal(err)
				}
				if equalJSONValue([]any(op), []any(owned[0])) {
					foundGeneration = true
				}
			}
		}
		if !foundUser || !foundGeneration {
			t.Errorf("source user splice/generation tuple: user=%v generation=%v", foundUser, foundGeneration)
		}
	})
}
