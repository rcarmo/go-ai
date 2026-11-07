package durable

import (
	"errors"
	"reflect"
	"testing"
)

// TestV150SessionFailureCarriesOriginalRejectionValue ports the original failure
// semantics: a storage rejection carries its value verbatim. The rejecting call is
// rejected with that value, later calls get a SessionFailed whose cause is the
// same value, and the Session end reports it as the failure error. The original
// failure slot is `{ error: unknown }`, so present null/undefined/falsy values are
// failures, not absences.
// Source: session/session.ts:274 (#failure: { readonly error: unknown }), 1642, 1740, 1839-1847.
func TestV150SessionFailureCarriesOriginalRejectionValue(t *testing.T) {
	for _, value := range []any{commitUndefinedValue{}, nil, false, 0, ""} {
		t.Run(reflect.ValueOf(value).Kind().String(), func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				s, err := OpenSession(b.store)
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close(bg)
				core := b.store.(nativeStorage).nativeCore()
				if _, err := s.Commit(bg, func(tx *Tx) error {
					id, e := tx.MintID()
					if e != nil {
						return e
					}
					return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "setup", Value: JSON{}})
				}); err != nil {
					t.Fatal(err)
				}
				core.appendFrame = func(byte, uint64, uint64, []byte) error {
					return newFailure(value)
				}

				// The rejecting call keeps the original value.
				_, err = s.Commit(bg, func(tx *Tx) error {
					id, e := tx.MintID()
					if e != nil {
						return e
					}
					return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "reject", Value: JSON{}})
				})
				var failure *Failure
				if !errors.As(err, &failure) || !reflect.DeepEqual(failure.Value(), value) {
					t.Fatalf("rejecting call lost the value: %v (%#v)", err, err)
				}
				if err == nil {
					t.Fatal("rejection accepted")
				}

				// A later call reports the first cause, value intact.
				var failed *SessionFailed
				if _, err := s.Commit(bg, func(tx *Tx) error { return nil }); !errors.As(err, &failed) {
					t.Fatalf("later call not fenced with SessionFailed: %v", err)
				} else if !reflect.DeepEqual(failed.Failure().Value(), value) {
					t.Fatalf("SessionFailed lost the cause value: %#v", failed.Failure().Value())
				}

				// The Session end carries the same value.
				end, err := s.Closed(bg)
				if err != nil {
					t.Fatal(err)
				}
				if end.Reason != "failed" || end.Failure == nil || !reflect.DeepEqual(end.Failure.Value(), value) {
					t.Fatalf("session end lost the value: %+v", end)
				}
			})
		})
	}
}

// TestV150SessionFailureKeepsErrorIdentity pins the error-valued case: when the
// rejection is a Go error its identity survives unchanged, so existing
// errors.Is/errors.As classification keeps working.
func TestV150SessionFailureKeepsErrorIdentity(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		core := b.store.(nativeStorage).nativeCore()
		cause := errors.New("lost acknowledgement")
		core.appendFrame = func(byte, uint64, uint64, []byte) error {
			return cause
		}
		_, err = s.Commit(bg, func(tx *Tx) error {
			id, e := tx.MintID()
			if e != nil {
				return e
			}
			return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "reject", Value: JSON{}})
		})
		if !errors.Is(err, cause) {
			t.Fatalf("error cause identity lost: %v", err)
		}
		end, err := s.Closed(bg)
		if err != nil {
			t.Fatal(err)
		}
		if end.Reason != "failed" || !errors.Is(end.Error, cause) {
			t.Fatalf("session end lost the cause: %+v", end)
		}
	})
}
