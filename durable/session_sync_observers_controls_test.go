package durable

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

// Derived from c9 guardObserver's copied listener iteration and spent guards.
// detach is an Observation operation, not forbidden Session reentry.
func TestMainSyncCommitObserverOrderAndDetachLater(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s, add := mainSyncCounterSession(t, b)
		var mu sync.Mutex
		var calls []string
		record := func(call string) { mu.Lock(); calls = append(calls, call); mu.Unlock() }
		var later *sessionCommitObservation
		for _, name := range []string{"first", "later", "last"} {
			observation, err := s.observeStoredCommits(sessionCommitObserver{
				advance: func(context.Context, CommitPublication) error {
					record(name)
					if name == "first" {
						later.detach()
					}
					return nil
				},
				end: func(WatchEnd) error { record(name + "-end"); return nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			if name == "later" {
				later = observation
			}
		}
		if err := add(); err != nil {
			t.Fatal(err)
		}
		if err := add(); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		if !reflect.DeepEqual(calls, []string{"first", "last", "first", "last"}) {
			t.Error("synchronous ordered publication/detach", calls)
		}
		mu.Unlock()
		if err := s.Close(bg); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if !reflect.DeepEqual(calls, []string{"first", "last", "first", "last", "first-end", "last-end"}) {
			t.Fatal("ordered ending/detach", calls)
		}
	})
}

// Source guard release/report order plus native containment of a broken report
// hook. Returned Go errors adapt throwing callbacks, NOT rejected JS thenables.
func TestMainSyncCommitObserverFaultContainment(t *testing.T) {
	for _, mode := range []string{"advance-self-detach", "report-self-detach", "report-end-panic", "close-end-panic"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				s, add := mainSyncCounterSession(t, b)
				bug, endBug := errors.New("advance bug"), errors.New("end bug")
				var mu sync.Mutex
				var calls []string
				var reports []error
				record := func(call string) { mu.Lock(); calls = append(calls, call); mu.Unlock() }
				var observation *sessionCommitObservation
				observer := sessionCommitObserver{
					advance: func(context.Context, CommitPublication) error {
						record("advance")
						if mode == "close-end-panic" {
							return nil
						}
						if mode == "advance-self-detach" {
							observation.detach()
						}
						return bug
					},
					end: func(WatchEnd) error { record("end"); panic(endBug) },
					report: func(_ context.Context, err error, _ ReportInfo) {
						mu.Lock()
						calls = append(calls, "report")
						reports = append(reports, err)
						mu.Unlock()
						if mode == "report-self-detach" {
							observation.detach()
						}
						if mode == "report-end-panic" || mode == "close-end-panic" {
							panic(errors.New("report bug"))
						}
					},
				}
				var err error
				observation, err = s.observeStoredCommits(observer)
				if err != nil {
					t.Fatal(err)
				}
				healthy := newMainSyncRecorder()
				if _, err = s.observeStoredCommits(healthy.observer()); err != nil {
					t.Fatal(err)
				}
				if err = add(); err != nil {
					t.Fatal(err)
				}
				if err = add(); err != nil {
					t.Fatal(err)
				}
				// Source flush equivalent for the baseline's asynchronous adapter.
				watchReceive(t, healthy.advanced)
				watchReceive(t, healthy.advanced)
				if err = s.Close(bg); err != nil {
					t.Fatal(err)
				}
				mu.Lock()
				defer mu.Unlock()
				var wantCalls []string
				var wantReports []error
				switch mode {
				case "advance-self-detach", "report-self-detach":
					wantCalls = []string{"advance", "report"}
					wantReports = []error{bug}
				case "report-end-panic":
					wantCalls = []string{"advance", "report", "end", "report"}
					wantReports = []error{bug, endBug}
				case "close-end-panic":
					wantCalls = []string{"advance", "advance", "end", "report"}
					wantReports = []error{endBug}
				}
				if !reflect.DeepEqual(calls, wantCalls) || !reflect.DeepEqual(reports, wantReports) {
					t.Error("contained callback order/original causes", calls, reports)
				}
				if s.failure() != nil {
					t.Fatal("guard failed Session", s.failure())
				}
			})
		})
	}
}

// The new lane must also end on a Close-held storage refusal. Reuse fixture
// setup, without rerunning the completed lifecycle/deadline/recovery gates.
func TestMainSyncCommitObserverCloseRefusalEnd(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s, doc, _ := openMainDeferredSession(t, b)
		if _, err := s.commitDeferred(bg, mainDeferredAppend(doc, "held")); err != nil {
			t.Fatal(err)
		}
		r := newMainSyncRecorder()
		if _, err := s.observeStoredCommits(r.observer()); err != nil {
			t.Fatal(err)
		}
		refusal := &StorageRejected{Reason: "close flush refused"}
		s.taskBookkeeping(func() {
			original := s.core.appendFrame
			s.core.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
				if kind == 2 {
					return refusal
				}
				if original != nil {
					return original(kind, ordinal, high, payload)
				}
				return nil
			}
		})
		if err := s.Close(bg); err != refusal {
			t.Fatal("close refusal identity", err)
		}
		if calls := r.callsCopy(); len(calls) != 1 || calls[0].end == nil || calls[0].end.Reason != "failed" || calls[0].end.Error != refusal {
			t.Fatal("observer refusal ending/no advance", calls)
		}
	})
}

// Source observeCommits asserts usability without acquiring the transaction
// gate. A listener attached during storage must see THAT publication, even
// though preparation saw none. The timeout only releases a broken adapter's
// blocked writer; all spawned work is joined before fixture cleanup.
func TestMainSyncCommitObserverPublicationMembership(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s, add := mainSyncCounterSession(t, b)
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		s.taskBookkeeping(func() {
			original := s.core.appendFrame
			s.core.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
				if original != nil {
					if err := original(kind, ordinal, high, payload); err != nil {
						return err
					}
				}
				if kind == 2 {
					once.Do(func() { close(entered); <-release })
				}
				return nil
			}
		})
		committed := make(chan error, 1)
		go func() { committed <- add() }()
		watchReceive(t, entered)
		r := newMainSyncRecorder()
		registered := make(chan error, 1)
		go func() { _, err := s.observeStoredCommits(r.observer()); registered <- err }()
		select {
		case err := <-registered:
			close(release)
			if err != nil {
				t.Error(err)
			}
		case <-time.After(time.Second):
			t.Error("registration waited for storage/Session line")
			close(release)
			if err := watchReceive(t, registered); err != nil {
				t.Error(err)
			}
		}
		if err := watchReceive(t, committed); err != nil {
			t.Fatal(err)
		}
		if calls := r.callsCopy(); len(calls) != 1 || calls[0].end != nil {
			t.Fatal("publication-time membership", calls)
		}
	})
}
