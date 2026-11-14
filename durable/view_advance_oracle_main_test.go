package durable

import (
	"context"
	"testing"
)

type upstreamWindowFrame struct {
	value ConversationViewCopy
	ops   []Operation
}

func recordUpstreamWindow(t *testing.T, c *ConversationHandle, options ...ConversationWindow) (ConversationViewCopy, <-chan upstreamWindowFrame) {
	t.Helper()
	w, err := detachedWatch(c, bg, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Stop() })
	initial, err := w.Value()
	if err != nil {
		t.Fatal(err)
	}
	frames := make(chan upstreamWindowFrame, 16)
	if err = w.Start(func(_ context.Context, v ConversationViewCopy, ops []Operation) error {
		frames <- upstreamWindowFrame{v, ops}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return initial, frames
}

// Await the final revision only AFTER every source commit has run. No callback
// acknowledgement is inserted between commits to manufacture frame counts.
func awaitUpstreamWindow(t *testing.T, ch <-chan upstreamWindowFrame, last ID) []upstreamWindowFrame {
	t.Helper()
	var frames []upstreamWindowFrame
	for {
		f := watchReceive(t, ch)
		frames = append(frames, f)
		if len(f.value.Entries) > 0 && f.value.Entries[len(f.value.Entries)-1].ID == last {
			return frames
		}
	}
}
func replayUpstreamWindow(t *testing.T, s *Session, initial ConversationViewCopy, frames []upstreamWindowFrame) ConversationViewCopy {
	t.Helper()
	before, err := dtoObject(initial, s.limits)
	if err != nil {
		t.Fatal(err)
	}
	var replica any = map[string]any(before)
	var last ConversationViewCopy
	for _, f := range frames {
		replica, err = ApplyOperations(replica, f.ops, s.limits)
		if err != nil {
			t.Fatal(err)
		}
		want, e := dtoObject(f.value, s.limits)
		if e != nil {
			t.Fatal(e)
		}
		if !equalDeltaJSON(replica, map[string]any(want)) {
			t.Fatal("source frame replay")
		}
		last = f.value
	}
	return last
}
func assertUpstreamWindowFresh(t *testing.T, s *Session, c *ConversationHandle, value ConversationViewCopy, min int) {
	t.Helper()
	fresh, err := detachedView(c, bg, ConversationWindow{Min: &min})
	if err != nil {
		t.Fatal(err)
	}
	got, err := dtoObject(value, s.limits)
	if err != nil {
		t.Fatal(err)
	}
	want, err := dtoObject(fresh, s.limits)
	if err != nil {
		t.Fatal(err)
	}
	if !equalDeltaJSON(map[string]any(got), map[string]any(want)) {
		t.Fatal("source committed view comparison")
	}
}

// Exact source note batches/window/value/operation assertions from pinned
// harness-view.test.ts:412-453,482-510. Provider/chat setup and Entry's required
// native empty Value/minted IDs are adaptations. JS shared entry identity is
// not represented by native detached structs.
func TestMainViewUpstreamAdvanceOracles(t *testing.T) {
	for _, mode := range []string{"grow-splice", "burst", "resumed", "from-only-initial"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				h, c := transcriptFixture(t, b)
				switch mode {
				case "grow-splice":
					written := upstreamViewNotes(t, h, 4)
					initial, ch := recordUpstreamWindow(t, c, transcriptBounds(3, 5))
					transcriptIDs(t, initial, written[1:], written[0])
					appended := upstreamViewNotes(t, h, 3)
					frames := awaitUpstreamWindow(t, ch, appended[2])
					if len(frames) != 3 {
						t.Fatalf("source natural frame count: got %d, want 3", len(frames))
					}
					for i, want := range []int{4, 5, 3} {
						if len(frames[i].value.Entries) != want {
							t.Fatal("source frame lengths", frames)
						}
					}
					state, err := h.session.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					// Source expectations use the commit's returned records. Read
					// native committed records independently of delivered values.
					e5, err := conversationEntryObject(state.Entries[appended[0]], h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					e7, err := conversationEntryObject(state.Entries[appended[2]], h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					assertConversationOperations(t, frames[0].ops, []Operation{{"p", []any{"entries"}, 3, 0, []any{map[string]any(e5)}}}, h.session.limits)
					assertConversationOperations(t, frames[2].ops, []Operation{{"p", []any{"entries"}, 5, 0, []any{map[string]any(e7)}}, {"p", []any{"entries"}, 0, 3, []any{}}, {"s", []any{"before"}, written[3]}}, h.session.limits)
					transcriptIDs(t, frames[2].value, appended, written[3])
					assertUpstreamWindowFresh(t, h.session, c, replayUpstreamWindow(t, h.session, initial, frames), 3)
				case "burst":
					upstreamViewNotes(t, h, 2)
					initial, ch := recordUpstreamWindow(t, c, transcriptBounds(2, 3))
					var burst []ID
					checkpointCommit(t, h.session, func(tx *Tx) error {
						for i := 0; i < 5; i++ {
							id, err := tx.mintID()
							if err != nil {
								return err
							}
							if err = tx.appendEntryRecord(Entry{ID: id, Conversation: c.ID(), Kind: "burst", Value: JSON{}}); err != nil {
								return err
							}
							burst = append(burst, id)
						}
						return nil
					})
					frames := awaitUpstreamWindow(t, ch, burst[4])
					if len(frames) != 1 {
						t.Fatal("source burst frame count", len(frames))
					}
					transcriptIDs(t, frames[0].value, burst[3:], burst[2])
					assertUpstreamWindowFresh(t, h.session, c, replayUpstreamWindow(t, h.session, initial, frames), 2)
				case "resumed":
					written := upstreamViewNotes(t, h, 5)
					from := written[4] + 1
					initial, ch := recordUpstreamWindow(t, c, ConversationWindow{From: &from})
					later := upstreamViewNotes(t, h, 2)
					frames := awaitUpstreamWindow(t, ch, later[1])
					transcriptIDs(t, frames[len(frames)-1].value, later, written[4])
					replayUpstreamWindow(t, h.session, initial, frames)
				case "from-only-initial":
					written := upstreamViewNotes(t, h, 3)
					from := written[2] + 1000
					initial, ch := recordUpstreamWindow(t, c, ConversationWindow{From: &from})
					transcriptIDs(t, initial, []ID{}, written[2])
					if initial.Entries == nil {
						t.Fatal("source empty entries must be an array")
					}
					next := upstreamViewNotes(t, h, 1)
					if next[0] >= from {
						t.Fatal("source input must append below from", next[0], from)
					}
					frames := awaitUpstreamWindow(t, ch, next[0])
					transcriptIDs(t, frames[len(frames)-1].value, next, written[2])
					state, err := h.session.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					got, err := conversationEntryObject(frames[len(frames)-1].value.Entries[0], h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					want, err := conversationEntryObject(state.Entries[next[0]], h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					if !equalDeltaJSON(map[string]any(got), map[string]any(want)) {
						t.Fatal("source complete appended entry")
					}
				}
			})
		})
	}
}
