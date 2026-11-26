package durable

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// harness-inbox.test.ts:675–700 keeps the original input across an onYield
// continuation, placing a previously queued model-visible write before it.
func TestSourceV161YieldContinuationKeepsOriginalInputAcrossWrite(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	var yields, requests atomic.Int64
	registry := NewRegistry()
	if err := registry.Install(&Extension{Name: "keep-going", Hooks: GenerationHooks{
		OnYield: func(context.Context, MessageReceipt, *HookAPI) (*Input, error) {
			if yields.Add(1) == 1 {
				return &Input{Content: "more"}, nil
			}
			return nil, nil
		},
	}}); err != nil {
		t.Fatal(err)
	}
	first, second := make(chan goai.Event, 1), make(chan goai.Event, 1)
	// The harness drains provider streams on close, including after a failed assertion.
	closeFirst := sync.OnceFunc(func() { close(first) })
	closeSecond := sync.OnceFunc(func() { close(second) })
	defer closeFirst()
	defer closeSecond()
	firstReached := make(chan struct{})
	secondRequest := make(chan []string, 1)
	ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, request *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		switch requests.Add(1) {
		case 1:
			close(firstReached)
			return first
		case 2:
			messages := make([]string, len(request.Messages))
			for i, m := range request.Messages {
				if len(m.Content) == 1 {
					messages[i] = string(m.Role) + ":" + m.Content[0].Text
				}
			}
			secondRequest <- messages
			return second
		default:
			ch := make(chan goai.Event, 1)
			ch <- terminal("unexpected request")
			close(ch)
			return ch
		}
	})
	options.Registry = registry
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, options)
	c := root(t, h, ref)
	input, err := c.Submit(ctx, Input{Content: "a"})
	if err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, firstReached)
	placed, err := input.Status(ctx)
	if err != nil || placed.Status != "placed" || placed.Entry == 0 || placed.Answer != 0 {
		t.Fatal("original input before the first answer", placed, err)
	}
	readRun := func() JSON {
		t.Helper()
		live, found, err := c.SnapshotDefinition(ctx, LiveDoc(), nil)
		if err != nil || !found {
			t.Fatal("missing LiveDoc during the request", found, err)
		}
		run := generationObject(generationObject(live.Copy())["run"])
		ids, _ := run["inputs"].([]any)
		if len(ids) != 1 || submissionEntryID(ids[0]) != input.ID() || submissionEntryID(run["first"]) != placed.Entry {
			t.Fatal("live run lost the original input or first entry", run)
		}
		return run
	}
	firstRun := readRun()

	note := "w"
	write, err := c.Submit(ctx, Input{Type: "write", Entry: &Entry{Kind: "note", Model: []MessageReceipt{{Role: goai.RoleUser, ContentText: &note}}}})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := write.Status(ctx)
	if err != nil || queued.Status != "queued" || queued.Entry != 0 || queued.Answer != 0 {
		t.Fatal("note must queue while the first answer is held", queued, err)
	}
	first <- terminal("first")
	closeFirst()

	type inputResult struct {
		settlement Settlement
		err        error
	}
	answered := make(chan inputResult, 1)
	waitExited := make(chan struct{})
	go func() {
		defer close(waitExited)
		settlement, err := input.Wait(ctx)
		answered <- inputResult{settlement, err}
	}()
	defer func() {
		cancel()
		<-waitExited
	}()
	select {
	case request := <-secondRequest:
		if want := []string{"user:a", "assistant:first", "user:w", "user:more"}; !slices.Equal(request, want) {
			t.Fatalf("continuation request = %q; want %q", request, want)
		}
	case result := <-answered:
		t.Fatalf("original input settled without a continuation: %+v, %v", result.settlement.Record, result.err)
	case <-ctx.Done():
		t.Fatal("waiting for continuation request", ctx.Err())
	}
	secondRun := readRun()
	if a, b := submissionEntryID(firstRun["taskId"]), submissionEntryID(secondRun["taskId"]); a == 0 || b == 0 || a == b {
		t.Fatal("expected a successor task in the same live run", a, b)
	}
	if status, err := input.Status(ctx); err != nil || status.Status != "placed" || status.Entry != placed.Entry || status.Answer != 0 {
		t.Fatal("original input settled before the continuation answered", status, err)
	}
	second <- terminal("second")
	closeSecond()

	result := <-answered
	if result.err != nil {
		t.Fatal(result.err)
	}
	page, err := c.Entries(ctx, EntryQuery{Order: Ascending}, 16, nil)
	if err != nil || page.Next != nil {
		t.Fatal("expected the entire short transcript", page.Next, err)
	}
	transcript := make([]string, len(page.Items))
	for i, entry := range page.Items {
		if len(entry.Model) != 1 {
			t.Fatal("expected one message per transcript entry", entry)
		}
		m := entry.Model[0]
		var text string
		if m.ContentText != nil {
			text = *m.ContentText
		} else if len(m.Content) == 1 {
			text = m.Content[0].Text
		} else {
			t.Fatal("expected one text in transcript message", m)
		}
		transcript[i] = entry.Kind + ":" + text
	}
	want := []string{"pi.user:a", "pi.assistant:first", "note:w", "pi.user:more", "pi.assistant:second"}
	if !slices.Equal(transcript, want) {
		t.Fatalf("committed transcript = %q; want %q", transcript, want)
	}
	if got := result.settlement.Record; got.ID != input.ID() || got.Status != "done" || got.Entry != page.Items[0].ID || got.Answer != page.Items[4].ID {
		t.Fatal("original input did not settle on the last answer", got)
	}
	if got, err := write.Status(ctx); err != nil || got.ID != write.ID() || got.Status != "done" || got.Entry != page.Items[2].ID || got.Answer != 0 {
		t.Fatal("note did not settle on its own entry", got, err)
	}
	if err := c.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || yields.Load() != 2 {
		t.Fatal("expected two requests and two yield calls, one continuation", requests.Load(), yields.Load())
	}
}
