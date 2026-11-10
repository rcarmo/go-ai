package durable

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"
)

// Transaction.#appendEntry only requires the conversation; it does not read its
// append key. Storage validates head references against the ordered batches.
func TestSessionV150BlindEntryAppendsBatchAgainstPendingPrefix(t *testing.T) {
	s := gatedSession(t)
	def, err := DefineEntry("blind.append")
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var calls []int
	var mu sync.Mutex
	commit := s.writer.host.commit
	s.writer.host.commit = func(ctx context.Context, batches []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
		mu.Lock()
		calls = append(calls, len(batches))
		first := len(calls) == 1
		mu.Unlock()
		if first {
			close(entered)
			<-release
		}
		return commit(ctx, batches, receive)
	}
	results := make([]chan error, 3)
	var firstID ID
	for i := 0; i < 3; i++ {
		results[i] = make(chan error, 1)
		ran := make(chan struct{})
		i := i
		go func() {
			_, err := s.Commit(bg, func(tx *Tx) error {
				content := EntryContent{Value: JSON{"index": i}}
				if i > 0 {
					content.Head = firstID
				}
				entry, err := tx.AppendTypedEntry(def, 1, content)
				if i == 0 {
					firstID = entry.ID
				}
				close(ran)
				return err
			})
			results[i] <- err
		}()
		if i == 0 {
			<-entered
			<-ran
			continue
		}
		select {
		case <-ran:
		case <-time.After(time.Second):
			unblock()
			for j := 0; j <= i; j++ {
				<-results[j]
			}
			t.Fatal("blind append waited for an unrelated append-key writer")
		}
	}
	queued := make(chan struct{})
	read := make(chan error, 1)
	go func() { _, err := s.Snapshot(&documentGateContext{Context: bg, queued: queued}); read <- err }()
	awaitTaskSignal(t, queued)
	unblock()
	for _, done := range results {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if err := <-read; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]int(nil), calls...)
	mu.Unlock()
	if !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("calls=%v, want [1 2]", got)
	}
	state, err := s.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Entries) != 3 {
		t.Fatalf("entries=%d", len(state.Entries))
	}
	sequences := map[uint64]bool{}
	for _, entry := range state.Entries {
		sequences[entry.Seq] = true
		if entry.Position != 1 || entry.Seq < 2 || entry.Seq > 4 {
			t.Errorf("receipt-time placement %+v", entry)
		}
		if entry.ID != firstID && entry.Head != firstID {
			t.Errorf("head reference lost %+v", entry)
		}
	}
	if len(sequences) != 3 {
		t.Fatalf("duplicate receipt sequences %v", sequences)
	}
}
