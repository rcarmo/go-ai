package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Selected source assertions from official v1.5.0 f9a26365568127461c47a07a37dcb42c140a3ac5,
// source-continue.test.ts: "answers a run that failed, without a new user
// message" and "is rejected while busy, after request ID deduplication".
// Go adaptations: registered fake provider, memory/journal storage, detached
// Snapshot documents/entries and SubmissionRecord. No native Continue helper,
// checkpoint seeding or fresh/empty user input substitutes for public Submit.
// Cross-type rejection and final Status/Wait answer identity are derived checks.
type mainContinueOracleFixture struct {
	h       *Harness
	c       *ConversationHandle
	first   *SubmissionHandle
	seen    <-chan []goai.Message
	release chan struct{}
}

func mainContinueOracleSetup(t *testing.T, store Storage) mainContinueOracleFixture {
	t.Helper()
	seen := make(chan []goai.Message, 1)
	release := make(chan struct{})
	var calls atomic.Int64
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, request *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		switch calls.Add(1) {
		case 1:
			message := terminal("").Message
			message.Content = nil
			message.StopReason, message.ErrorMessage = goai.StopReasonError, "Invalid request"
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: message}
			close(ch)
		case 2:
			seen <- request.Messages
			go func() {
				defer close(ch)
				select {
				case <-release:
					ch <- terminal("recovered")
				case <-ctx.Done():
				}
			}()
		default:
			t.Error("unexpected extra provider request")
			close(ch)
		}
		return ch
	})
	h := openHarness(t, store, options)
	cleanupTaskGates(t, release)
	c := root(t, h, ref)
	first, err := c.Submit(bg, Input{Content: "Analyze this", RequestID: "failed-1"})
	if err != nil {
		t.Fatal(err)
	}
	if failed := waitSubmission(t, first); failed.Record.Status != "unanswered" || failed.Record.Reason != "model_error" {
		t.Fatalf("first wait = %+v; want unanswered/model_error", failed.Record)
	}
	state, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	failure, ok := mainContinueOracleLive(t, state, c.ID())["runFailure"].(map[string]any)
	if !ok || failure["reason"] != "model_error" {
		t.Fatalf("first runFailure = %v; want model_error", failure)
	}
	return mainContinueOracleFixture{h, c, first, seen, release}
}

func mainContinueOracleLive(t *testing.T, state Snapshot, conversation ID) JSON {
	t.Helper()
	for _, doc := range state.Documents {
		if !doc.Retired && doc.Kind == "pi.live" && doc.Owner == conversation {
			return doc.Value
		}
	}
	t.Fatal("missing pi.live document")
	return nil
}

func mainContinueOracleOneUser(t *testing.T, state Snapshot, conversation ID) {
	t.Helper()
	users := 0
	for _, entry := range state.Entries {
		if entry.Conversation == conversation && entry.Value["role"] == "user" {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("user entries = %d; want only the original user entry", users)
	}
}

func TestMainContinueFailedRunWithoutNewUserEntry(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		f := mainContinueOracleSetup(t, b.store)
		c := f.c
		resumed, err := c.Submit(bg, Input{Type: "continue", RequestID: "resume-1"})
		if err != nil {
			t.Fatalf("explicit continue Submit: %v", err)
		}
		placed, err := resumed.Status(bg)
		if err != nil || placed.Type != "continue" || placed.Status != "placed" || placed.Entry != 0 {
			t.Fatalf("continue admission = %+v, %v; want continue/placed with Entry=0", placed, err)
		}
		state, err := f.h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		mainContinueOracleOneUser(t, state, c.ID())
		// The provider gate makes placed status deterministic, and exposes the
		// actual request context rather than a reconstructed context helper.
		var messages []goai.Message
		select {
		case messages = <-f.seen:
		case <-time.After(3 * time.Second):
			t.Fatal("continue provider request did not arrive")
		}
		if len(messages) != 1 || messages[0].Role != goai.RoleUser || len(messages[0].Content) != 1 || messages[0].Content[0].Type != "text" || messages[0].Content[0].Text != "Analyze this" {
			t.Fatalf("continue provider context = %+v; want original user only, no fabricated empty user", messages)
		}
		releaseTaskGate(f.release)
		settled := waitSubmission(t, resumed)
		if settled.Record.Type != "continue" || settled.Record.Status != "done" || settled.Record.Entry != 0 || settled.Record.Answer == 0 {
			t.Fatalf("continue settlement = %+v; want continue/done, Entry=0 and answer", settled.Record)
		}
		final, err := resumed.Status(bg)
		if err != nil || final.Type != "continue" || final.Status != "done" || final.Entry != 0 || final.Answer != settled.Record.Answer {
			t.Fatalf("final status = %+v, %v; want same answer as Wait: %+v", final, err, settled.Record)
		}
		state, err = f.h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		answer, ok := state.Entries[settled.Record.Answer]
		// Native v1.4 message entries store their receipt in Value; Model is
		// the separate application-entry contribution field, not this format.
		var receipt MessageReceipt
		decodeErr := fromObject(answer.Value, &receipt, f.h.session.limits)
		if !ok || answer.Conversation != c.ID() || decodeErr != nil || receipt.Role != goai.RoleAssistant || len(receipt.Content) != 1 || receipt.Content[0].Type != "text" || receipt.Content[0].Text != "recovered" {
			t.Fatalf("persisted answer = %+v, decode=%v; want recovered", answer, decodeErr)
		}
		old, err := f.first.Status(bg)
		if err != nil || old.Status != "unanswered" {
			t.Fatalf("old input status = %+v, %v; must stay unanswered", old, err)
		}
		mainContinueOracleOneUser(t, state, c.ID())
		if failure, exists := mainContinueOracleLive(t, state, c.ID())["runFailure"]; exists {
			t.Fatalf("continue retained previous runFailure: %v", failure)
		}
		if err := f.h.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}

func TestMainContinueBusyRejectAndRequestIDDedup(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		f := mainContinueOracleSetup(t, b.store)
		c := f.c
		resumed, err := c.Submit(bg, Input{Type: "continue", RequestID: "resume-1"})
		if err != nil {
			t.Fatalf("explicit continue Submit: %v", err)
		}
		select {
		case <-f.seen:
		case <-time.After(3 * time.Second):
			t.Fatal("continue provider request did not arrive")
		}
		// No WhenBusy override: continue itself must reject while busy.
		if _, err := c.Submit(bg, Input{Type: "continue"}); !errors.Is(err, ErrConversationBusy) {
			t.Fatalf("busy continue = %v; want ErrConversationBusy", err)
		}
		duplicate, err := c.Submit(bg, Input{Type: "continue", RequestID: "resume-1"})
		if err != nil || duplicate.ID() != resumed.ID() {
			t.Fatalf("same request ID must dedup before busy: %v, %v", duplicate, err)
		}
		// Native follow-up is the public input adaptation. Reusing the continue
		// request ID as an input must reject, not dedup across types or report busy.
		_, err = c.Submit(bg, Input{Content: "different type", RequestID: "resume-1", WhenBusy: "reject"})
		var rejected *StorageRejected
		if !errors.As(err, &rejected) || rejected.Reason != "request ID cross-type conflict" {
			t.Fatalf("cross-type request ID = %v; want request ID cross-type conflict", err)
		}
		if err := f.h.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}
