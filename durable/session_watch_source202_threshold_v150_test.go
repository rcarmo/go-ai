package durable

import (
	"context"
	"fmt"
	"testing"
)

// Source session-watches.test.ts:202 and Chord pending.ts boundary. This
// companion checks the 10,000-op threshold that source202's final root alone
// cannot distinguish from an earlier quota or native-operation fallback.
func TestV150WatchSource202NoEarlyRootAtEightThousand(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxMembers = 12000
	limits.MaxRetainedBytes = 64 << 20
	store, err := openTestSQLite(t, sqliteFixtureOptions{Limits: &limits})
	if err != nil {
		t.Fatal(err)
	}
	seedRootConversation(t, store)
	s, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	def, err := DefineDocument(DefinitionOptions{Kind: "watch.keys.source202.threshold", Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"keys": map[string]any{}}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Commit(bg, func(tx *Tx) error { _, err := tx.AcquireDocument(def, 0, nil, nil); return err }); err != nil {
		t.Fatal(err)
	}
	watch, found, err := s.WatchDefinition(bg, def, 0, nil)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	defer watch.Stop()
	for _, bounds := range [][2]int{{0, 4000}, {4000, 8000}} {
		_, err = s.Commit(bg, func(tx *Tx) error {
			doc, err := tx.AcquireDocument(def, 0, nil, nil)
			if err != nil {
				return err
			}
			return doc.Update(func(value JSON) error {
				keys, ok := value["keys"].(map[string]any)
				if !ok {
					return fmt.Errorf("invalid keys object %T", value["keys"])
				}
				for i := bounds[0]; i < bounds[1]; i++ {
					keys[fmt.Sprintf("k%d", i)] = i
				}
				return nil
			})
		})
		if err != nil {
			t.Fatalf("commit [%d,%d): %v", bounds[0], bounds[1], err)
		}
	}
	calls := make(chan WatchFrame, 2)
	if err = watch.StartFrame(func(_ context.Context, frame WatchFrame) error { calls <- frame; return nil }); err != nil {
		t.Fatal(err)
	}
	frame := watchReceive(t, calls)
	if len(frame.Ops) != 8000 {
		verb := "empty"
		if len(frame.Ops) > 0 {
			verb = fmt.Sprint(frame.Ops[0][0])
		}
		t.Fatalf("source retains 8000 pending ops, got %d first=%s", len(frame.Ops), verb)
	}
	for i, op := range frame.Ops {
		if op[0] != "s" {
			t.Fatalf("op %d is %v", i, op)
		}
	}
	value, err := watch.Value()
	if err != nil {
		t.Fatal(err)
	}
	keys, ok := value["keys"].(map[string]any)
	if !ok || len(keys) != 8000 {
		t.Fatalf("keys count %d (%T)", len(keys), value["keys"])
	}
	select {
	case extra := <-calls:
		t.Fatalf("extra delivery: %+v", extra)
	default:
	}
}
