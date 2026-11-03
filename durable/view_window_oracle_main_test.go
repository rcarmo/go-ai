package durable

import "testing"

// Same one-entry-per-commit notes fixture as harness-view.test.ts:382-388.
func upstreamViewNotes(t *testing.T, h *Harness, count int) []ID {
	t.Helper()
	out := make([]ID, 0, count)
	for index := 0; index < count; index++ {
		var id ID
		_, err := h.session.Commit(bg, func(tx *Tx) error {
			var err error
			id, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{}, HasData: true, Data: index})
		})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	return out
}

// Port the inputs and assertions at harness-view.test.ts:398-411,455-480
// (81f3cd41). Additional backends exercise the same oracle, without replacing it.
func TestMainViewUpstreamHydrationOracles(t *testing.T) {
	for _, count := range []int{10, 30} {
		name := "newest-min-and-default"
		if count == 30 {
			name = "from-exact-short-future-overflow-start"
		}
		t.Run(name, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				h, c := transcriptFixture(t, b)
				written := upstreamViewNotes(t, h, count)
				fresh := func(options ...ConversationWindow) ConversationView {
					watch, err := c.Watch(bg, options...)
					if err != nil {
						t.Fatal(err)
					}
					defer watch.Stop()
					value, err := watch.Value()
					if err != nil {
						t.Fatal(err)
					}
					return value
				}
				assertNoBefore := func(value ConversationView) {
					body, err := dtoObject(value, h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					if _, present := body["before"]; present {
						t.Fatal("upstream requires before to be absent at transcript start")
					}
				}
				if count == 10 {
					min := 4
					transcriptIDs(t, fresh(ConversationWindow{Min: &min}), written[6:], written[5])
					all := fresh()
					transcriptIDs(t, all, written, 0)
					assertNoBefore(all)
					return
				}
				window := transcriptBounds(5, 10)
				for _, example := range []struct {
					name   string
					from   ID
					want   []ID
					before ID
				}{
					{"exact-max", written[20], written[20:], written[19]},
					{"fewer-than-min", written[28], written[28:], written[27]},
					{"after-newest", written[29] + 1, []ID{}, written[29]},
					{"more-than-max", written[19], written[25:], written[24]},
				} {
					t.Run(example.name, func(t *testing.T) {
						bounds := window
						bounds.From = &example.from
						transcriptIDs(t, fresh(bounds), example.want, example.before)
					})
				}
				min := 40
				whole := fresh(ConversationWindow{Min: &min, From: &written[0]})
				transcriptIDs(t, whole, written, 0)
				assertNoBefore(whole)
			})
		})
	}
}
