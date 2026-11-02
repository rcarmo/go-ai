package durable

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
)

// c9f4c7b9 session-deferred-commits.test.ts:420. Reuse the real native
// deadline fixture (reserved root, manual foreground timers, raw initial doc).
func TestMainSessionDeferredUnloadDocuments(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		f := openMainDeadlineSession(t, b, nil)
		old, _ := mainDeferredLifecycleDefinitions(t)
		mainDeferredLifecycleAppend(t, f.session, old, 1, "held")
		if value, ok, err := f.session.SnapshotDefinition(bg, old, 1, nil); err != nil || !ok || !equalJSONValue(value, JSON{"text": "held"}) {
			t.Fatal(value, ok, err)
		}
		if err := f.session.UnloadDocuments(bg); err != nil {
			t.Fatal(err)
		}
		if value := mainDeferredSnapshot(t, b.store).Documents[f.doc].Value; !equalJSONValue(value, JSON{"text": "held"}) {
			t.Fatal("unload did not store hold", value)
		}
		f.session.taskBookkeeping(func() {
			if len(f.session.definitionCache) != 0 || len(f.session.heldDocuments) != 0 {
				t.Fatal("unload retained cache/hold")
			}
		})
		if value, ok, err := f.session.SnapshotDefinition(bg, old, 1, nil); err != nil || !ok || !equalJSONValue(value, JSON{"text": "held"}) {
			t.Fatal("cold reload", value, ok, err)
		}
	})
}

// c9f4c7b9:429. Snapshot reload stores a delta, not a migration base. The
// later typed commit migrates and edits the held value through the public Tx API.
func TestMainSessionDeferredDefinitionVersionReload(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		f := openMainDeadlineSession(t, b, nil)
		old, next := mainDeferredLifecycleDefinitions(t)
		mainDeferredLifecycleAppend(t, f.session, old, 1, "held")
		batches := mainDeferredLifecycleBatches(t, f.session)
		value, ok, err := f.session.SnapshotDefinition(bg, next, 1, nil)
		if err != nil || !ok || !equalJSONValue(value, JSON{"text": "held", "migrated": true}) {
			t.Fatal(value, ok, err)
		}
		writes := batches()
		if len(writes) != 1 || len(writes[0]) != 1 || writes[0][0].Op != "delta-document" {
			t.Fatal("reload did not store exactly a delta", writes)
		}
		if value := mainDeferredSnapshot(t, b.store).Documents[f.doc].Value; !equalJSONValue(value, JSON{"text": "held"}) {
			t.Fatal("reload stored migration or lost hold", value)
		}
		if _, err := f.session.Commit(bg, func(tx *Tx) error { _, err := tx.AcquireDocument(old, 1, nil, nil); return err }); err != nil {
			t.Fatal(err)
		}
		mainDeferredLifecycleAppend(t, f.session, old, 1, "+")
		if _, err := f.session.Commit(bg, func(tx *Tx) error {
			h, err := tx.AcquireDocument(next, 1, nil, nil)
			if err != nil {
				return err
			}
			return h.ApplyOperations([]Operation{{"a", []any{"text"}, "v2"}})
		}); err != nil {
			t.Fatal(err)
		}
		if value := mainDeferredSnapshot(t, b.store).Documents[f.doc].Value; !equalJSONValue(value, JSON{"text": "held+v2", "migrated": true}) {
			t.Fatal(value)
		}
	})
}

// c9f4c7b9:445. Native write names adapt document.change/document.retire;
// inspect actual backend-admitted commands rather than inferring from a callback.
func TestMainSessionDeferredRetirement(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		f := openMainDeadlineSession(t, b, nil)
		old, _ := mainDeferredLifecycleDefinitions(t)
		mainDeferredLifecycleAppend(t, f.session, old, 1, "held")
		batches := mainDeferredLifecycleBatches(t, f.session)
		if _, err := f.session.Commit(bg, func(tx *Tx) error { return tx.RetireDefinition(old, 1, nil) }); err != nil {
			t.Fatal(err)
		}
		writes := batches()
		if len(writes) != 1 || len(writes[0]) != 2 || writes[0][0].Op != "delta-document" || writes[0][1].Op != "retire-document" {
			t.Fatal("retirement batch/order", writes)
		}
		if delta := writes[0][0].Delta; delta.Version != 1 || !reflect.DeepEqual(delta.Ops, []Operation{{"a", []any{"text"}, "held"}}) {
			t.Fatal("retirement changed pending delta", delta)
		}
		f.session.taskBookkeeping(func() {
			if len(f.session.heldDocuments) != 0 {
				t.Fatal("retirement left hold")
			}
		})
		if value, ok, err := f.session.SnapshotDefinition(bg, old, 1, nil); err != nil || ok {
			t.Fatal("retired definition visible", value, ok, err)
		}
	})
}

// Source-derived from SessionImpl.#loadDocument: a version reload is a storage
// barrier before migration/user callback failure, not only a dependency folded
// into a later successful outer commit.
func TestMainSessionDeferredReloadRollbackBarrier(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		f := openMainDeadlineSession(t, b, nil)
		old, next := mainDeferredLifecycleDefinitions(t)
		mainDeferredLifecycleAppend(t, f.session, old, 1, "held")
		failure := errors.New("after reload")
		batches := mainDeferredLifecycleBatches(t, f.session)
		if _, err := f.session.Commit(bg, func(tx *Tx) error {
			h, err := tx.AcquireDocument(next, 1, nil, nil)
			if err != nil {
				return err
			}
			if err := h.ApplyOperations([]Operation{{"a", []any{"text"}, "discard"}}); err != nil {
				return err
			}
			return failure
		}); err != failure {
			t.Fatal(err)
		}
		doc := mainDeferredSnapshot(t, b.store).Documents[f.doc]
		if doc.Version != 1 || !equalJSONValue(doc.Value, JSON{"text": "held"}) {
			t.Fatal("callback rollback lost reload barrier or stored private migration", doc)
		}
		if writes := batches(); len(writes) != 1 || len(writes[0]) != 1 || writes[0][0].Op != "delta-document" {
			t.Fatal("reload barrier writes", writes)
		}
	})
}

// Native composition control: two independent source reload barriers must not
// lose earlier staged writes, reserved IDs, receipt history or publication readiness.
func TestMainSessionDeferredMultipleReloadPrefixes(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		f := openMainDeadlineSession(t, b, nil)
		old, next := mainDeferredLifecycleDefinitions(t)
		var peer, peerDoc ID
		if _, err := f.session.Commit(bg, func(tx *Tx) error {
			var err error
			peer, err = tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.CreateConversation(Conversation{ID: peer}); err != nil {
				return err
			}
			h, err := tx.AcquireDocument(old, peer, nil, nil)
			if err == nil {
				peerDoc = h.id
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		mainDeferredLifecycleAppend(t, f.session, old, 1, "one")
		mainDeferredLifecycleAppend(t, f.session, old, peer, "two")
		before := mainDeferredSnapshot(t, b.store).Seq
		publications := f.publications()
		batches := mainDeferredLifecycleBatches(t, f.session)
		var entry ID
		seq, err := f.session.Commit(bg, func(tx *Tx) error {
			var err error
			entry, err = tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.AppendEntry(Entry{ID: entry, Conversation: 1, Kind: "note", Value: JSON{"text": "staged"}}); err != nil {
				return err
			}
			h, err := tx.AcquireDocument(next, 1, nil, nil)
			if err != nil {
				return err
			}
			if err := h.ApplyOperations([]Operation{{"a", []any{"text"}, "+"}}); err != nil {
				return err
			}
			_, err = tx.AcquireDocument(next, peer, nil, nil)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		writes := batches()
		if len(writes) != 3 || len(writes[0]) != 1 || len(writes[1]) != 1 || writes[0][0].Op != "delta-document" || writes[0][0].Delta.ID != f.doc || writes[1][0].Op != "delta-document" || writes[1][0].Delta.ID != peerDoc {
			t.Fatal("selected reload prefixes", writes)
		}
		raw := mainDeferredSnapshot(t, b.store)
		if seq != before+3 || raw.Seq != seq || raw.Entries[entry].Value["text"] != "staged" || !equalJSONValue(raw.Documents[f.doc].Value, JSON{"text": "one+", "migrated": true}) || !equalJSONValue(raw.Documents[peerDoc].Value, JSON{"text": "two", "migrated": true}) {
			t.Fatal("reload lost staged work/receipts", seq, before, raw)
		}
		f.stored.mu.Lock()
		frames := append([]PublicationFrame{}, f.stored.pending[publications:]...)
		f.stored.mu.Unlock()
		if len(frames) != 3 {
			t.Fatal("stored reload publications", len(frames))
		}
		for i, frame := range frames {
			if frame.Publication.Seq != before+uint64(i)+1 {
				t.Fatal("reload publication receipt", frame.Publication.Seq)
			}
			select {
			case <-frame.ready:
			default:
				t.Fatal("reload publication never became ready")
			}
		}
	})
}

// Source #loadDocument stores before materializeDocument invokes migration.
func TestMainSessionDeferredReloadMigrationFailure(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		f := openMainDeadlineSession(t, b, nil)
		old, next := mainDeferredLifecycleDefinitions(t)
		mainDeferredLifecycleAppend(t, f.session, old, 1, "held")
		failure := errors.New("migration failed")
		next.options.Migrate = func(JSON, uint64) (JSON, error) { return nil, failure }
		if _, _, err := f.session.SnapshotDefinition(bg, next, 1, nil); err != failure {
			t.Fatal(err)
		}
		if value := mainDeferredSnapshot(t, b.store).Documents[f.doc].Value; !equalJSONValue(value, JSON{"text": "held"}) {
			t.Fatal("migration failure lost reload barrier", value)
		}
	})
}

// Native cancellation contract: do not admit a cancelled reload; once its store
// is admitted, settle/adopt it even if the requesting reader cancels.
func TestMainSessionDeferredReloadCancellation(t *testing.T) {
	for _, admitted := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-admission", true: "after-admission"}[admitted], func(t *testing.T) {
			utf16Backends(t, func(t *testing.T, b backend) {
				f := openMainDeadlineSession(t, b, nil)
				old, next := mainDeferredLifecycleDefinitions(t)
				mainDeferredLifecycleAppend(t, f.session, old, 1, "held")
				before := mainDeferredSnapshot(t, b.store).Seq
				ctx, cancel := context.WithCancel(bg)
				defer cancel()
				if admitted {
					f.session.taskBookkeeping(func() {
						if err := f.session.core.enter(bg); err != nil {
							t.Fatal(err)
						}
						defer f.session.core.leave()
						original := f.session.core.appendFrame
						f.session.core.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
							if kind == 2 {
								cancel()
							}
							if original != nil {
								return original(kind, ordinal, high, payload)
							}
							return nil
						}
					})
					if _, _, err := f.session.SnapshotDefinition(ctx, next, 1, nil); !errors.Is(err, context.Canceled) {
						t.Fatal("admitted reader did not cancel", err)
					}
				} else {
					if _, err := f.session.Commit(ctx, func(tx *Tx) error { cancel(); _, err := tx.AcquireDocument(next, 1, nil, nil); return err }); !errors.Is(err, context.Canceled) {
						t.Fatal(err)
					}
				}
				raw := mainDeferredSnapshot(t, b.store)
				if admitted {
					if raw.Seq != before+1 || !equalJSONValue(raw.Documents[f.doc].Value, JSON{"text": "held"}) {
						t.Fatal("cancel abandoned admitted reload", raw)
					}
				} else if raw.Seq != before || !equalJSONValue(raw.Documents[f.doc].Value, JSON{"text": ""}) {
					t.Fatal("cancel admitted reload", raw)
				}
				value, ok, err := f.session.SnapshotDefinition(bg, old, 1, nil)
				if err != nil || !ok || !equalJSONValue(value, JSON{"text": "held"}) {
					t.Fatal("cancel lost Session/image", value, ok, err)
				}
			})
		})
	}
}

func mainDeferredLifecycleDefinitions(t *testing.T) (*DocumentDefinition, *DocumentDefinition) {
	t.Helper()
	options := DefinitionOptions{Kind: "test.deferred.text", Version: 1, Scope: "conversation", History: "latest", Fork: "initial", Initial: func(JSON) (JSON, error) { return JSON{"text": ""}, nil }, CheckpointWhen: func(value JSON, _ []Operation, _ CheckpointInfo) (bool, error) {
		if value["explode"] == true {
			return false, errors.New("checkpoint failed")
		}
		return value["base"] == true, nil
	}}
	old, err := DefineDocument(options)
	if err != nil {
		t.Fatal(err)
	}
	options.Version = 2
	options.Initial = func(JSON) (JSON, error) { return JSON{"text": "", "migrated": true}, nil }
	options.Migrate = func(value JSON, _ uint64) (JSON, error) { value["migrated"] = true; return value, nil }
	options.CheckpointWhen = func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }
	next, err := DefineDocument(options)
	if err != nil {
		t.Fatal(err)
	}
	return old, next
}
func mainDeferredLifecycleAppend(t *testing.T, s *Session, def *DocumentDefinition, owner ID, text string) {
	t.Helper()
	_, err := s.commitPersistence(bg, func(tx *Tx) error {
		h, err := tx.AcquireDocument(def, owner, nil, nil)
		if err != nil {
			return err
		}
		return h.ApplyOperations([]Operation{{"a", []any{"text"}, text}})
	}, true, 0, true)
	if err != nil {
		t.Fatal(err)
	}
}

// Preserve the real appender, record only successful admitted commit commands,
// and decode the actual backend payload. This manufactures no publication/receipt.
func mainDeferredLifecycleBatches(t *testing.T, s *Session) func() [][]Write {
	t.Helper()
	var mu sync.Mutex
	var batches [][]Write
	s.taskBookkeeping(func() {
		if err := s.core.enter(bg); err != nil {
			t.Fatal(err)
		}
		defer s.core.leave()
		original := s.core.appendFrame
		s.core.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
			if original != nil {
				if err := original(kind, ordinal, high, payload); err != nil {
					return err
				}
			}
			if kind == 2 {
				var record commitRecord
				if err := decodeStrict(payload, s.limits, s.limits.MaxFramePayloadBytes, &record); err != nil {
					return err
				}
				mu.Lock()
				batches = append(batches, record.Writes)
				mu.Unlock()
			}
			return nil
		}
	})
	return func() [][]Write { mu.Lock(); defer mu.Unlock(); return append([][]Write{}, batches...) }
}
