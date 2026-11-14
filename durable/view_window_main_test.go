package durable

import (
	"context"
	"fmt"
	"testing"
)

func transcriptBounds(min, max int) ConversationWindow {
	return ConversationWindow{Min: &min, Max: &max}
}
func transcriptIDs(t *testing.T, value ConversationViewCopy, want []ID, before ID) {
	t.Helper()
	if len(value.Entries) != len(want) || value.Before != before {
		t.Fatalf("window: entries=%v before=%d; want IDs=%v before=%d", value.Entries, value.Before, want, before)
	}
	for i, e := range value.Entries {
		if e.ID != want[i] {
			t.Fatalf("entry[%d]=%d want %d", i, e.ID, want[i])
		}
	}
}
func transcriptListener(t *testing.T, w *detachedConversationWatch, limits Limits) <-chan ConversationViewCopy {
	t.Helper()
	initial, err := w.Value()
	if err != nil {
		t.Fatal(err)
	}
	replay, err := dtoObject(initial, limits)
	if err != nil {
		t.Fatal(err)
	}
	frames := make(chan ConversationViewCopy, 16)
	if err = w.Start(func(_ context.Context, v ConversationViewCopy, ops []Operation) error {
		next, err := ApplyOperations(replay, ops, limits)
		if err != nil {
			return err
		}
		wire, err := dtoObject(v, limits)
		if err != nil {
			return err
		}
		if !equalJSONValue(next, wire) {
			return fmt.Errorf("window operations failed replay")
		}
		replay = wire
		frames <- v
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return frames
}

func TestMainTranscriptWindowHydration(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		ids := transcriptNotes(t, h, 10)
		future := ids[9] + 1000
		gap := ids[4] - 1
		tests := []struct {
			name   string
			window ConversationWindow
			want   []ID
			before ID
		}{
			{"newest", transcriptBounds(3, 6), ids[7:], ids[6]},
			{"from-within-max", func() ConversationWindow { w := transcriptBounds(3, 6); w.From = &ids[4]; return w }(), ids[4:], ids[3]},
			{"from-too-old", func() ConversationWindow { w := transcriptBounds(3, 6); w.From = &ids[2]; return w }(), ids[7:], ids[6]},
			{"from-future", func() ConversationWindow { w := transcriptBounds(3, 6); w.From = &future; return w }(), nil, ids[9]},
			{"from-id-gap", func() ConversationWindow { w := transcriptBounds(3, 6); w.From = &gap; return w }(), ids[4:], ids[3]},
			{"equal-min-max", transcriptBounds(3, 3), ids[7:], ids[6]},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				value, err := detachedView(c, bg, test.window)
				if err != nil {
					t.Fatal(err)
				}
				transcriptIDs(t, value, test.want, test.before)
				watch, err := detachedWatch(c, bg, test.window)
				if err != nil {
					t.Fatal(err)
				}
				defer watch.Stop()
				value, err = watch.Value()
				if err != nil {
					t.Fatal(err)
				}
				transcriptIDs(t, value, test.want, test.before)
				if value.Entries == nil {
					t.Fatal("empty entries must remain an array")
				}
			})
		}
	})
}

func TestMainTranscriptWindowShortBeginningAndDefaults(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		ids := transcriptNotes(t, h, 2)
		for _, from := range []ID{0, 1, ids[0]} {
			window := transcriptBounds(3, 6)
			window.From = &from
			value, err := detachedView(c, bg, window)
			if err != nil {
				t.Fatal(err)
			}
			transcriptIDs(t, value, ids, 0)
			wire, err := dtoObject(value, h.session.limits)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := wire["before"]; ok {
				t.Fatal("before must be omitted at transcript start")
			}
		}
		min := 3
		watch, err := detachedWatch(c, bg, ConversationWindow{Min: &min})
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		frames := transcriptListener(t, watch, h.session.limits)
		ids = append(ids, transcriptNotes(t, h, 4)...)
		transcriptIDs(t, transcriptFrame(t, frames), ids, 0) // default max=2*min
		ids = append(ids, transcriptNotes(t, h, 1)...)
		transcriptIDs(t, transcriptFrame(t, frames), ids[4:], ids[3])
	})
}

func TestMainTranscriptIndependentWindowGrowthAndTrim(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		ids := transcriptNotes(t, h, 10)
		small, err := detachedWatch(c, bg, transcriptBounds(2, 3))
		if err != nil {
			t.Fatal(err)
		}
		defer small.Stop()
		large, err := detachedWatch(c, bg, transcriptBounds(3, 6))
		if err != nil {
			t.Fatal(err)
		}
		defer large.Stop()
		a := transcriptListener(t, small, h.session.limits)
		z := transcriptListener(t, large, h.session.limits)
		ids = append(ids, transcriptNotes(t, h, 1)...)
		transcriptIDs(t, transcriptFrame(t, a), ids[8:], ids[7])
		transcriptIDs(t, transcriptFrame(t, z), ids[7:], ids[6])
		ids = append(ids, transcriptNotes(t, h, 2)...)
		transcriptIDs(t, transcriptFrame(t, a), ids[11:], ids[10])
		transcriptIDs(t, transcriptFrame(t, z), ids[7:], ids[6])
		ids = append(ids, transcriptNotes(t, h, 3)...)
		transcriptIDs(t, transcriptFrame(t, a), ids[14:], ids[13])
		transcriptIDs(t, transcriptFrame(t, z), ids[13:], ids[12])
	})
}

func TestMainTranscriptFromOnlyConstrainsAttachment(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		ids := transcriptNotes(t, h, 5)
		from := ids[4] + 1000
		min, max := 2, 3
		watch, err := detachedWatch(c, bg, ConversationWindow{From: &from, Min: &min, Max: &max})
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		initial, err := watch.Value()
		if err != nil {
			t.Fatal(err)
		}
		transcriptIDs(t, initial, nil, ids[4])
		// Subsequent caller mutation cannot change the acquired watch's bounds.
		from, min, max = 0, 100, 200
		frames := transcriptListener(t, watch, h.session.limits)
		added := transcriptNotes(t, h, 2)
		value := transcriptFrame(t, frames)
		transcriptIDs(t, value, added, ids[4])
		value.Entries[0].Value["ordinal"] = "escaped"
		own, err := watch.Value()
		if err != nil || own.Entries[0].Value["ordinal"] == "escaped" {
			t.Fatal("window value escaped", own, err)
		}
		more := transcriptNotes(t, h, 2)
		transcriptIDs(t, transcriptFrame(t, frames), more, added[1])
	})
}

// harness-view.test.ts:512 checks RangeError as well as rejection. Native
// argument rejection uses StorageRejected; the integer cases already exist
// below. Fractional min and NaN from have no int/ID value and are not credited.
func TestMainTranscriptWindowRejectsInvalidOptions(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		_, c := transcriptFixture(t, b)
		tooLarge := int(^uint(0) >> 1)
		invalidID := ID(MaxID + 1)
		zero, negative := 0, -1
		tests := []ConversationWindow{
			{Min: &zero}, {Min: &negative}, {Max: &zero}, transcriptBounds(3, 2),
			{From: &invalidID},
		}
		if uint64(tooLarge) > MaxID {
			tests = append(tests, ConversationWindow{Min: &tooLarge}, ConversationWindow{Max: &tooLarge})
		}
		for i, w := range tests {
			if _, err := detachedView(c, bg, w); err == nil {
				t.Fatalf("View accepted invalid option %d", i)
			} else {
				rejected(t, err)
			}
			if watch, err := detachedWatch(c, bg, w); err == nil {
				watch.Stop()
				t.Fatalf("Watch accepted invalid option %d", i)
			} else {
				rejected(t, err)
			}
		}
		if _, err := detachedView(c, bg, ConversationWindow{}, ConversationWindow{}); err == nil {
			t.Fatal("View ignored extra window")
		}
		if watch, err := detachedWatch(c, bg, ConversationWindow{}, ConversationWindow{}); err == nil {
			watch.Stop()
			t.Fatal("Watch ignored extra window")
		}
		// Source's final assertion accepts equal bounds on the empty view.
		if _, err := detachedView(c, bg, transcriptBounds(3, 3)); err != nil {
			t.Fatal(err)
		}
		// Rejections do not poison ordinary admitted reads/acquisition.
		if _, err := detachedView(c, bg); err != nil {
			t.Fatal(err)
		}
	})
}

func TestMainTranscriptWindowForkHistory(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		ids := transcriptNotes(t, h, 3)
		fork, err := c.Fork(bg, ids[1])
		if err != nil {
			t.Fatal(err)
		}
		watch, err := detachedWatch(fork, bg, transcriptBounds(2, 3))
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		initial, err := watch.Value()
		if err != nil {
			t.Fatal(err)
		}
		transcriptIDs(t, initial, ids[:2], 0)
		frames := transcriptListener(t, watch, h.session.limits)
		transcriptNotes(t, h, 1) // Future parent entries are beyond the fork cutoff.
		var summary ID
		if _, err = h.session.Commit(bg, func(tx *Tx) (any, error) {
			var err error
			summary, err = tx.MintID()
			if err != nil {
				return nil, err
			}
			return nil, tx.AppendEntry(Entry{ID: summary, Conversation: fork.id, Kind: "app.summary", Head: ids[1], Value: JSON{}})
		}); err != nil {
			t.Fatal(err)
		}
		transcriptIDs(t, transcriptFrame(t, frames), []ID{ids[0], ids[1], summary}, 0)
	})
}

// Pending publication-time windows must replay after folding, without the raw
// subscription's 100-value reset. Native byte/operation bounds remain separate.
func TestMainTranscriptWindowPendingReplay(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		future := ID(MaxID)
		window := transcriptBounds(3, 6)
		window.From = &future
		watch, err := detachedWatch(c, bg, window)
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		baseline, err := watch.Value()
		if err != nil {
			t.Fatal(err)
		}
		replay, err := dtoObject(baseline, h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		ids := []ID{}
		for i := 0; i < 105; i++ {
			ids = append(ids, transcriptNotes(t, h, 1)...)
		}
		latest := make(chan ConversationViewCopy, 1)
		replaced := false
		if err = watch.Start(func(_ context.Context, v ConversationViewCopy, ops []Operation) error {
			if len(ops) > 0 && ops[0][0] == "r" {
				replaced = true
			}
			next, err := ApplyOperations(replay, ops, h.session.limits)
			if err != nil {
				return err
			}
			wire, err := dtoObject(v, h.session.limits)
			if err != nil {
				return err
			}
			if !equalJSONValue(next, wire) {
				return fmt.Errorf("overflow cursor failed replay")
			}
			replay = wire
			if len(v.Entries) > 0 && v.Entries[len(v.Entries)-1].ID == ids[len(ids)-1] {
				if replaced {
					return fmt.Errorf("pending window used a count-only root replacement")
				}
				latest <- v
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		value := transcriptFrame(t, latest)
		if len(value.Entries) < 3 || len(value.Entries) > 6 {
			t.Fatalf("unbounded replacement: %d entries", len(value.Entries))
		}
		start := len(ids) - len(value.Entries)
		transcriptIDs(t, value, ids[start:], ids[start-1])
	})
}

// Legacy content/operation assertions explicitly request detached data. Identity
// assertions use ConversationWatch directly, without this conversion.
type detachedConversationWatch struct{ *ConversationWatch }

func detachedWatch(c *ConversationHandle, ctx context.Context, window ...ConversationWindow) (*detachedConversationWatch, error) {
	w, err := c.Watch(ctx, window...)
	if err != nil {
		return nil, err
	}
	return &detachedConversationWatch{w}, nil
}
func detachedView(c *ConversationHandle, ctx context.Context, window ...ConversationWindow) (ConversationViewCopy, error) {
	value, err := c.View(ctx, window...)
	if err != nil {
		return ConversationViewCopy{}, err
	}
	return value.Copy(c.h.session.limits)
}
func (w *detachedConversationWatch) Value() (ConversationViewCopy, error) {
	value, err := w.ConversationWatch.Value()
	if err != nil {
		return ConversationViewCopy{}, err
	}
	return value.Copy(w.limits)
}
func (w *detachedConversationWatch) Start(listener func(context.Context, ConversationViewCopy, []Operation) error) error {
	return w.ConversationWatch.Start(func(ctx context.Context, value ConversationView, ops []Operation) error {
		detached, err := value.Copy(w.limits)
		if err != nil {
			return err
		}
		return listener(ctx, detached, ops)
	})
}
