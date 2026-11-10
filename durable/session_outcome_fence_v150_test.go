package durable

import (
	"context"
	"sync"
	"testing"
	"time"
)

// session.ts #enqueue: only the line part ends at admission. A commit with no
// writes still delivers behind the outcomes admitted before it.
func TestSessionV150ReadOnlyCommitDeliversBehindEarlierWriter(t *testing.T) {
	s := gatedSession(t)
	id, err := s.MintID(bg)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	commit := s.writer.host.commit
	s.writer.host.commit = func(ctx context.Context, batches []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
		close(entered)
		<-release
		return commit(ctx, batches, receive)
	}
	first := make(chan error, 1)
	go func() {
		_, err := s.Commit(bg, func(tx *Tx) error {
			return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "fence", Value: JSON{}})
		})
		first <- err
	}()
	<-entered
	ran := make(chan struct{})
	second := make(chan error, 1)
	go func() { _, err := s.Commit(bg, func(*Tx) error { close(ran); return nil }); second <- err }()
	awaitTaskSignal(t, ran)
	select {
	case err := <-second:
		t.Fatalf("read-only outcome overtook held writer: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	unblock()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
}
