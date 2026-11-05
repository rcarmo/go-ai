package durable

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Pinned f9 session-group-commit.test.ts:256. These are Session-line reads,
// not direct raw Storage calls (which have no Session job-order guarantee).
func TestV150SessionReadLinePendingConversation(t *testing.T) {
	for _, name := range []string{"latestEntryId", "latestHeadMarker", "tx.scanEntries", "line.scanEntries", "line.findLatestHeadMarker"} {
		t.Run(name, func(t *testing.T) {
			session, _, held, ctx := openGatesSession(t)
			held.holdCommits()
			created := make(chan ID, 1)
			settled := make(chan error, 1)
			runGatesJob(t, session, ctx, func(tx *Tx) error {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				if err := tx.CreateConversation(Conversation{ID: id}); err != nil {
					return err
				}
				created <- id
				return nil
			}, settled)
			var id ID
			select {
			case id = <-created:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case <-held.entered:
			case <-ctx.Done():
				t.Fatal("creation never reached storage")
			}
			reading := make(chan error, 1)
			started := make(chan struct{})
			go func() {
				close(started)
				var err error
				switch name {
				case "latestEntryId":
					_, err = session.Commit(ctx, func(tx *Tx) error {
						value, found, e := tx.LatestEntryID(id)
						if e == nil && (found || value != 0) {
							return errors.New("unexpected latest entry")
						}
						return e
					})
				case "latestHeadMarker":
					_, err = session.Commit(ctx, func(tx *Tx) error {
						_, found, e := tx.LatestHeadMarker(id, 0)
						if e == nil && found {
							return errors.New("unexpected head marker")
						}
						return e
					})
				case "tx.scanEntries":
					_, err = session.Commit(ctx, func(tx *Tx) error {
						page, e := tx.ScanEntries(EntryQuery{Conversation: id}, 1, "")
						if e == nil && len(page.Items) != 0 {
							return errors.New("unexpected entries")
						}
						return e
					})
				case "line.scanEntries":
					err = session.ReadOnLine(ctx, func(line *ReadLine) error {
						page, e := line.ScanEntries(EntryQuery{Conversation: id}, 1, "")
						if e == nil && len(page.Items) != 0 {
							return errors.New("unexpected entries")
						}
						return e
					})
				case "line.findLatestHeadMarker":
					err = session.ReadOnLine(ctx, func(line *ReadLine) error {
						_, found, e := line.FindLatestHeadMarker(id, 0)
						if e == nil && found {
							return errors.New("unexpected marker")
						}
						return e
					})
				}
				reading <- err
			}()
			<-started
			select {
			case err := <-reading:
				t.Fatalf("read settled before publication: %v", err)
			case <-time.After(30 * time.Millisecond):
			}
			held.releaseHold()
			if err := settleWithinGate(t, settled, 3); err != nil {
				t.Fatal(err)
			}
			if err := settleWithinGate(t, reading, 3); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestV150SessionReadLineLifetimeAndCancellation(t *testing.T) {
	session, _, _, ctx := openGatesSession(t)
	var escaped *ReadLine
	if err := session.ReadOnLine(ctx, func(line *ReadLine) error { escaped = line; _, _, err := line.FindLatestHeadMarker(1, 0); return err }); err != nil {
		t.Fatal(err)
	}
	if _, _, err := escaped.FindLatestHeadMarker(1, 0); !errors.Is(err, ErrSealed) {
		t.Fatalf("escaped read=%v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	called := false
	if err := session.ReadOnLine(canceled, func(*ReadLine) error { called = true; return nil }); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("canceled read called=%v error=%v", called, err)
	}
}
