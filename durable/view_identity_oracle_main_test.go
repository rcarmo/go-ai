package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"reflect"
	"testing"
)

// Pinned f9a26365 harness-view.test.ts:525, content/window portion only. Shared
// docs identity is asserted separately through the readonly public API. These exact
// source window sizes and note kind do not use transcriptNotes' allocator-gap
// adaptation. Native supplies explicit entry IDs and an empty Value object.
func TestMainViewSource525IndependentWindowContents(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, conversation := transcriptFixture(t, b)
		ids := source525Notes(t, h.session, conversation.ID(), 6)
		left, err := detachedWatch(conversation, bg, transcriptBounds(2, 3))
		if err != nil {
			t.Fatal(err)
		}
		defer left.Stop()
		right, err := detachedWatch(conversation, bg, ConversationWindow{Min: source525Int(5)})
		if err != nil {
			t.Fatal(err)
		}
		defer right.Stop()
		a, err := left.Value()
		if err != nil {
			t.Fatal(err)
		}
		z, err := right.Value()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(source525IDs(a), ids[4:]) || !reflect.DeepEqual(source525IDs(z), ids[1:]) {
			t.Fatalf("initial windows: left=%v right=%v ids=%v", source525IDs(a), source525IDs(z), ids)
		}
		leftFrames := make(chan ConversationViewCopy, 2)
		rightFrames := make(chan ConversationViewCopy, 2)
		if err := left.Start(func(_ context.Context, v ConversationViewCopy, _ []Operation) error { leftFrames <- v; return nil }); err != nil {
			t.Fatal(err)
		}
		if err := right.Start(func(_ context.Context, v ConversationViewCopy, _ []Operation) error { rightFrames <- v; return nil }); err != nil {
			t.Fatal(err)
		}
		more := source525Notes(t, h.session, conversation.ID(), 1)
		a = watchReceive(t, leftFrames)
		z = watchReceive(t, rightFrames)
		wantLeft := append(append([]ID(nil), ids[4:]...), more...)
		wantRight := append(append([]ID(nil), ids[1:]...), more...)
		if !reflect.DeepEqual(source525IDs(a), wantLeft) || !reflect.DeepEqual(source525IDs(z), wantRight) {
			t.Fatalf("appended windows: left=%v right=%v", source525IDs(a), source525IDs(z))
		}
		if !reflect.DeepEqual(a.Docs, z.Docs) {
			t.Fatal("same-revision windows have different document contents")
		}
	})
}

func source525Notes(t *testing.T, session *Session, conversation ID, count int) []ID {
	t.Helper()
	ids := make([]ID, 0, count)
	for range count {
		checkpointCommit(t, session, func(tx *Tx) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.AppendEntry(Entry{ID: id, Conversation: conversation, Kind: "note", Value: JSON{}}); err != nil {
				return err
			}
			ids = append(ids, id)
			return nil
		})
	}
	return ids
}

func source525Int(n int) *int { return &n }

func source525IDs(value ConversationViewCopy) []ID {
	ids := make([]ID, len(value.Entries))
	for i, entry := range value.Entries {
		ids[i] = entry.ID
	}
	return ids
}

// Pinned harness-view.test.ts:330: public docs identity and peer lifetime.
// Readonly nodes give the source object-identity assertions a Go counterpart.
func TestMainSharedViewObserversShareDocsIdentity(t *testing.T) {
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
		if a.Docs != bValue.Docs {
			t.Error("source330 shared docs identity: independent observations expose different docs nodes")
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
		if value := watchReceive(t, frames); immutableViewMember(t, immutableViewMember(t, value.Docs, "pi.live"), "text").String() != "x" {
			t.Fatalf("healthy observer omitted the commit: %+v", value)
		}
		if end, closed := right.End(); closed {
			t.Fatalf("healthy observer ended: %+v", end)
		}
	})
}

// Source160 requires unchanged pi.live identity across a configuration frame,
// and unchanged docs identity across the following entry frame. Keep the two
// node comparisons exact, including the initial empty entries-array identity.
func TestMainSharedViewSource160UnchangedMapIdentity(t *testing.T) {
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
		if first.value.Entries.Len() != initial.Entries.Len() || !reflect.DeepEqual(immutableViewMember(t, first.value.Docs, "pi.live"), immutableViewMember(t, initial.Docs, "pi.live")) || !reflect.DeepEqual(second.value.Docs, first.value.Docs) {
			t.Fatal("unchanged parts differ in content")
		}
		if first.value.Entries != initial.Entries {
			t.Error("source160 unchanged entries identity was not preserved")
		}
		if immutableViewMember(t, first.value.Docs, "pi.live") != immutableViewMember(t, initial.Docs, "pi.live") {
			t.Error("source160 unchanged pi.live node identity was not preserved")
		}
		if second.value.Docs != first.value.Docs {
			t.Error("source160 unchanged docs node identity was not preserved")
		}
	})
}

// Source525's shared identity assertion. Independent window contents are also
// checked in TestMainViewSource525IndependentWindowContents.
func TestMainSharedViewSource525WindowDocsIdentity(t *testing.T) {
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
		if a.Docs != z.Docs {
			t.Error("source525 independent windows do not share docs node identity")
		}
	})
}

func immutableViewMember(t *testing.T, node ReadonlyValue, key string) ReadonlyValue {
	t.Helper()
	child, ok := node.Get(key)
	if !ok {
		t.Fatalf("missing readonly member %q", key)
	}
	return child
}

func TestMainSharedViewCanonicalEntryShape(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		source525Notes(t, h.session, c.ID(), 1)
		value, err := c.View(bg)
		if err != nil {
			t.Fatal(err)
		}
		entry, ok := value.Entries.Index(0)
		if !ok {
			t.Fatal("missing entry")
		}
		if _, ok := entry.Get("conversationId"); !ok {
			t.Fatal("public view exposes native entry envelope instead of upstream EntryRecord")
		}
		for _, key := range []string{"seq", "position", "conversation"} {
			if _, ok := entry.Get(key); ok {
				t.Fatalf("native-only field leaked: %s", key)
			}
		}
	})
}

func TestMainSharedViewPublicNodesHaveNoWritablePointerAuthority(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		_, c := transcriptFixture(t, b)
		w, err := c.Watch(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer w.Stop()
		value, err := w.Value()
		if err != nil {
			t.Fatal(err)
		}
		for _, node := range []any{value.Docs, value.Entries, immutableViewMember(t, value.Docs, "pi.live")} {
			reflected := reflect.ValueOf(node)
			if reflected.Kind() == reflect.Pointer || reflected.CanSet() {
				t.Fatal("public readonly handle exposes overwrite authority")
			}
		}
	})
}

func TestMainSharedViewMountRebuildAndLateAttachment(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		first, err := c.Watch(bg)
		if err != nil {
			t.Fatal(err)
		}
		initial, err := first.Value()
		if err != nil {
			t.Fatal(err)
		}
		frames := make(chan ConversationView, 2)
		if err := first.Start(func(_ context.Context, value ConversationView, _ []Operation) error { frames <- value; return nil }); err != nil {
			t.Fatal(err)
		}
		upstreamLiveText(t, h.session, c.ID(), "adopted")
		changed := watchReceive(t, frames)
		late, err := c.Watch(bg, transcriptBounds(1, 2))
		if err != nil {
			t.Fatal(err)
		}
		latest, err := late.Value()
		if err != nil {
			t.Fatal(err)
		}
		if latest.Docs != changed.Docs || latest.Docs == initial.Docs {
			t.Fatal("late observer did not reuse the adopted mount")
		}
		first.Stop()
		if _, closed := late.End(); closed {
			t.Fatal("stopping a peer destroyed the mount")
		}
		late.Stop()
		h.session.observerMu.Lock()
		remaining := len(h.session.subscriptions)
		h.session.observerMu.Unlock()
		if remaining != 0 {
			t.Fatal("last detach retained mount owners", remaining)
		}
		rebuilt, err := c.Watch(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer rebuilt.Stop()
		next, err := rebuilt.Value()
		if err != nil {
			t.Fatal(err)
		}
		if next.Docs == latest.Docs || !reflect.DeepEqual(next.Docs.Copy(), latest.Docs.Copy()) {
			t.Fatal("remount must rebuild equal documents with new identity")
		}
		keys := next.Docs.Keys()
		keys[0] = "caller overwrite"
		detached := next.Docs.Copy().(map[string]any)
		detached["pi.live"].(map[string]any)["text"] = "caller overwrite"
		if immutableViewMember(t, immutableViewMember(t, next.Docs, "pi.live"), "text").String() != "adopted" {
			t.Fatal("detached mutation escaped")
		}
	})
}

func TestMainSharedViewRetainedEntryIdentityThroughTrim(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		source525Notes(t, h.session, c.ID(), 3)
		w, err := c.Watch(bg, transcriptBounds(2, 3))
		if err != nil {
			t.Fatal(err)
		}
		defer w.Stop()
		initial, err := w.Value()
		if err != nil {
			t.Fatal(err)
		}
		frames := make(chan ConversationView, 2)
		if err := w.Start(func(_ context.Context, value ConversationView, _ []Operation) error { frames <- value; return nil }); err != nil {
			t.Fatal(err)
		}
		source525Notes(t, h.session, c.ID(), 1)
		grown := watchReceive(t, frames)
		for i := 0; i < initial.Entries.Len(); i++ {
			before, _ := initial.Entries.Index(i)
			after, _ := grown.Entries.Index(i)
			if before != after {
				t.Fatal("append copied a retained entry node")
			}
		}
		retained, _ := grown.Entries.Index(2)
		source525Notes(t, h.session, c.ID(), 1)
		trimmed := watchReceive(t, frames)
		first, _ := trimmed.Entries.Index(0)
		if trimmed.Entries.Len() != 2 || first != retained || trimmed.Docs != initial.Docs {
			t.Fatal("trim lost retained entry or document identity")
		}
	})
}
