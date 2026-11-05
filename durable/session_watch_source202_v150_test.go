package durable

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

// Pinned session-watches.test.ts:202. The source mutates a readonly-facing
// document; Go uses the public DocumentHandle.Update callback to stage it.
func TestV150WatchSource202MoreThanTenThousandFoldedOps(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	def, err := DefineDocument(DefinitionOptions{Kind: "watch.keys.source202", Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"keys": map[string]any{}}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
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
	for _, bounds := range [][2]int{{0, 4000}, {4000, 8000}, {8000, 10001}} {
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
			t.Fatalf("source commit [%d,%d) rejected: %v", bounds[0], bounds[1], err)
		}
	}
	calls := make(chan WatchFrame, 2)
	if err = watch.StartFrame(func(_ context.Context, frame WatchFrame) error { calls <- frame; return nil }); err != nil {
		t.Fatal(err)
	}
	frame := watchReceive(t, calls)
	value, err := watch.Value()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(frame.Value, value) || len(frame.Ops) != 1 || frame.Ops[0][0] != "r" || !reflect.DeepEqual(frame.Ops[0][1], map[string]any(value)) {
		t.Fatalf("expected one root replacement: %+v", frame)
	}
	keys, ok := value["keys"].(map[string]any)
	if !ok || len(keys) != 10001 {
		t.Fatalf("keys count: %d (%T)", len(keys), value["keys"])
	}
	select {
	case extra := <-calls:
		t.Fatalf("extra delivery: %+v", extra)
	default:
	}
}
