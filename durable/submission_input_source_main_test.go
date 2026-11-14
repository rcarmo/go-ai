package durable

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Source harness-submissions.test.ts:57,82 and harness/types.ts:68–96.
// This also checks the accepted legacy Go spelling as a compatibility control.
func TestMainSubmissionSourceInputAdmissionAndBusyModes(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		select {
		case <-entered:
		default:
			close(entered)
		}
		go func() {
			select {
			case <-release:
				ch <- terminal("done")
			case <-ctx.Done():
			}
			close(ch)
		}()
		return ch
	})
	h := openHarness(t, store, options)
	t.Cleanup(unblock)
	c := root(t, h, ref)
	first, err := c.Submit(bg, Input{Type: "input", Content: "one", RequestID: "one"})
	if err != nil {
		t.Fatalf("source input admission: %v", err)
	}
	status, err := first.Status(bg)
	if err != nil || status.Type != "input" || status.Status != "placed" || status.Entry == 0 {
		t.Fatalf("initial: %+v %v", status, err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		unblock()
		t.Fatal("provider did not start")
	}
	before, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Submit(bg, Input{Type: "input", Content: "refuse", WhenBusy: "reject"}); !errors.Is(err, ErrConversationBusy) {
		t.Fatalf("busy reject: %v", err)
	}
	after, err := h.Snapshot(bg)
	if err != nil || after.Seq != before.Seq {
		t.Fatalf("rejected input wrote: %d %d %v", before.Seq, after.Seq, err)
	}
	dup, err := c.Submit(bg, Input{Type: "input", Content: "ignored", RequestID: "one", WhenBusy: "reject"})
	if err != nil || dup.ID() != first.ID() {
		t.Fatalf("dedup: %v %v", dup, err)
	}
	for _, mode := range []string{"followUp", "follow-up", "steer"} {
		sub, err := c.Submit(bg, Input{Type: "input", Content: mode, WhenBusy: mode})
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		status, err := sub.Status(bg)
		if err != nil || status.Status != "queued" || status.Type != "input" {
			t.Fatalf("%s status %+v %v", mode, status, err)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		value := mainContinueBoundaryDocument(t, state, c.ID(), "pi.inbox")
		items, ok := value["items"].([]any)
		if !ok || len(items) == 0 {
			t.Fatalf("%s inbox %v", mode, value)
		}
		item, ok := items[len(items)-1].(map[string]any)
		if !ok {
			t.Fatalf("%s item %T", mode, items[len(items)-1])
		}
		want := "followUp"
		if mode == "steer" {
			want = "steer"
		}
		if item["mode"] != want {
			t.Fatalf("%s queued mode %v want %s", mode, item["mode"], want)
		}
		if result, err := sub.Abort(bg); err != nil || result != "aborted" {
			t.Fatalf("%s abort %v %v", mode, result, err)
		}
	}
	unblock()
	if got := waitSubmission(t, first); got.Submission.Status != "done" {
		t.Fatalf("settlement %+v", got)
	}
}

// Literal harness-input-entries.test.ts:100-121: preserve the input content
// arm and placement clock, application data/kind, settlement and fork context.
func TestSourceIdleInputEntryPreservesStringContentAndPlacementClock(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan []goai.Message, 1)
	ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		requests <- input.Messages
		ch := make(chan goai.Event, 1)
		ch <- terminal("hi ann")
		close(ch)
		return ch
	})
	options.Now = func() int64 { return 42 }
	h := openHarness(t, store, options)
	c := root(t, h, ref)
	sub, err := c.Submit(bg, Input{Type: "input", Content: "ann: hi", Entry: &Entry{Kind: "chat.mention", HasData: true, Data: JSON{"author": "ann", "text": "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	settled := waitSubmission(t, sub)
	page, err := c.Entries(bg, EntryQuery{Order: "ascending"}, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	var placed, answer Entry
	for _, entry := range page.Items {
		if entry.Kind == "chat.mention" {
			placed = entry
		}
		if entry.Kind == "pi.assistant" {
			answer = entry
		}
	}
	if placed.ID == 0 || answer.ID == 0 || !placed.HasData || !equalJSONValue(placed.Data, JSON{"author": "ann", "text": "hi"}) {
		t.Fatal(page.Items)
	}
	var transcript []string
	for _, entry := range page.Items {
		if entry.Kind != "pi.system" {
			transcript = append(transcript, entry.Kind)
		}
	}
	if !reflect.DeepEqual(transcript, []string{"chat.mention", "pi.assistant"}) || len(answer.Model) != 1 || len(answer.Model[0].Content) != 1 || answer.Model[0].Content[0].Text != "hi ann" {
		t.Fatal("unexpected complete transcript", page.Items)
	}
	encoded, err := json.Marshal(placed.Model)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"role":"user","content":"ann: hi","timestamp":42}]`
	if !reflect.DeepEqual(dropinJSON(t, encoded), dropinJSON(t, []byte(want))) {
		t.Fatalf("model %s; expected %s", encoded, want)
	}
	if settled.Record.Status != "done" || settled.Record.Entry != placed.ID || settled.Record.Answer != answer.ID {
		t.Fatal(settled)
	}
	request := <-requests
	var users []goai.Message
	for _, message := range request {
		if message.Role == goai.RoleUser {
			users = append(users, message)
		}
	}
	if len(users) != 1 || len(users[0].Content) != 1 || users[0].Content[0].Text != "ann: hi" {
		t.Fatal(request)
	}
	view, err := c.ContextView(bg, placed.ID)
	if err != nil {
		t.Fatal(err)
	}
	var userModel []MessageReceipt
	for _, message := range view.Messages {
		if message.Role != goai.RoleSystem {
			userModel = append(userModel, message)
		}
	}
	wire, err := json.Marshal(userModel)
	if err != nil || !reflect.DeepEqual(dropinJSON(t, wire), dropinJSON(t, encoded)) {
		t.Fatal("historical context changed placed model", string(wire), err)
	}
}

// Source appendInput in harness/inbox.ts:128-138 copies the content union as-is.
// Paired original probe covers the same three literal arms, without normalization.
func TestSourceInputPreservesEmptyContentArms(t *testing.T) {
	for _, test := range []struct {
		name    string
		input   Input
		content string
	}{
		{"empty-string", Input{Type: "input", Content: ""}, `""`},
		{"empty-blocks", Input{Type: "input", Blocks: []goai.ContentBlock{}}, `[]`},
		{"empty-text-block", Input{Type: "input", Blocks: []goai.ContentBlock{{Type: "text", Text: ""}}}, `[{"type":"text","text":""}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				ch <- terminal("done")
				close(ch)
				return ch
			})
			options.Now = func() int64 { return 42 }
			h := openHarness(t, store, options)
			c := root(t, h, ref)
			sub, err := c.Submit(bg, test.input)
			if err != nil {
				t.Fatal(err)
			}
			settled := waitSubmission(t, sub)
			entry, ok, err := h.Entry(bg, settled.Record.Entry)
			if err != nil || !ok {
				t.Fatal(entry, ok, err)
			}
			encoded, err := json.Marshal(entry.Model)
			want := `[{"role":"user","content":` + test.content + `,"timestamp":42}]`
			if err != nil || !reflect.DeepEqual(dropinJSON(t, encoded), dropinJSON(t, []byte(want))) {
				t.Fatalf("placed model %s; expected %s; err=%v", encoded, want, err)
			}
		})
	}
}

func TestCanonicalInboxInputIsPlacementAuthority(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		go func() {
			select {
			case <-entered:
			default:
				close(entered)
			}
			select {
			case <-release:
				ch <- terminal("done")
			case <-ctx.Done():
			}
			close(ch)
		}()
		return ch
	})
	options.Now = func() int64 { return 42 }
	h := openHarness(t, store, options)
	t.Cleanup(unblock)
	c := root(t, h, ref)
	first, err := c.Submit(bg, Input{Content: "first"})
	if err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, entered)
	queued, err := c.Submit(bg, Input{Content: "private checkpoint", Entry: &Entry{Kind: "app.private"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.session.Commit(bg, func(tx *Tx) (any, error) {
		inbox, err := builtin(tx, c.ID(), "pi.inbox")
		if err != nil {
			return nil, err
		}
		return nil, inbox.Update(func(value JSON) error {
			for _, raw := range value["items"].([]any) {
				item := raw.(map[string]any)
				if submissionEntryID(item["id"]) == queued.ID() {
					item["content"] = []any{}
					item["entry"] = JSON{"kind": "app.canonical", "data": nil}
					return nil
				}
			}
			return reject("expected queued input")
		})
	}); err != nil {
		t.Fatal(err)
	}
	unblock()
	waitSubmission(t, first)
	settled := waitSubmission(t, queued)
	entry, ok, err := h.Entry(bg, settled.Record.Entry)
	if err != nil || !ok {
		t.Fatal(entry, ok, err)
	}
	model, err := json.Marshal(entry.Model)
	if err != nil || entry.Kind != "app.canonical" || !entry.HasData || entry.Data != nil || !reflect.DeepEqual(dropinJSON(t, model), dropinJSON(t, []byte(`[{"role":"user","content":[],"timestamp":42}]`))) {
		t.Fatal("private checkpoint replaced Inbox authority", entry, string(model), err)
	}
}
