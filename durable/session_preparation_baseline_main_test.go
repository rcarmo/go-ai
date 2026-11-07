package durable

import (
	"strings"
	"testing"
)

func TestMainPreparedPublicationRejectsStaleBaselineBeforeStorage(t *testing.T) {
	for _, advance := range []string{"published", "stored-unpublished"} {
		t.Run(advance, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				seedRootConversation(t, b.store)
				s, err := OpenSession(b.store)
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close(bg)
				firstID, err := s.MintID(bg)
				if err != nil {
					t.Fatal(err)
				}
				secondID, err := s.MintID(bg)
				if err != nil {
					t.Fatal(err)
				}
				appends := 0
				appendFrame := s.core.appendFrame
				s.core.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
					if kind == 2 {
						appends++
					}
					if appendFrame != nil {
						return appendFrame(kind, ordinal, high, payload)
					}
					return nil
				}
				ready := make(chan struct{})
				defer close(ready)
				s.taskBookkeeping(func() {
					first := mainPrepareOnlyPublication(t, s, bg, firstID, ready)
					second := mainPrepareOnlyPublication(t, s, bg, secondID, ready)
					receipt, candidate, err := s.settlePreparedPublication(&first)
					if err != nil {
						t.Fatal(err)
					}
					if advance == "published" {
						if _, err := s.publishSessionPublication(&first, receipt, &candidate); err != nil {
							t.Fatal(err)
						}
					}
					rejected, image, err := s.settlePreparedPublication(&second)
					if err == nil || !strings.Contains(err.Error(), "stale prepared") {
						t.Fatal("stale preparation entered writer", err)
					}
					if rejected.seq != 0 || image.Seq != 0 || appends != 1 || s.core.state.Seq != 2 || len(s.core.state.Entries) != 1 {
						t.Fatal("stale preparation changed storage", rejected.seq, image.Seq, appends, s.core.state.Seq)
					}
					if _, ok := s.core.state.Entries[secondID]; ok {
						t.Fatal("stale write became durable")
					}
				})
				if err := s.Close(bg); err != nil {
					t.Fatal(err)
				}
				reopened := utf16ReopenStore(t, b.store)
				defer reopened.Close(bg)
				state, err := reopened.Snapshot(bg)
				if err != nil || state.Seq != 2 || len(state.Entries) != 1 {
					t.Fatal("stale write recovered", state, err)
				}
			})
		})
	}
}
