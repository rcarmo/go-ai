package durable

import (
	"context"
	"errors"
	"testing"
)

// Snapshot authority is not established by a sequence or document ID. An
// adapter may project a different value without changing either field.
type mainDefinitionProjection struct {
	nativeStorage
	id      ID
	origin  string
	missing bool
	failure error
	reads   int
}

func (a *mainDefinitionProjection) Snapshot(ctx context.Context) (Snapshot, error) {
	a.reads++
	if a.failure != nil {
		return Snapshot{}, a.failure
	}
	state, err := a.nativeStorage.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if a.missing {
		delete(state.Documents, a.id)
	} else if d, ok := state.Documents[a.id]; ok {
		d.Value = JSON{"origin": a.origin}
		state.Documents[a.id] = d
	}
	return state, nil
}

func TestMainDefinitionWrapperProjectionOverridesNativeCache(t *testing.T) {
	for _, api := range []string{"snapshot", "watch", "state", "acquire"} {
		t.Run(api, func(t *testing.T) {
			utf16Backends(t, func(t *testing.T, b backend) {
				id := mint(t, b.store)
				apply(t, b.store, Write{Op: "put-document", Document: &Document{ID: id, Scope: "session", Kind: "test.wrapper-definition", Version: 1, Value: JSON{"origin": "native"}}})
				s, err := OpenSession(b.store)
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close(bg)
				calls := 0
				def, err := DefineDocument(DefinitionOptions{Kind: "test.wrapper-definition", Scope: "session", Version: 2,
					Initial: func(JSON) (JSON, error) { t.Fatal("existing document initialized"); return nil, nil },
					Migrate: func(value JSON, from uint64) (JSON, error) {
						calls++
						if from != 1 {
							t.Fatal(from)
						}
						value["migrated"] = true
						return value, nil
					},
					CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil },
				})
				if err != nil {
					t.Fatal(err)
				}
				initial, ok, err := s.SnapshotDefinition(bg, def, 0, nil)
				if err != nil || !ok || initial["origin"] != "native" || calls != 1 {
					t.Fatal(initial, ok, err, calls)
				}
				state, ok, err := s.DocumentState(bg, def, 0, nil)
				if err != nil || !ok {
					t.Fatal(ok, err)
				}
				defer state.Dispose()
				adapter := &mainDefinitionProjection{nativeStorage: b.store.(nativeStorage), id: id}
				s.taskBookkeeping(func() { s.store = adapter })
				rollback := errors.New("definition projection rollback")
				read := func() (JSON, bool, error) {
					switch api {
					case "watch":
						watch, ok, err := s.WatchDefinition(bg, def, 0, nil)
						if err != nil || !ok {
							return nil, ok, err
						}
						defer watch.Stop()
						value, err := watch.Value()
						return value, err == nil, err
					case "state":
						value, err := state.Value(bg)
						return value, value != nil && err == nil, err
					case "acquire":
						var value JSON
						_, err := s.Commit(bg, func(tx *Tx) error {
							handle, err := tx.AcquireDocument(def, 0, nil, nil)
							if err != nil {
								return err
							}
							value, err = handle.Get()
							if err != nil {
								return err
							}
							return rollback
						})
						if errors.Is(err, rollback) {
							return value, true, nil
						}
						return nil, false, err
					default:
						return s.SnapshotDefinition(bg, def, 0, nil)
					}
				}
				for _, origin := range []string{"projection-one", "projection-two"} {
					s.taskBookkeeping(func() { adapter.origin = origin })
					before := calls
					value, ok, err := read()
					if err != nil || !ok || value["origin"] != origin || value["migrated"] != true {
						t.Fatal("adapter value replaced by cached native value", value, ok, err)
					}
					if calls != before+1 {
						t.Fatal("adapter migration bypassed", calls, before)
					}
					value["origin"] = "caller mutation"
				}
				// Adapter request failures keep their existing nonfatal projection, and a
				// failed Snapshot never reaches migration or the transaction callback.
				s.taskBookkeeping(func() { adapter.failure = reject("definition adapter request") })
				before := calls
				if _, _, err := read(); err == nil {
					t.Fatal("adapter error lost")
				} else {
					var request *StorageRequestError
					if !errors.As(err, &request) || request.Reason != "definition adapter request" {
						t.Fatal(err)
					}
				}
				if calls != before || s.failure() != nil {
					t.Fatal("failed read migrated or failed Session", calls, before, s.failure())
				}
				// Returning to native storage must recover the original warm value. Reads
				// of projected data must neither refresh nor overwrite that cache entry.
				s.taskBookkeeping(func() { s.store = b.store })
				value, ok, err := s.SnapshotDefinition(bg, def, 0, nil)
				if err != nil || !ok || value["origin"] != "native" || calls != before {
					t.Fatal("native migration cache lost or contaminated", value, ok, err, calls, before)
				}
				snapshot, err := b.store.Snapshot(bg)
				if err != nil || snapshot.Seq != 1 || snapshot.Documents[id].Version != 1 || snapshot.Documents[id].Value["origin"] != "native" {
					t.Fatal("read persisted projected migration", snapshot, err)
				}
			})
		})
	}
}

func TestMainDefinitionWrapperUnmigratedProjection(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		id := mint(t, b.store)
		apply(t, b.store, Write{Op: "put-document", Document: &Document{ID: id, Scope: "session", Kind: "test.wrapper-plain", Version: 1, Value: JSON{"origin": "native"}}})
		s, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		def, err := DefineDocument(DefinitionOptions{Kind: "test.wrapper-plain", Scope: "session", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.SnapshotDefinition(bg, def, 0, nil); err != nil {
			t.Fatal(err)
		}
		adapter := &mainDefinitionProjection{nativeStorage: b.store.(nativeStorage), id: id, origin: "projected"}
		s.taskBookkeeping(func() { s.store = adapter })
		value, ok, err := s.SnapshotDefinition(bg, def, 0, nil)
		if err != nil || !ok || value["origin"] != "projected" {
			t.Fatal("plain projection bypassed", value, ok, err)
		}
		s.taskBookkeeping(func() { adapter.missing = true })
		if value, ok, err := s.SnapshotDefinition(bg, def, 0, nil); err != nil || ok || value != nil {
			t.Fatal("absent projection revived", value, ok, err)
		}
	})
}

func TestMainDefinitionWrapperNeverAdmitsMigrationCache(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		id := mint(t, b.store)
		apply(t, b.store, Write{Op: "put-document", Document: &Document{ID: id, Scope: "session", Kind: "test.wrapper-uncached", Version: 1, Value: JSON{"origin": "native"}}})
		adapter := &mainDefinitionProjection{nativeStorage: b.store.(nativeStorage), id: id, origin: "projected"}
		s, err := OpenSession(adapter)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		mode := "valid"
		calls := 0
		sentinel := errors.New("migration callback error")
		def, err := DefineDocument(DefinitionOptions{Kind: "test.wrapper-uncached", Scope: "session", Version: 2,
			Initial: func(JSON) (JSON, error) { t.Fatal("unexpected initializer"); return nil, nil },
			Migrate: func(value JSON, _ uint64) (JSON, error) {
				calls++
				switch mode {
				case "callback-error":
					return nil, sentinel
				case "invalid-value":
					return JSON{"invalid": func() {}}, nil
				}
				value["migrated"] = true
				return value, nil
			},
			CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, next := range []string{"valid", "callback-error", "invalid-value", "valid"} {
			mode = next
			before := calls
			value, ok, err := s.SnapshotDefinition(bg, def, 0, nil)
			switch mode {
			case "callback-error":
				if !errors.Is(err, sentinel) || ok {
					t.Fatal("migration error lost", ok, err)
				}
			case "invalid-value":
				if err == nil || ok {
					t.Fatal("invalid migration value accepted", value, ok, err)
				}
			default:
				if err != nil || !ok || value["origin"] != "projected" || value["migrated"] != true {
					t.Fatal(value, ok, err)
				}
				value["origin"] = "caller mutation"
			}
			if calls != before+1 {
				t.Fatal("wrapper reused migration", calls, before)
			}
			s.taskBookkeeping(func() {
				if len(s.definitionCache) != 0 || s.definitionCacheBytes != 0 || s.documentExpiry != nil {
					t.Fatal("wrapper admitted or scheduled native cache")
				}
			})
		}
		if s.failure() != nil {
			t.Fatal("projection error failed session", s.failure())
		}
	})
}
