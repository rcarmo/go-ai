package durable

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Translates f9a26365 harness-deferred-progress.test.ts:398-426. Existing
// compaction reopen coverage sleeps after a retry or reopens after completion;
// this one loses the process while the summary provider is actively running.
func TestMainHarnessSummarisationCrashResumesCheckpoint(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		entered := make(chan struct{})
		requests := make(chan string, 2)
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			n := calls.Add(1)
			ch := make(chan goai.Event, 1)
			if n <= 3 {
				ch <- terminal(fmt.Sprintf("a%d %s", n, strings.Repeat("word ", 100)))
				close(ch)
				return ch
			}
			if !strings.HasPrefix(input.SystemPrompt, "You are a context summarization assistant") || len(input.Messages) != 1 || len(input.Messages[0].Content) != 1 {
				t.Error("not a summary request", input)
			} else {
				requests <- input.Messages[0].Content[0].Text
			}
			if n == 4 {
				close(entered)
				go func() { <-ctx.Done(); close(ch) }()
				return ch
			}
			if n != 5 {
				t.Error("unexpected provider redispatch", n)
			}
			ch <- terminal("SUMMARY")
			close(ch)
			return ch
		})
		enabled, reserve, recent, background := false, 1000, 150, 0
		options.Settings = &HarnessSettings{Compaction: &CompactionSettings{Enabled: &enabled, ReserveTokens: &reserve, KeepRecentTokens: &recent, BackgroundTokens: &background}}
		h := openHarness(t, b.store, options)
		c := root(t, h, ref)
		for _, label := range []string{"u1", "u2", "u3"} {
			sub, err := c.Submit(bg, Input{Content: label + " " + strings.Repeat("word ", 100)})
			if err != nil {
				t.Fatal(err)
			}
			if result := waitSubmission(t, sub); result.Submission.Status != "done" {
				t.Fatal(result)
			}
		}
		id, err := c.Compact(bg, CompactionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		watchReceive(t, entered)
		firstRequest := watchReceive(t, requests)
		before, err := b.store.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		record, err := CanonicalTask(before.Tasks[id], h.session.limits)
		if err != nil || taskCheckpointMap(record.State.Checkpoint)["phase"] != "summarize" {
			t.Fatal("summary intent not stored before dispatch", record, err)
		}
		reopened := (&mainRecoveryHarness{t: t, h: h, store: b.store}).crash()
		next := openHarness(t, reopened, options)
		result := waitPublicTask(t, next, id)
		if result.State.Outcome == nil || result.State.Outcome.Status != "completed" {
			t.Fatal(result)
		}
		if got := watchReceive(t, requests); got != firstRequest {
			t.Error("recovery changed pinned summary input", got, firstRequest)
		}
		if calls.Load() != 5 {
			t.Error("request count", calls.Load())
		}
		if err := next.WaitForIdle(bg); err != nil {
			t.Fatal(err)
		}
		state, err := next.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, entry := range state.Entries {
			if entry.Conversation == c.ID() && entry.Kind == "pi.compaction" {
				count++
				if len(entry.Model) != 1 || len(entry.Model[0].Content) != 1 || !strings.Contains(entry.Model[0].Content[0].Text, "<summary>\nSUMMARY\n</summary>") {
					t.Error("wrong summary contribution", entry.Model)
				}
			}
		}
		if count != 1 {
			t.Error("compaction entries", count)
		}
		foundLive := false
		for _, doc := range state.Documents {
			if doc.Owner == c.ID() && doc.Kind == "pi.live" {
				foundLive = true
				if len(doc.Value) != 0 {
					t.Error("live state not cleared", doc.Value)
				}
			}
		}
		if !foundLive {
			t.Error("live document missing")
		}
		if err := next.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}
