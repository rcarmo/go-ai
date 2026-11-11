package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// Source session-observers.test.ts:389/403/414/427 through a scheduler-backed
// Session. A private fast report callback adapts the source reporter; public
// readonly ownership and host reporting are tested separately.
func TestMainHarnessStoredObserverLifecycle(t *testing.T) {
	for _, mode := range []string{"close", "failure", "detach", "listener"} {
		t.Run(mode, func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			h := taskTestHarness(t, store)
			def := mustDefinition(t, DefinitionOptions{Kind: "source.commit.observer.harness", Scope: "session", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"n": 0}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
			if err := sourceDocumentSet(h.session, def, 0, 1, false); err != nil {
				t.Fatal(err)
			}
			var calls []uint64
			var endings []WatchEnd
			var reports []error
			bug := errors.New("listener bug")
			observer, err := h.session.observeStoredCommits(sessionCommitObserver{
				advance: func(_ context.Context, p sessionPublication) error {
					calls = append(calls, p.Seq)
					if mode == "listener" {
						return bug
					}
					return nil
				},
				end:    func(end WatchEnd) error { endings = append(endings, end); return nil },
				report: func(_ context.Context, err error, _ ReportInfo) { reports = append(reports, err) },
			})
			if err != nil {
				t.Fatalf("scheduler observer unavailable: %v", err)
			}
			defer observer.detach()
			if mode == "failure" {
				original := store.appendFrame
				failure := &StorageRejected{Reason: "disk gone"}
				var active atomic.Bool
				active.Store(true)
				store.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
					if kind == 2 && active.Swap(false) {
						return failure
					}
					return original(kind, ordinal, high, payload)
				}
				t.Cleanup(func() { _ = h.Close(bg); store.appendFrame = original })
				if err := sourceDocumentSet(h.session, def, 0, 2, false); !errors.Is(err, failure) {
					t.Fatalf("storage rejection %v", err)
				}
				ctx, cancel := context.WithTimeout(bg, 4*time.Second)
				defer cancel()
				if _, err := h.session.Closed(ctx); err != nil {
					t.Fatal(err)
				}
				if len(calls) != 0 || len(endings) != 1 || endings[0].Reason != "failed" || !errors.Is(endings[0].Error, failure) {
					t.Fatalf("failure calls=%v ends=%+v", calls, endings)
				}
				return
			}
			if err := sourceDocumentSet(h.session, def, 0, 2, false); err != nil {
				t.Fatal(err)
			}
			if mode == "detach" {
				observer.detach()
				observer.detach()
			}
			if err := sourceDocumentSet(h.session, def, 0, 3, false); err != nil {
				t.Fatal(err)
			}
			if err := h.Close(bg); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "close":
				if len(calls) != 2 || calls[0] >= calls[1] || len(endings) != 1 || endings[0].Reason != "failed" || endings[0].Error == nil || endings[0].Error.Error() != "session closed" {
					t.Fatalf("close calls=%v ends=%+v", calls, endings)
				}
			case "detach":
				if len(calls) != 1 || len(endings) != 0 {
					t.Fatalf("detach calls=%v ends=%+v", calls, endings)
				}
			case "listener":
				if len(calls) != 1 || len(endings) != 1 || endings[0].Reason != "listener_error" || endings[0].Error != bug || len(reports) != 1 || reports[0] != bug || h.session.failure() != nil {
					t.Fatalf("guard calls=%v ends=%+v reports=%v session=%v", calls, endings, reports, h.session.failure())
				}
			}
		})
	}
}
