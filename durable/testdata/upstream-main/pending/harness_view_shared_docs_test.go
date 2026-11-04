package durable

import (
	"context"
	"errors"
	"reflect"
	"testing"
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
