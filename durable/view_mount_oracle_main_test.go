package durable

import (
	"context"
	"testing"
)

func startUpstreamMountRecord(t *testing.T, w *ConversationWatch) <-chan upstreamWindowFrame {
	t.Helper()
	ch := make(chan upstreamWindowFrame, 16)
	if err := w.Start(func(_ context.Context, v ConversationView, ops []Operation) error {
		ch <- upstreamWindowFrame{v, ops}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return ch
}

// Pinned harness-view.test.ts:234-292. Native setup/required entry Value and
// minted IDs are adaptations. Rebuilt-value equality is preserved; the source
// rebuilt !== last reference assertion is unported for detached Go structs.
func TestMainViewUpstreamMountOracles(t *testing.T) {
	for _, mode := range []string{"retire-recreate", "undelivered-101", "independent-remount"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				h, c := transcriptFixture(t, b)
				switch mode {
				case "retire-recreate":
					_, ch := recordUpstreamWindow(t, c)
					checkpointCommit(t, h.session, func(tx *Tx) error {
						live, err := builtin(tx, c.ID(), "pi.live")
						if err != nil {
							return err
						}
						return live.Retire()
					})
					checkpointCommit(t, h.session, func(tx *Tx) error {
						// Source tx.doc lazily creates the absent document. Native
						// builtin only looks up existing documents; create explicitly.
						id, err := tx.MintID()
						if err != nil {
							return err
						}
						live, err := tx.CreateDocument(Document{ID: id, Scope: "conversation", Owner: c.ID(), Kind: "pi.live", Version: 1, Value: JSON{}})
						if err != nil {
							return err
						}
						return live.Update(func(v JSON) error { v["tools"] = []any{}; return nil })
					})
					retired := watchReceive(t, ch)
					recreated := watchReceive(t, ch)
					assertConversationOperations(t, retired.ops, []Operation{{"d", []any{"docs", "pi.live"}}}, h.session.limits)
					assertConversationOperations(t, recreated.ops, []Operation{{"s", []any{"docs", "pi.live"}, JSON{"tools": []any{}}}}, h.session.limits)
					if _, ok := retired.value.Docs["pi.live"]; ok {
						t.Fatal("source retired document remains mounted")
					}
					if len(ch) != 0 {
						t.Fatal("extra source recreation frames")
					}
				case "undelivered-101":
					w, err := c.Watch(bg)
					if err != nil {
						t.Fatal(err)
					}
					defer w.Stop()
					initial, err := w.Value()
					if err != nil {
						t.Fatal(err)
					}
					var last ID
					for i := 0; i < 101; i++ {
						last = appendUpstreamHistory(t, h, c, "note", 0)
					}
					ch := startUpstreamMountRecord(t, w)
					frames := awaitUpstreamWindow(t, ch, last)
					if len(frames) != 1 || len(ch) != 0 {
						t.Fatal("source folded call count", len(frames), len(ch))
					}
					if len(frames[0].ops) > 0 && frames[0].ops[0][0] == "r" {
						t.Fatal("source folded call must not start with root replacement")
					}
					assertUpstreamWindowFresh(t, h.session, c, replayUpstreamWindow(t, h.session, initial, frames), 50)
				case "independent-remount":
					kept, err := c.Watch(bg)
					if err != nil {
						t.Fatal(err)
					}
					defer kept.Stop()
					keptCh := startUpstreamMountRecord(t, kept)
					peer, err := c.Watch(bg)
					if err != nil {
						t.Fatal(err)
					}
					defer peer.Stop()
					peerCh := startUpstreamMountRecord(t, peer)
					one := appendUpstreamHistory(t, h, c, "one", 0)
					keptFrames := awaitUpstreamWindow(t, keptCh, one)
					assertUpstreamKinds(t, keptFrames[len(keptFrames)-1].value, "one")
					// Source drains both watches before detaching the peer.
					peerFrames := awaitUpstreamWindow(t, peerCh, one)
					peer.Stop()
					if len(peerFrames) != 1 {
						t.Fatal("source peer frame count", len(peerFrames))
					}
					two := appendUpstreamHistory(t, h, c, "two", 0)
					keptFrames = awaitUpstreamWindow(t, keptCh, two)
					last := keptFrames[len(keptFrames)-1].value
					assertUpstreamKinds(t, last, "one", "two")
					kept.Stop()
					if len(peerCh) != 0 {
						t.Fatal("stopped peer received another frame")
					}
					fresh, err := c.Watch(bg)
					if err != nil {
						t.Fatal(err)
					}
					rebuilt, err := fresh.Value()
					fresh.Stop()
					if err != nil {
						t.Fatal(err)
					}
					got, err := dtoObject(rebuilt, h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					want, err := dtoObject(last, h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					if !equalDeltaJSON(map[string]any(got), map[string]any(want)) {
						t.Fatal("source rebuilt view differs from last revision")
					}
				}
			})
		})
	}
}
