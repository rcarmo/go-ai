package durable

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

// Source session-observers.test.ts:241,254: a queued commit belongs to the
// atomic baseline; cancellation during queued attachment leaves no observer.
func TestMainPublicDocumentObserverQueuedAttachment(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		name := "baseline"
		if cancelled {
			name = "cancelled"
		}
		t.Run(name, func(t *testing.T) {
			s, _, def, owner := sourceDocumentObserverSession(t, false)
			if err := sourceDocumentSet(s, def, owner, 1, false); err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			commitDone := make(chan error, 1)
			attachDone := make(chan struct {
				observation *DocumentObservation
				found       bool
				err         error
			}, 1)
			t.Cleanup(func() { unblock() })
			go func() {
				_, err := s.Commit(bg, func(tx *Tx) error {
					doc, err := tx.AcquireDocument(def, owner, nil, nil)
					if err != nil {
						return err
					}
					if err = doc.ApplyOperations([]Operation{{"s", []any{"count"}, 11}}); err != nil {
						return err
					}
					close(entered)
					<-release
					return nil
				})
				commitDone <- err
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				unblock()
				<-commitDone
				t.Fatal("commit did not enter")
			}
			base, cancel := context.WithCancel(bg)
			defer cancel()
			admission := &mainCommitAdmissionContext{Context: base, entered: make(chan struct{})}
			calls := make(chan struct{}, 1)
			go func() {
				obs, found, err := s.ObserveDefinition(admission, def, owner, nil, DocumentObserver{Advance: func(context.Context, DocumentCall) error { calls <- struct{}{}; return nil }, End: func(WatchEnd) error { return nil }})
				attachDone <- struct {
					observation *DocumentObservation
					found       bool
					err         error
				}{obs, found, err}
			}()
			select {
			case <-admission.entered:
			case <-time.After(3 * time.Second):
				cancel()
				unblock()
				<-commitDone
				<-attachDone
				t.Fatal("attachment did not enter")
			}
			if cancelled {
				cancel()
			}
			unblock()
			if err := <-commitDone; err != nil {
				t.Fatal(err)
			}
			got := <-attachDone
			if cancelled {
				if got.observation != nil || got.found || !errors.Is(got.err, context.Canceled) {
					t.Fatalf("cancelled: %+v", got)
				}
			} else {
				if got.err != nil || !got.found || got.observation == nil || immutableViewMember(t, got.observation.Value, "count").Number() != json.Number("11") {
					t.Fatalf("baseline: %+v", got)
				}
				got.observation.Detach()
			}
			if len(calls) != 0 {
				t.Fatal("queued commit delivered after baseline")
			}
			s.observerMu.Lock()
			n := len(s.documentObservers)
			s.observerMu.Unlock()
			if n != 0 {
				t.Fatalf("retained observers=%d", n)
			}
		})
	}
}
