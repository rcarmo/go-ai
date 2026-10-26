package durable

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"
)

func TestDocument120IdleRetentionZeroExpiryAndWatchPin(t *testing.T) {
	for _, mode := range []string{"zero", "expiry", "pinned"} {
		t.Run(mode, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var calls atomic.Int64
				retention := int64(20)
				if mode == "zero" {
					retention = 0
				}
				s, e := OpenSessionWithOptions(b.store, SessionOptions{DocumentRetentionMs: &retention})
				if e != nil {
					t.Fatal(e)
				}
				defer s.Close(bg)
				def, e := DefineDocument(DefinitionOptions{CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }, Kind: "retained", Version: 2, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"x": 0}, nil }, Migrate: func(v JSON, _ uint64) (JSON, error) { calls.Add(1); return v, nil }})
				if e != nil {
					t.Fatal(e)
				}
				_, e = s.Commit(bg, func(tx *Tx) error {
					id, e := tx.MintID()
					if e != nil {
						return e
					}
					_, e = tx.CreateDocument(Document{ID: id, Scope: "session", Kind: "retained", Version: 1, Value: JSON{"x": 1}})
					return e
				})
				if e != nil {
					t.Fatal(e)
				}
				var watch *DocumentWatch
				if mode == "pinned" {
					watch, _, e = s.WatchDefinition(bg, def, 0, nil)
					if e != nil {
						t.Fatal(e)
					}
					defer watch.Stop()
				}
				read := func() {
					v, ok, e := s.SnapshotDefinition(bg, def, 0, nil)
					if e != nil || !ok || v["x"] != json.Number("1") {
						t.Fatal(v, ok, e)
					}
				}
				read()
				read()
				if mode == "zero" {
					if calls.Load() != 2 {
						t.Fatal("zero retention cached", calls.Load())
					}
					return
				}
				if calls.Load() != 1 {
					t.Fatal("warm migration reran", calls.Load())
				}
				if mode == "pinned" {
					time.Sleep(60 * time.Millisecond)
					read()
					if calls.Load() != 1 {
						t.Fatal("attached value expired", calls.Load())
					}
					watch.Stop()
				}
				deadline, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				for {
					empty := false
					s.taskBookkeeping(func() { empty = len(s.definitionCache) == 0 })
					if empty {
						break
					}
					select {
					case <-deadline.Done():
						t.Fatal("idle document timer did not evict")
					case <-time.After(time.Millisecond):
					}
				}
				read()
				if calls.Load() != 2 {
					t.Fatal("expired migration not recomputed", calls.Load())
				}
			})
		})
	}
}
