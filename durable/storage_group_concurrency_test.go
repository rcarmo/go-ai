package durable

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

// A receipt holds the core after the first batch is durable. Memory/journal
// have adopted only that batch; SQLite has adopted all three. Neither a raw
// reader nor a second caller may enter that partially delivered call.
func TestInternalGroupQueuedReadAndWriter(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		batches, ids := groupEntryBatches(t, b.store, 4)
		core := b.store.(nativeStorage).nativeCore()
		ctx, cancel := context.WithTimeout(bg, 5*time.Second)
		held, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		var workers sync.WaitGroup
		defer func() { unblock(); cancel(); workers.Wait() }()
		var events []string // writes are serialised by the core, read after all joins
		first := make(chan error, 1)
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := core.commitBatches(ctx, batches[:3], func(index int, seq uint64) error {
				events = append(events, fmt.Sprintf("first %d %d", index, seq))
				if index == 0 {
					close(held)
					select {
					case <-release:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				return nil
			}, false)
			first <- err
		}()
		select {
		case <-held:
		case <-ctx.Done():
			t.Fatal("first group never reached receipt")
		}
		writeAdmission := &mainCommitAdmissionContext{Context: ctx, entered: make(chan struct{})}
		other := make(chan error, 1)
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := core.commitBatches(writeAdmission, batches[3:], func(index int, seq uint64) error {
				events = append(events, fmt.Sprintf("other %d %d", index, seq))
				return nil
			}, false)
			other <- err
		}()
		readAdmission := &mainCommitAdmissionContext{Context: ctx, entered: make(chan struct{})}
		type reading struct {
			state Snapshot
			err   error
		}
		read := make(chan reading, 1)
		workers.Add(1)
		go func() {
			defer workers.Done()
			state, err := b.store.Snapshot(readAdmission)
			read <- reading{state, err}
		}()
		for _, entered := range []<-chan struct{}{writeAdmission.entered, readAdmission.entered} {
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("queued operation never attempted admission")
			}
		}
		select {
		case result := <-read:
			t.Fatalf("read entered an unfinished group: seq=%d err=%v", result.state.Seq, result.err)
		default:
		}
		select {
		case err := <-other:
			t.Fatalf("writer entered an unfinished group: %v", err)
		default:
		}
		unblock()
		for _, outcome := range []<-chan error{first, other} {
			select {
			case err := <-outcome:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("writer did not settle")
			}
		}
		var result reading
		select {
		case result = <-read:
		case <-ctx.Done():
			t.Fatal("reader did not settle")
		}
		workers.Wait()
		if result.err != nil {
			t.Fatal(result.err)
		}
		// Readers and the second writer need no assumed FIFO ordering. Either
		// seq 3 or 4 is valid, but all three earlier entries must be visible.
		if result.state.Seq != 3 && result.state.Seq != 4 {
			t.Fatal("read observed a partial first group", result.state.Seq)
		}
		for index, id := range ids[:3] {
			if result.state.Entries[id].Seq != uint64(index+1) {
				t.Fatal("read missed an earlier group member", id)
			}
		}
		if !reflect.DeepEqual(events, []string{"first 0 1", "first 1 2", "first 2 3", "other 0 4"}) {
			t.Fatal("receipts interleaved across calls", events)
		}
	})
}
