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

// Source view.ts observe()/attach(): synchronous membership, shared mount,
// acquisition-only cancellation and isolated guard termination.
func TestMainSharedViewSynchronousObservation(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		w, err := c.Watch(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer w.Stop()
		baseline, err := w.Value()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(bg)
		defer cancel()
		var advances []ConversationView
		ends := 0
		observation, err := h.ObserveConversation(ctx, c.ID(), ViewObserver{
			Advance: func(_ context.Context, value ConversationView, _ []Operation) error {
				advances = append(advances, value)
				return nil
			},
			End: func(WatchEnd) error { ends++; return nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		if observation.Value.Docs != baseline.Docs {
			t.Fatal("synchronous observation does not share the watch mount")
		}
		cancel()
		source525Notes(t, h.session, c.ID(), 1)
		if len(advances) != 1 || ends != 0 {
			t.Fatal("synchronous delivery or acquisition-only context mismatch", len(advances), ends)
		}
		if observation.Value.Entries.Len() != 0 || advances[0].Entries.Len() != 1 {
			t.Fatal("observation must retain its initial revision")
		}
		observation.Detach()
		observation.Detach()
		source525Notes(t, h.session, c.ID(), 1)
		if len(advances) != 1 || ends != 0 {
			t.Fatal("detach delivered a later advance/end")
		}
	})
}

func TestMainSharedViewSynchronousObserverFailureAndClose(t *testing.T) {
	for _, mode := range []string{"failure", "close"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				h, c := transcriptFixture(t, b)
				failure := errors.New("observer failed")
				var ends []WatchEnd
				calls := 0
				peerCalls := 0
				observer, err := h.ObserveConversation(bg, c.ID(), ViewObserver{
					Advance: func(context.Context, ConversationView, []Operation) error {
						calls++
						if mode == "failure" {
							return failure
						}
						return nil
					},
					End: func(end WatchEnd) error { ends = append(ends, end); return nil },
				})
				if err != nil {
					t.Fatal(err)
				}
				defer observer.Detach()
				peer, err := h.ObserveConversation(bg, c.ID(), ViewObserver{Advance: func(context.Context, ConversationView, []Operation) error { peerCalls++; return nil }, End: func(WatchEnd) error { return nil }})
				if err != nil {
					t.Fatal(err)
				}
				defer peer.Detach()
				source525Notes(t, h.session, c.ID(), 1)
				if calls != 1 || peerCalls != 1 {
					t.Fatal("observer callbacks ran after commit delivery", calls, peerCalls)
				}
				if mode == "failure" {
					if len(ends) != 1 || ends[0].Reason != "listener_error" || ends[0].Error != failure {
						t.Fatal("listener failure ending", ends)
					}
					source525Notes(t, h.session, c.ID(), 1)
					if calls != 1 || peerCalls != 2 {
						t.Fatal("failing observer affected its peer", calls, peerCalls)
					}
				} else {
					if err := h.Close(bg); err != nil {
						t.Fatal(err)
					}
					if len(ends) != 1 || ends[0].Reason != "failed" || ends[0].Error == nil || ends[0].Error.Error() != "session closed" {
						t.Fatal("close ending", ends)
					}
				}
			})
		})
	}
}

func TestMainSharedViewSynchronousMembershipOrder(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		var peer *ViewObservation
		var calls []string
		first, err := h.ObserveConversation(bg, c.ID(), ViewObserver{Advance: func(context.Context, ConversationView, []Operation) error {
			calls = append(calls, "first")
			peer.Detach()
			return nil
		}, End: func(WatchEnd) error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
		defer first.Detach()
		peer, err = h.ObserveConversation(bg, c.ID(), ViewObserver{Advance: func(context.Context, ConversationView, []Operation) error { calls = append(calls, "peer"); return nil }, End: func(WatchEnd) error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
		defer peer.Detach()
		source525Notes(t, h.session, c.ID(), 1)
		if !reflect.DeepEqual(calls, []string{"first"}) {
			t.Fatal("source Set registration/detach order", calls)
		}
	})
}

// Literal source525 observer/watch inputs: six note records (data=index),
// min2/max3 observation and min5 watch, followed by one note.
func TestMainSharedViewSource525SynchronousWindow(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		written := upstreamViewNotes(t, h, 6)
		var changes []ConversationView
		observation, err := h.ObserveConversation(bg, c.ID(), ViewObserver{Advance: func(_ context.Context, value ConversationView, _ []Operation) error {
			changes = append(changes, value)
			return nil
		}, End: func(WatchEnd) error { return nil }}, transcriptBounds(2, 3))
		if err != nil {
			t.Fatal(err)
		}
		defer observation.Detach()
		wide, err := c.Watch(bg, ConversationWindow{Min: source525Int(5)})
		if err != nil {
			t.Fatal(err)
		}
		defer wide.Stop()
		initial, err := wide.Value()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(readonlyEntryIDs(t, observation.Value), written[4:]) || !reflect.DeepEqual(readonlyEntryIDs(t, initial), written[1:]) {
			t.Fatal("source525 initial windows")
		}
		frames := make(chan ConversationView, 1)
		if err := wide.Start(func(_ context.Context, value ConversationView, _ []Operation) error { frames <- value; return nil }); err != nil {
			t.Fatal(err)
		}
		next := upstreamViewNotes(t, h, 1)
		frame := watchReceive(t, frames)
		if len(changes) != 1 || !reflect.DeepEqual(readonlyEntryIDs(t, changes[0]), append(append([]ID{}, written[4:]...), next...)) || !reflect.DeepEqual(readonlyEntryIDs(t, frame), append(append([]ID{}, written[1:]...), next...)) {
			t.Fatal("source525 independent append windows")
		}
		if changes[0].Docs != frame.Docs {
			t.Fatal("source525 observation and watch do not share one mount")
		}
	})
}
func readonlyEntryIDs(t *testing.T, value ConversationView) []ID {
	t.Helper()
	ids := make([]ID, value.Entries.Len())
	for i := range ids {
		entry, _ := value.Entries.Index(i)
		id, err := immutableViewMember(t, entry, "id").Number().Int64()
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = ID(id)
	}
	return ids
}

func TestMainSharedViewSynchronousMemoryAdoption(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		f, c, live := mainMemoryConversation(t, b)
		calls := 0
		var value ConversationView
		observation, err := c.h.ObserveConversation(bg, c.ID(), ViewObserver{Advance: func(_ context.Context, next ConversationView, _ []Operation) error { calls++; value = next; return nil }, End: func(WatchEnd) error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
		defer observation.Detach()
		before, err := b.store.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		mainMemoryLive(t, f, live, []Operation{{"s", []any{"text"}, "memory"}})
		if calls != 1 || immutableViewMember(t, immutableViewMember(t, value.Docs, "pi.live"), "text").String() != "memory" {
			t.Fatal("synchronous memory adoption missing", calls)
		}
		after, err := b.store.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if after.Seq != before.Seq {
			t.Fatal("memory-only publication already stored")
		}
		if err := f.session.UnloadDocuments(bg); err != nil {
			t.Fatal(err)
		}
		if calls != 1 {
			t.Fatal("held storage settlement duplicated observation", calls)
		}
	})
}

func TestMainSharedViewSynchronousMountAndCloseOrder(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		other, err := h.CreateConversation(bg, AgentChange{})
		if err != nil {
			t.Fatal(err)
		}
		var calls, ends []string
		attach := func(id ID, label string) *ViewObservation {
			observation, err := h.ObserveConversation(bg, id, ViewObserver{Advance: func(context.Context, ConversationView, []Operation) error { calls = append(calls, label); return nil }, End: func(WatchEnd) error { ends = append(ends, label); return nil }})
			if err != nil {
				t.Fatal(err)
			}
			return observation
		}
		first := attach(c.ID(), "removed")
		peer := attach(other.ID(), "other")
		defer peer.Detach()
		retained := attach(c.ID(), "retained")
		defer retained.Detach()
		first.Detach()
		checkpointCommit(t, h.session, func(tx *Tx) error {
			for _, conversation := range []ID{c.ID(), other.ID()} {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				if err := tx.AppendEntry(Entry{ID: id, Conversation: conversation, Kind: "note", Value: JSON{}}); err != nil {
					return err
				}
			}
			return nil
		})
		if !reflect.DeepEqual(calls, []string{"retained", "other"}) {
			t.Fatal("mount Set order changed with first observer detachment", calls)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(ends, []string{"retained", "other"}) {
			t.Fatal("close did not preserve mount/observer Set order", ends)
		}
		h.session.observerMu.Lock()
		mounts := len(h.session.conversationMounts)
		h.session.observerMu.Unlock()
		if mounts != 0 {
			t.Fatal("last observer end retained mount owners", mounts)
		}
	})
}

func TestMainSharedViewSynchronousReportsAndContainsEndFault(t *testing.T) {
	store, _ := newTestSQLite(t)
	advanceFailure := errors.New("advance fault")
	endFailure := errors.New("end fault")
	var reports []error
	h := taskTestHarnessOptions(t, store, Options{OnReportWithInfo: func(_ context.Context, err error, _ ReportInfo) { reports = append(reports, err) }})
	c, err := h.Root(bg, AgentChange{})
	if err != nil {
		t.Fatal(err)
	}
	bad, err := h.ObserveConversation(bg, c.ID(), ViewObserver{Advance: func(context.Context, ConversationView, []Operation) error { return advanceFailure }, End: func(WatchEnd) error { return endFailure }})
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Detach()
	calls := 0
	peer, err := h.ObserveConversation(bg, c.ID(), ViewObserver{Advance: func(context.Context, ConversationView, []Operation) error { calls++; return nil }, End: func(WatchEnd) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Detach()
	source525Notes(t, h.session, c.ID(), 1)
	if calls != 1 || !reflect.DeepEqual(reports, []error{advanceFailure, endFailure}) {
		t.Fatal("observer/End faults were not isolated and reported", calls, reports)
	}
}

// Source-derived mount/loaded-tracker contract, independently established by the
// pinned oracle. A mount borrows the loaded initial node, then replays edits on
// its own branch; do not over-share the later changed document root.
func TestMainViewMountedAgentBorrowsLoadedRevisionThenReplaysIndependently(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		if err := c.Configure(bg, AgentChange{}); err != nil {
			t.Fatal(err)
		}
		def, err := DefineDocument(DefinitionOptions{Kind: "pi.agent", Scope: "conversation", Version: 1, History: "rewindable", Fork: "asOf", Initial: func(JSON) (JSON, error) { return JSON{}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return true, nil }})
		if err != nil {
			t.Fatal(err)
		}
		before, found, err := c.SnapshotDefinition(bg, def, nil)
		if err != nil || !found {
			t.Fatal(found, err)
		}
		watch, err := c.Watch(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		initial, err := watch.Value()
		if err != nil {
			t.Fatal(err)
		}
		if immutableViewMember(t, initial.Docs, "pi.agent") != before {
			t.Fatal("mount did not borrow the loaded builtin tracker")
		}
		var mounted ConversationView
		observation, err := h.ObserveConversation(bg, c.ID(), ViewObserver{Advance: func(_ context.Context, value ConversationView, _ []Operation) error { mounted = value; return nil }, End: func(WatchEnd) error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
		defer observation.Detach()
		if observation.Value.Docs != initial.Docs {
			t.Fatal("mounts were not shared")
		}
		level := goai.ModelThinkingLevel("high")
		if err := c.ConfigurePatch(bg, AgentPatch{ThinkingLevel: &level}); err != nil {
			t.Fatal(err)
		}
		after, found, err := c.SnapshotDefinition(bg, def, nil)
		if err != nil || !found {
			t.Fatal(found, err)
		}
		if immutableViewMember(t, after, "model") != immutableViewMember(t, before, "model") {
			t.Fatal("loaded builtin lost unchanged child identity")
		}
		agent := immutableViewMember(t, mounted.Docs, "pi.agent")
		if agent == after || !equalDeltaJSON(agent.Copy(), after.Copy()) {
			t.Fatal("edited mounted branch was not an independent replay")
		}
		if immutableViewMember(t, mounted.Docs, "pi.live") != immutableViewMember(t, initial.Docs, "pi.live") {
			t.Fatal("unchanged mounted builtin lost identity")
		}
	})
}
