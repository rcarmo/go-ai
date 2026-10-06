package durable

import (
	"context"
	"sync"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestProviderSessionIdentityPersistsAcrossRequestsForkAndReopen(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var mu sync.Mutex
		var sessions []string
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, _ *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
			mu.Lock()
			sessions = append(sessions, opts.SessionID)
			mu.Unlock()
			ch := make(chan goai.Event, 1)
			ch <- terminal("answer")
			close(ch)
			return ch
		})
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		for _, input := range []string{"first", "second"} {
			sub, err := conversation.Submit(bg, Input{Content: input})
			if err != nil {
				t.Fatal(err)
			}
			waitSubmission(t, sub)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		cutoff := ID(0)
		for _, entry := range state.Entries {
			if entry.Conversation == conversation.ID() && entry.ID > cutoff {
				cutoff = entry.ID
			}
		}
		fork, err := conversation.Fork(bg, cutoff)
		if err != nil {
			t.Fatal(err)
		}
		child, err := h.Conversation(bg, fork.ID())
		if err != nil {
			t.Fatal(err)
		}
		sub, err := child.Submit(bg, Input{Content: "fork"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, sub)
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		reopened := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		original, err := reopened.Conversation(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		sub, err = original.Submit(bg, Input{Content: "reopened"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, sub)
		mu.Lock()
		defer mu.Unlock()
		if len(sessions) != 4 || sessions[0] == "" || sessions[0] != sessions[1] || sessions[0] == sessions[2] || sessions[0] != sessions[3] {
			t.Fatal("provider conversation identity", sessions)
		}
		if len(sessions[0]) != 36 || sessions[0][14] != '7' {
			t.Fatal("provider identity is not UUIDv7", sessions[0])
		}
	})
}

func TestProgressSettingsPresenceMergesAndDetaches(t *testing.T) {
	zero, large := int64(0), int64(5000)
	h := &Harness{options: Options{Settings: &HarnessSettings{Progress: &ProgressSettings{PartialIntervalMs: &large, OutputIntervalMs: &zero}}}}
	settings := h.resolvedSettings(RequestSettings{})
	if *settings.Progress.PartialIntervalMs != 5000 || *settings.Progress.OutputIntervalMs != 0 {
		t.Fatal(settings.Progress)
	}
	*settings.Progress.PartialIntervalMs = 7
	if *h.options.Settings.Progress.PartialIntervalMs != 5000 {
		t.Fatal("progress host settings aliased")
	}
	settings = h.resolvedSettings(RequestSettings{Progress: &ProgressSettings{PartialIntervalMs: &zero}})
	if *settings.Progress.PartialIntervalMs != 0 || *settings.Progress.OutputIntervalMs != 0 {
		t.Fatal("explicit zero lost", settings.Progress)
	}
	if progressDelayInterval(1024, 0) != 10*1000*1000 {
		t.Fatal("byte rate policy must remain with zero interval")
	}
}
