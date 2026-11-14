package durable

import (
	"reflect"
	"testing"
)

func appendUpstreamHistory(t *testing.T, h *Harness, c *ConversationHandle, kind string, head ID) ID {
	t.Helper()
	var id ID
	checkpointCommit(t, h.session, func(tx *Tx) error {
		var err error
		id, err = tx.MintID()
		if err != nil {
			return err
		}
		return tx.appendEntryRecord(Entry{ID: id, Conversation: c.ID(), Kind: kind, Head: head, Value: JSON{}})
	})
	return id
}

func assertUpstreamKinds(t *testing.T, value ConversationViewCopy, want ...string) {
	t.Helper()
	got := make([]string, len(value.Entries))
	for i, e := range value.Entries {
		got[i] = e.Kind
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("source entry kinds: got %v, want %v", got, want)
	}
}

// Pinned harness-view.test.ts:178-233. Preserve source kinds, commit order,
// frame arrays and assertions without inter-commit delivery barriers. Native
// minted IDs/required Value, flat Parent/ParentAt and built-in setup are API
// adaptations; this does not assert JavaScript object identity or microtasks.
func TestMainViewUpstreamHistoryOracles(t *testing.T) {
	for _, mode := range []string{"summary-reset", "fork-markers", "fork-isolation"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				h, c := transcriptFixture(t, b)
				switch mode {
				case "summary-reset":
					appendUpstreamHistory(t, h, c, "a", 0)
					bID := appendUpstreamHistory(t, h, c, "b", 0)
					appendUpstreamHistory(t, h, c, "c", 0)
					initial, ch := recordUpstreamWindow(t, c)
					summary := appendUpstreamHistory(t, h, c, "summary", bID)
					appendUpstreamHistory(t, h, c, "d", 0)
					if err := c.Reset(bg, ""); err != nil {
						t.Fatal(err)
					}
					// Observe the committed final ID, not callback scheduling, after all writes.
					fresh, err := detachedView(c, bg)
					if err != nil {
						t.Fatal(err)
					}
					if len(fresh.Entries) != 6 {
						t.Fatal("source committed length", len(fresh.Entries))
					}
					frames := awaitUpstreamWindow(t, ch, fresh.Entries[5].ID)
					if len(frames) != 3 {
						t.Fatalf("source natural frame count: got %d, want 3", len(frames))
					}
					assertUpstreamKinds(t, frames[0].value, "a", "b", "c", "summary")
					assertUpstreamKinds(t, frames[1].value, "a", "b", "c", "summary", "d")
					assertUpstreamKinds(t, frames[2].value, "a", "b", "c", "summary", "d", "pi.reset")
					state, err := h.session.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					record, err := conversationEntryObject(state.Entries[summary], h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					assertConversationOperations(t, frames[0].ops, []Operation{{"p", []any{"entries"}, 3, 0, []any{map[string]any(record)}}}, h.session.limits)
					if frames[0].value.Entries[3].Head != bID {
						t.Fatal("source summary head")
					}
					assertUpstreamWindowFresh(t, h.session, c, replayUpstreamWindow(t, h.session, initial, frames), 6)
				case "fork-markers":
					a := appendUpstreamHistory(t, h, c, "a", 0)
					bID := appendUpstreamHistory(t, h, c, "b", 0)
					fork, err := c.Fork(bg, bID)
					if err != nil {
						t.Fatal(err)
					}
					initial, ch := recordUpstreamWindow(t, fork)
					summary := appendUpstreamHistory(t, h, fork, "summary", bID)
					frames := awaitUpstreamWindow(t, ch, summary)
					transcriptIDs(t, initial, []ID{a, bID}, 0)
					assertUpstreamKinds(t, frames[0].value, "a", "b", "summary")
					assertUpstreamWindowFresh(t, h.session, fork, frames[0].value, 3)
				case "fork-isolation":
					first := appendUpstreamHistory(t, h, c, "first", 0)
					fork, err := c.Fork(bg, first)
					if err != nil {
						t.Fatal(err)
					}
					initial, ch := recordUpstreamWindow(t, fork)
					assertUpstreamKinds(t, initial, "first")
					if initial.Conversation.Parent != c.ID() || initial.Conversation.ParentAt != first {
						t.Fatal("source fork parent", initial.Conversation)
					}
					appendUpstreamHistory(t, h, c, "parent", 0)
					child := appendUpstreamHistory(t, h, fork, "child", 0)
					frames := awaitUpstreamWindow(t, ch, child)
					if len(frames) != 1 {
						t.Fatalf("source fork frame array: got %d, want 1", len(frames))
					}
					assertUpstreamKinds(t, frames[0].value, "first", "child")
				}
			})
		})
	}
}
