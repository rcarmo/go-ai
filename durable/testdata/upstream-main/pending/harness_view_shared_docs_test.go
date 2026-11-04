package durable

import (
	"context"
	"errors"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Pinned f9a26365 harness-view.test.ts:330 requires a.value.docs === b.value.docs.
// Value() is the native public accessor for the source's .value. Comparing map
// identities preserves that assertion; equal contents or isolated copies do not.
// Native's accessor deliberately deep-copies its result, so this oracle measures
// the public identity mismatch, not whether an internal mount could be shared.
// Keep the assertion pending until the identity/ownership contracts are reconciled.
//
// Compile via a project-owned Go overlay mapping zz_pending_view_identity_test.go
// to this file and the protected continuation prototype to a package-only stub.
func TestPendingViewObserversShareDocsIdentity(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, conversation := transcriptFixture(t, b)
		left, err := conversation.Watch(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer left.Stop()
		right, err := conversation.Watch(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer right.Stop()
		a, err := left.Value()
		if err != nil {
			t.Fatal(err)
		}
		bValue, err := right.Value()
		if err != nil {
			t.Fatal(err)
		}
		if a.Docs == nil || bValue.Docs == nil {
			t.Fatal("both observations must expose mounted documents")
		}
		if !reflect.DeepEqual(a.Docs, bValue.Docs) {
			t.Fatal("observations at the same revision have different document contents")
		}
		if reflect.ValueOf(a.Docs).UnsafePointer() != reflect.ValueOf(bValue.Docs).UnsafePointer() {
			t.Error("source330 shared docs identity: independent observations expose different docs maps")
		}

		// Continue past the identity failure so source330's listener-lifetime
		// assertions retain independent evidence instead of becoming unreachable.
		failure := errors.New("boom")
		if err := left.Start(func(context.Context, ConversationView, []Operation) error { return failure }); err != nil {
			t.Fatal(err)
		}
		frames := make(chan ConversationView, 2)
		if err := right.Start(func(_ context.Context, value ConversationView, _ []Operation) error {
			frames <- value
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		upstreamLiveText(t, h.session, conversation.ID(), "x")
		watchReceive(t, left.Closed())
		end, closed := left.End()
		if !closed || end.Reason != "listener_error" || end.Error != failure {
			t.Fatalf("failed listener ending: %+v closed=%v", end, closed)
		}
		if value := watchReceive(t, frames); value.Docs["pi.live"]["text"] != "x" {
			t.Fatalf("healthy observer omitted the commit: %+v", value)
		}
		if end, closed := right.End(); closed {
			t.Fatalf("healthy observer ended: %+v", end)
		}
	})
}

// Source160 requires unchanged pi.live identity across a configuration frame,
// and unchanged docs identity across the following entry frame. Keep the two
// map comparisons exact. The source's initial empty entries-array identity has
// no faithful Go slice-object counterpart and is not asserted here.
func TestPendingViewSource160UnchangedMapIdentity(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, conversation := transcriptFixture(t, b)
		// Initialise the native agent configuration before recording, as the
		// source's openChat(chatSetup()) does. Otherwise ConfigurePatch first
		// normalises the bare transcript fixture and emits a whole-agent set.
		if err := conversation.Configure(bg, AgentChange{}); err != nil {
			t.Fatal(err)
		}
		other, err := h.CreateConversation(bg, AgentChange{})
		if err != nil {
			t.Fatal(err)
		}
		watch, err := conversation.Watch(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		initial, err := watch.Value()
		if err != nil {
			t.Fatal(err)
		}
		type frame struct {
			value ConversationView
			ops   []Operation
		}
		frames := make(chan frame, 4)
		if err := watch.Start(func(_ context.Context, v ConversationView, ops []Operation) error {
			frames <- frame{v, ops}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		source525Notes(t, h.session, other.ID(), 1)
		level := goai.ModelThinkingLevel("high")
		if err := conversation.ConfigurePatch(bg, AgentPatch{ThinkingLevel: &level}); err != nil {
			t.Fatal(err)
		}
		source525Notes(t, h.session, conversation.ID(), 1)
		first, second := watchReceive(t, frames), watchReceive(t, frames)
		wantOps := []Operation{{"s", []any{"docs", "pi.agent", "thinkingLevel"}, "high"}}
		if !reflect.DeepEqual(first.ops, wantOps) || len(frames) != 0 {
			t.Fatalf("source160 touching frames: first ops=%v extra=%d", first.ops, len(frames))
		}
		if len(first.value.Entries) != len(initial.Entries) || !reflect.DeepEqual(first.value.Docs["pi.live"], initial.Docs["pi.live"]) || !reflect.DeepEqual(second.value.Docs, first.value.Docs) {
			t.Fatal("unchanged parts differ in content")
		}
		if reflect.ValueOf(first.value.Docs["pi.live"]).UnsafePointer() != reflect.ValueOf(initial.Docs["pi.live"]).UnsafePointer() {
			t.Error("source160 unchanged pi.live map identity was not preserved")
		}
		if reflect.ValueOf(second.value.Docs).UnsafePointer() != reflect.ValueOf(first.value.Docs).UnsafePointer() {
			t.Error("source160 unchanged docs map identity was not preserved")
		}
	})
}

// Source525's separate identity assertion. The exact window-content scenario is
// retained as a passing control in TestMainViewSource525IndependentWindowContents.
func TestPendingViewSource525WindowDocsIdentity(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, conversation := transcriptFixture(t, b)
		source525Notes(t, h.session, conversation.ID(), 6)
		left, err := conversation.Watch(bg, transcriptBounds(2, 3))
		if err != nil {
			t.Fatal(err)
		}
		defer left.Stop()
		right, err := conversation.Watch(bg, ConversationWindow{Min: source525Int(5)})
		if err != nil {
			t.Fatal(err)
		}
		defer right.Stop()
		frames := make(chan ConversationView, 2)
		if err := left.Start(func(_ context.Context, v ConversationView, _ []Operation) error { frames <- v; return nil }); err != nil {
			t.Fatal(err)
		}
		if err := right.Start(func(_ context.Context, v ConversationView, _ []Operation) error { frames <- v; return nil }); err != nil {
			t.Fatal(err)
		}
		source525Notes(t, h.session, conversation.ID(), 1)
		a, z := watchReceive(t, frames), watchReceive(t, frames)
		if !reflect.DeepEqual(a.Docs, z.Docs) {
			t.Fatal("same-revision windows differ in document contents")
		}
		if reflect.ValueOf(a.Docs).UnsafePointer() != reflect.ValueOf(z.Docs).UnsafePointer() {
			t.Error("source525 independent windows do not share docs map identity")
		}
	})
}
