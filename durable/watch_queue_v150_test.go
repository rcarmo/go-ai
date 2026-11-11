package durable

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Source pending.ts keeps 10,000 folded sets. The default pending-byte policy
// must not replace them just because three queued frames duplicate old values
// when their folded latest-value representation is within the same quota.
func TestV150WatchTenThousandSetsRemainIncremental(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	seedRootConversation(t, store)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	def, err := DefineDocument(DefinitionOptions{Kind: "source.watch.exact-threshold", Scope: "session", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"keys": map[string]any{}}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Commit(bg, func(tx *Tx) error { _, err := tx.AcquireDocument(def, 0, nil, nil); return err }); err != nil {
		t.Fatal(err)
	}
	watch, found, err := session.WatchDefinition(bg, def, 0, nil)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	defer watch.Stop()
	for _, bounds := range [][2]int{{0, 4000}, {4000, 8000}, {8000, 10000}} {
		if _, err := session.Commit(bg, func(tx *Tx) error {
			handle, err := tx.AcquireDocument(def, 0, nil, nil)
			if err != nil {
				return err
			}
			return handle.Update(func(value JSON) error {
				keys := value["keys"].(map[string]any)
				for i := bounds[0]; i < bounds[1]; i++ {
					keys[fmt.Sprintf("k%d", i)] = i
				}
				return nil
			})
		}); err != nil {
			t.Fatal(err)
		}
	}
	calls := make(chan rawWatchFrame, 1)
	if err := startDetachedDocumentFrames(watch, func(_ context.Context, frame rawWatchFrame) error { calls <- frame; return nil }); err != nil {
		t.Fatal(err)
	}
	frame := watchReceive(t, calls)
	if len(frame.Ops) != 10000 {
		t.Fatalf("source exact threshold: count=%d first=%v", len(frame.Ops), frame.Ops[0][0])
	}
	if len(frame.Value["keys"].(map[string]any)) != 10000 {
		t.Fatal("value lost keys")
	}
}

func TestV150CompactedWatchRetainsEveryReadinessGate(t *testing.T) {
	first, second := make(chan struct{}), make(chan struct{})
	close(second)
	frames := []rawWatchFrame{
		{Kind: "value", Value: JSON{"n": 1}, ownedValue: JSON{"n": 1}, Ops: []Operation{{"s", []any{"n"}, 1}}, ready: first},
		{Kind: "value", Value: JSON{"n": 2}, ownedValue: JSON{"n": 2}, Ops: []Operation{{"s", []any{"n"}, 2}}, ready: second},
	}
	folded, _, err := compactPendingWatchFrames(frames, DefaultLimits(), 1024)
	if err != nil || folded.ready != nil || len(folded.pendingReady) != 1 || folded.pendingReady[0] != first {
		t.Fatalf("readiness lost: count=%d error=%v", len(folded.pendingReady), err)
	}
	close(first)
}

func TestV150WatchStopReleasesPendingReadiness(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	seedRootConversation(t, store)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	ready := make(chan struct{})
	defer close(ready)
	watch := &DocumentWatch{session: session, current: JSON{"n": 0}, byteLimit: 1 << 20, closed: make(chan struct{}), started: true, listener: func(context.Context, rawWatchFrame) error {
		t.Error("stopped readiness delivered a callback")
		return nil
	}, pending: []rawWatchFrame{{Kind: "value", Value: JSON{"n": 1}, ownedValue: JSON{"n": 1}, Ops: []Operation{{"s", []any{"n"}, 1}}, pendingReady: []<-chan struct{}{ready}}}}
	defer watch.Stop()
	watch.kick()
	deadline := time.Now().Add(time.Second)
	for {
		watch.mu.Lock()
		waiting := watch.running && len(watch.pending) == 0
		watch.mu.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("watch worker never reached readiness wait")
		}
		time.Sleep(time.Millisecond)
	}
	watch.Stop()
	for {
		watch.mu.Lock()
		running := watch.running
		watch.mu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stopped watch retained a readiness worker")
		}
		time.Sleep(time.Millisecond)
	}
}
