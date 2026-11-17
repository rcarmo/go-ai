package durable

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Complete pinned harness-view.test.ts:128-158 oracle, with documented native
// provider/input/entry-envelope adaptations. Promoted unchanged after binding
// generation presentation to the runtime-owned document progress lane.
func TestMainViewUpstreamGenerationOracle(t *testing.T) {
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
		var touching atomic.Int64
		observer, err := h.ObserveCommits(CommitObserver{Advance: func(_ context.Context, frame CommitPublication) error {
			for i := range frame.Changes.Len() {
				node, _ := frame.Changes.Index(i)
				change := generationObject(node.Copy())
				if change["type"] == "entry" && submissionEntryID(generationObject(change["value"])["conversationId"]) == c.ID() {
					touching.Add(1)
					break
				}
				if change["type"] == "document" && change["kind"] == "change" && submissionEntryID(change["conversationId"]) == c.ID() {
					ops, _ := change["ops"].([]any)
					switch generationObject(change["record"])["kind"] {
					case "pi.agent", "pi.inbox", "pi.live", "pi.provider", "pi.usage":
						if len(ops) > 0 {
							touching.Add(1)
							return nil
						}
					}
				}
			}
			return nil
		}, End: func(WatchEnd) error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
		defer observer.Detach()
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
		fresh, err := detachedView(c, bg)
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
		if len(frames) != int(touching.Load())+1 {
			t.Errorf("source frames=stored touches+1: frames=%d stored touches=%d", len(frames), touching.Load())
		}
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
			for _, frame := range frames {
				t.Logf("ops: %v", frame.ops)
			}
		}
	})
}
