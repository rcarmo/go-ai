package durable

import (
	"context"
	"errors"
	"testing"
)

// Native invariants of the new guard: spent handles release real callback,
// reporter and Session fields, not weak references to a proxy payload box.
func TestMainSyncCommitObserverReleaseAndSharedBounds(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s, add := mainSyncCounterSession(t, b)
		if err := add(); err != nil {
			t.Fatal(err)
		}
		observer := sessionCommitObserver{advance: func(context.Context, sessionPublication) error { return nil }, end: func(WatchEnd) error { return nil }, report: func(context.Context, error, ReportInfo) {}}
		var handles []*sessionCommitObservation
		for i := 0; i < s.observerCapacity(); i++ {
			handle, err := s.observeStoredCommits(observer)
			if err != nil {
				t.Fatal(err)
			}
			handles = append(handles, handle)
		}
		if _, err := s.observeStoredCommits(observer); err == nil {
			t.Fatal("sync process limit missing")
		}
		if _, _, err := s.SubscribeCommits(bg); err == nil {
			t.Fatal("legacy registration ignored sync capacity")
		}
		handles[0].detach()
		handles[0].detach()
		mainSyncAssertReleased(t, handles[0])
		_, legacy, err := s.SubscribeCommits(bg)
		if err != nil {
			t.Fatal("released capacity not reusable", err)
		}
		if _, err := s.observeStoredCommits(observer); err == nil {
			t.Fatal("sync registration ignored legacy capacity")
		}
		legacy.Stop()
		fault, err := s.observeStoredCommits(sessionCommitObserver{advance: func(context.Context, sessionPublication) error { return errors.New("guarded error") }, end: observer.end, report: observer.report})
		if err != nil {
			t.Fatal(err)
		}
		if err := add(); err != nil {
			t.Fatal(err)
		}
		mainSyncAssertReleased(t, fault)
		if err := s.Close(bg); err != nil {
			t.Fatal(err)
		}
		for _, handle := range handles {
			mainSyncAssertReleased(t, handle)
		}
		s.observerMu.Lock()
		defer s.observerMu.Unlock()
		if s.syncCommitObservers != nil {
			t.Fatal("ended source retained guard list")
		}
	})
}
func mainSyncAssertReleased(t *testing.T, handle *sessionCommitObservation) {
	t.Helper()
	handle.mu.Lock()
	defer handle.mu.Unlock()
	if handle.source != nil || handle.observer.advance != nil || handle.observer.end != nil || handle.observer.report != nil {
		t.Fatal("spent observation retains source/callback/report")
	}
}
