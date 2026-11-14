package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

// Source session-observers.test.ts:132, 469, 496 and 520. Public values are
// readonly handles; returned Go errors adapt source throws.
func TestMainPublicDocumentObserverRetirementAndGuards(t *testing.T) {
	for _, mode := range []string{"follow", "self-detach", "peer-detach", "error"} {
		t.Run(mode, func(t *testing.T) {
			s, _, def, owner := sourceDocumentObserverSession(t, false)
			if err := sourceDocumentSet(s, def, owner, 1, false); err != nil {
				t.Fatal(err)
			}
			var first, second *DocumentObservation
			var calls []string
			var firstFrames, secondFrames []DocumentCall
			var firstEnds, secondEnds []WatchEnd
			failure := errors.New("retirement callback failed")
			first, found, err := s.ObserveDefinition(bg, def, owner, nil, DocumentObserver{
				Advance: func(_ context.Context, frame DocumentCall) error {
					calls = append(calls, "first")
					firstFrames = append(firstFrames, frame)
					if frame.Retired {
						switch mode {
						case "self-detach":
							first.Detach()
						case "peer-detach":
							second.Detach()
						case "error":
							return failure
						}
					}
					return nil
				},
				End: func(end WatchEnd) error { firstEnds = append(firstEnds, end); return nil },
			})
			if err != nil || !found {
				t.Fatal(found, err)
			}
			second, found, err = s.ObserveDefinition(bg, def, owner, nil, DocumentObserver{
				Advance: func(_ context.Context, frame DocumentCall) error {
					calls = append(calls, "second")
					secondFrames = append(secondFrames, frame)
					return nil
				},
				End: func(end WatchEnd) error { secondEnds = append(secondEnds, end); return nil },
			})
			if err != nil || !found {
				t.Fatal(found, err)
			}
			defer first.Detach()
			defer second.Detach()
			_, err = s.Commit(bg, func(tx *Tx) (any, error) {
				if err := tx.RetireDefinition(def, owner, nil); err != nil {
					return nil, err
				}
				doc, err := tx.AcquireDocument(def, owner, nil, nil)
				if err != nil {
					return nil, err
				}
				return nil, doc.ApplyOperations([]Operation{{"s", []any{"count"}, 2}})
			})
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"first", "first", "second", "second"}
			if mode == "self-detach" || mode == "error" {
				want = []string{"first", "second", "second"}
			}
			if mode == "peer-detach" {
				want = []string{"first", "first"}
			}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("calls %v want %v", calls, want)
			}
			if len(firstFrames) == 0 || !firstFrames[0].Retired || immutableViewMember(t, firstFrames[0].Value, "count").Number() != json.Number("1") || len(firstFrames[0].Ops) != 0 {
				t.Fatalf("retirement %+v", firstFrames)
			}
			if mode == "follow" || mode == "peer-detach" {
				if len(firstFrames) != 2 || firstFrames[1].Retired || immutableViewMember(t, firstFrames[1].Value, "count").Number() != json.Number("2") || len(firstFrames[1].Ops) != 1 || firstFrames[1].Ops[0][0] != "r" {
					t.Fatalf("replacement %+v", firstFrames)
				}
			}
			if mode == "error" {
				if len(firstEnds) != 1 || firstEnds[0].Reason != "listener_error" || !errors.Is(firstEnds[0].Error, failure) {
					t.Fatalf("error end %+v", firstEnds)
				}
			} else if len(firstEnds) != 0 {
				t.Fatalf("unexpected first end %+v", firstEnds)
			}
			if len(secondEnds) != 0 {
				t.Fatalf("unexpected peer end %+v", secondEnds)
			}
			if mode != "peer-detach" && len(secondFrames) != 2 {
				t.Fatalf("healthy peer %+v", secondFrames)
			}
			if err := s.Close(bg); err != nil {
				t.Fatal(err)
			}
			if mode == "peer-detach" && len(secondEnds) != 0 {
				t.Fatalf("detached peer ended %+v", secondEnds)
			}
			if mode == "self-detach" && len(firstEnds) != 0 {
				t.Fatalf("detached observer ended %+v", firstEnds)
			}
		})
	}
}

// Literal source259 error cause plus source-derived watch/queued controls.
// Go WithCancelCause adapts AbortController.abort(Error); retain exact identity.
func TestMainDocumentAttachmentCancellationCauseIdentity(t *testing.T) {
	for _, kind := range []string{"observe", "watch"} {
		for _, queued := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/queued=%v", kind, queued), func(t *testing.T) {
				s := gatedSession(t)
				def, err := DefineDocument(DefinitionOptions{Kind: "oracle.cancel.identity", Scope: "session", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"count": 0}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.Commit(bg, func(tx *Tx) (any, error) {
					doc, err := tx.AcquireDocument(def, 0, nil, nil)
					if err != nil {
						return nil, err
					}
					return nil, doc.ApplyOperations([]Operation{{"s", []any{"count"}, 1}})
				}); err != nil {
					t.Fatal(err)
				}
				before, found, err := s.SnapshotDefinition(bg, def, 0, nil)
				if err != nil || !found {
					t.Fatal(found, err)
				}
				ctx, cancel := context.WithCancelCause(bg)
				defer cancel(nil)
				gone := errors.New("gone")
				acquire := func() error {
					if kind == "observe" {
						guard, found, err := s.ObserveDefinition(ctx, def, 0, nil, DocumentObserver{Advance: func(context.Context, DocumentCall) error { t.Error("cancelled observation advanced"); return nil }, End: func(WatchEnd) error { t.Error("cancelled observation ended"); return nil }})
						if guard != nil || found {
							t.Error("cancelled attachment registered")
						}
						return err
					}
					watch, found, err := s.WatchDefinition(ctx, def, 0, nil)
					if watch != nil || found {
						t.Error("cancelled watch registered")
					}
					return err
				}
				var actual error
				if queued {
					if err := s.jobs.acquire(bg); err != nil {
						t.Fatal(err)
					}
					result := make(chan error, 1)
					go func() { result <- acquire() }()
					s.jobs.waitPending(1)
					cancel(gone)
					s.jobs.release()
					actual = watchReceive(t, result)
				} else {
					cancel(gone)
					actual = acquire()
				}
				if actual != gone {
					t.Fatalf("cancellation identity: got %v want exact %v", actual, gone)
				}
				s.observerMu.Lock()
				attached := len(s.documentObservers) + len(s.watches)
				s.observerMu.Unlock()
				if attached != 0 {
					t.Fatal("cancelled attachment leaked", attached)
				}
				after, found, err := s.SnapshotDefinition(ctx, def, 0, nil)
				if err != nil || !found || after != before {
					t.Fatal("warm snapshot started consulting cancellation", found, err)
				}
			})
		}
	}
}
