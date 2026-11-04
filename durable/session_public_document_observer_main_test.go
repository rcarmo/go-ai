package durable

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
)

// Source session-observers.test.ts:77,87,105 and 215. Go callbacks return
// errors and the acquisition value is detached; JS identity is not tested.
func TestMainPublicDocumentObserverSource(t *testing.T) {
	for _, mode := range []string{"absent", "stored", "memory", "listener-error"} {
		t.Run(mode, func(t *testing.T) {
			memory := mode == "memory"
			s, store, def, owner := sourceDocumentObserverSession(t, memory)
			if mode != "absent" {
				if err := sourceDocumentSet(s, def, owner, 1, false); err != nil {
					t.Fatal(err)
				}
			}
			var frames []WatchFrame
			var ends []WatchEnd
			failure := errors.New("listener failed")
			obs, found, err := s.ObserveDefinition(context.Background(), def, owner, nil, DocumentObserver{
				Advance: func(_ context.Context, frame WatchFrame) error {
					frames = append(frames, frame)
					if mode == "listener-error" {
						return failure
					}
					return nil
				},
				End: func(end WatchEnd) error { ends = append(ends, end); return nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "absent" {
				if found || obs != nil {
					t.Fatalf("absent: %v %v", found, obs)
				}
				return
			}
			if !found || obs == nil || obs.Value["count"] != json.Number("1") {
				t.Fatalf("baseline: %v %v", found, obs)
			}
			before, err := store.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := sourceDocumentSet(s, def, owner, 3, memory); err != nil {
				t.Fatal(err)
			}
			if len(frames) != 1 || frames[0].Value["count"] != json.Number("3") || !reflect.DeepEqual(frames[0].Ops, []Operation{{"s", []any{"count"}, json.Number("3")}}) {
				t.Fatalf("frames=%+v", frames)
			}
			if mode == "listener-error" && (len(ends) != 1 || ends[0].Reason != "listener_error" || !errors.Is(ends[0].Error, failure)) {
				t.Fatalf("ends=%+v", ends)
			}
			if memory {
				after, err := store.Snapshot(context.Background())
				if err != nil || after.Seq != before.Seq {
					t.Fatalf("memory stored: %d %d %v", before.Seq, after.Seq, err)
				}
				if err := s.UnloadDocuments(context.Background()); err != nil {
					t.Fatal(err)
				}
				if len(frames) != 1 {
					t.Fatalf("duplicate on flush: %v", frames)
				}
			}
			obs.Value["count"] = math.MaxInt32 // detached baseline must not alter later data
			obs.Detach()
			obs.Detach()
			if err := sourceDocumentSet(s, def, owner, 4, false); err != nil {
				t.Fatal(err)
			}
			if len(frames) != 1 {
				t.Fatalf("advance after detach: %v", frames)
			}
		})
	}
}
