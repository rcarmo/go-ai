package durable

import (
	"context"
	"errors"
	"testing"
)

// Source session-observers.test.ts:215, through the public Harness and
// conversation-bound attachment APIs. Go errors stand in for JS throws.
func TestMainPublicHarnessDocumentObserverIsolationAndClose(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	var reports []error
	var infos []ReportInfo
	h := taskTestHarnessOptions(t, store, Options{OnReportWithInfo: func(_ context.Context, err error, info ReportInfo) {
		reports = append(reports, err)
		infos = append(infos, info)
	}})
	root, err := h.Root(bg, AgentChange{})
	if err != nil {
		t.Fatal(err)
	}
	def := mustDefinition(t, DefinitionOptions{Kind: "public.observer", Scope: "conversation", Version: 1, History: "latest", Fork: "initial", Initial: func(JSON) (JSON, error) { return JSON{"count": 0}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
	set := func(n int) {
		t.Helper()
		_, err := root.Commit(bg, func(tx *Tx) error {
			doc, err := tx.AcquireDocument(def, root.ID(), nil, nil)
			if err != nil {
				return err
			}
			return doc.Set(JSON{"count": n})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	set(1)
	failure := errors.New("public observer failure")
	var badEnds, goodEnds []WatchEnd
	var badCalls, goodCalls int
	bad, found, err := h.ObserveDefinition(bg, def, root.ID(), nil, DocumentObserver{
		Advance: func(context.Context, DocumentCall) error { badCalls++; return failure },
		End:     func(end WatchEnd) error { badEnds = append(badEnds, end); return nil },
	})
	if err != nil || !found || bad == nil {
		t.Fatal(found, err)
	}
	good, found, err := root.ObserveDefinition(bg, def, nil, DocumentObserver{
		Advance: func(context.Context, DocumentCall) error { goodCalls++; return nil },
		End:     func(end WatchEnd) error { goodEnds = append(goodEnds, end); return nil },
	})
	if err != nil || !found || good == nil {
		t.Fatal(found, err)
	}
	set(2)
	set(3)
	if badCalls != 1 || goodCalls != 2 || len(badEnds) != 1 || badEnds[0].Reason != "listener_error" || !errors.Is(badEnds[0].Error, failure) || len(reports) != 1 || reports[0] != failure || infos[0].Type != "listener_error" {
		t.Fatalf("calls=%d/%d badEnds=%+v reports=%v infos=%+v", badCalls, goodCalls, badEnds, reports, infos)
	}
	if err := h.Close(bg); err != nil {
		t.Fatal(err)
	}
	if len(goodEnds) != 1 || goodEnds[0].Reason != "failed" {
		t.Fatalf("close endings=%+v", goodEnds)
	}
	if _, _, err := h.ObserveDefinition(bg, def, root.ID(), nil, DocumentObserver{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed attach: %v", err)
	}
	good.Detach()
	bad.Detach()
}
