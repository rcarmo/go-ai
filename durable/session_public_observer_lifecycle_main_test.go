package durable

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

// Source session-observers.test.ts:132, 469, 496 and 520. Values are detached
// Go maps and returned errors adapt source throws; JS identity is not tested.
func TestMainPublicDocumentObserverRetirementAndGuards(t *testing.T) {
	for _, mode := range []string{"follow", "self-detach", "peer-detach", "error"} {
		t.Run(mode, func(t *testing.T) {
			s, _, def, owner := sourceDocumentObserverSession(t, false)
			if err := sourceDocumentSet(s, def, owner, 1, false); err != nil {
				t.Fatal(err)
			}
			var first, second *DocumentObservation
			var calls []string
			var firstFrames, secondFrames []WatchFrame
			var firstEnds, secondEnds []WatchEnd
			failure := errors.New("retirement callback failed")
			first, found, err := s.ObserveDefinition(bg, def, owner, nil, DocumentObserver{
				Advance: func(_ context.Context, frame WatchFrame) error {
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
				Advance: func(_ context.Context, frame WatchFrame) error {
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
			_, err = s.Commit(bg, func(tx *Tx) error {
				if err := tx.RetireDefinition(def, owner, nil); err != nil {
					return err
				}
				doc, err := tx.AcquireDocument(def, owner, nil, nil)
				if err != nil {
					return err
				}
				return doc.ApplyOperations([]Operation{{"s", []any{"count"}, 2}})
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
			if len(firstFrames) == 0 || !firstFrames[0].Retired || firstFrames[0].Value["count"] != json.Number("1") || len(firstFrames[0].Ops) != 0 {
				t.Fatalf("retirement %+v", firstFrames)
			}
			if mode == "follow" || mode == "peer-detach" {
				if len(firstFrames) != 2 || firstFrames[1].Retired || firstFrames[1].Value["count"] != json.Number("2") || len(firstFrames[1].Ops) != 1 || firstFrames[1].Ops[0][0] != "r" {
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
