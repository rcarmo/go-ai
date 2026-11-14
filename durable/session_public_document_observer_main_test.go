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
// errors instead of JS throws; public values now use readonly handles.
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
			var frames []DocumentCall
			var ends []WatchEnd
			failure := errors.New("listener failed")
			obs, found, err := s.ObserveDefinition(context.Background(), def, owner, nil, DocumentObserver{
				Advance: func(_ context.Context, frame DocumentCall) error {
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
			if !found || obs == nil || immutableViewMember(t, obs.Value, "count").Number() != json.Number("1") {
				t.Fatalf("baseline: %v %v", found, obs)
			}
			before, err := store.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := sourceDocumentSet(s, def, owner, 3, memory); err != nil {
				t.Fatal(err)
			}
			if len(frames) != 1 || immutableViewMember(t, frames[0].Value, "count").Number() != json.Number("3") || !reflect.DeepEqual(frames[0].Ops, []Operation{{"s", []any{"count"}, json.Number("3")}}) {
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
			obs.Value.Copy().(map[string]any)["count"] = math.MaxInt32 // explicit detached copy must not alter later data
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

// Literal source132 retirement identity: the retired call carries the same
// readonly value exposed at attachment, followed by a whole new incarnation.
func TestMainPublicDocumentObserverRetirementIdentity(t *testing.T) {
	s, _, def, owner := sourceDocumentObserverSession(t, true)
	if err := sourceDocumentSet(s, def, owner, 2, false); err != nil {
		t.Fatal(err)
	}
	var frames []DocumentCall
	observation, found, err := s.ObserveDefinition(bg, def, owner, nil, DocumentObserver{Advance: func(_ context.Context, frame DocumentCall) error { frames = append(frames, frame); return nil }, End: func(WatchEnd) error { return nil }})
	if err != nil || !found {
		t.Fatal(found, err)
	}
	defer observation.Detach()
	if immutableViewMember(t, observation.Value, "count").Number() != json.Number("2") {
		t.Fatal("source starting value", observation.Value)
	}
	if _, err := s.Commit(bg, func(tx *Tx) (any, error) { return nil, tx.RetireDefinition(def, owner, nil) }); err != nil {
		t.Fatal(err)
	}
	absentCalls := 0
	absent, found, err := s.ObserveDefinition(bg, def, owner, nil, DocumentObserver{Advance: func(context.Context, DocumentCall) error { absentCalls++; return nil }, End: func(WatchEnd) error { return nil }})
	if err != nil || found || absent != nil {
		t.Fatal("retired address must be absent for a new attachment", found, err)
	}
	if _, err := s.Commit(bg, func(tx *Tx) (any, error) {
		doc, err := tx.AcquireDocument(def, owner, nil, nil)
		if err != nil {
			return nil, err
		}
		return nil, doc.Set(JSON{"count": 9})
	}); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || !frames[0].Retired || frames[1].Retired {
		t.Fatal("retirement/recreation calls", frames)
	}
	if frames[0].Value != observation.Value {
		t.Fatal("source132 retirement copied the initial document instead of preserving identity")
	}
	if frames[0].Kind != "value" || len(frames[0].Ops) != 0 || frames[1].Kind != "value" || immutableViewMember(t, frames[1].Value, "count").Number() != json.Number("9") || !reflect.DeepEqual(frames[1].Ops, []Operation{{"r", map[string]any{"count": json.Number("9")}}}) || absentCalls != 0 {
		t.Fatal("source132 exact calls", frames, absentCalls)
	}
}

// Source-derived persistent ownership control: concurrent observers of one
// address share each adopted value and retain untouched child nodes.
func TestMainPublicDocumentObserverSharedReadonlyRevisions(t *testing.T) {
	s, _, def, owner := sourceDocumentObserverSession(t, false)
	if _, err := s.Commit(bg, func(tx *Tx) (any, error) {
		doc, err := tx.AcquireDocument(def, owner, nil, nil)
		if err != nil {
			return nil, err
		}
		return nil, doc.Set(JSON{"count": 1, "keep": JSON{"text": "original"}})
	}); err != nil {
		t.Fatal(err)
	}
	var leftFrames, rightFrames []DocumentCall
	attach := func(frames *[]DocumentCall) *DocumentObservation {
		observation, found, err := s.ObserveDefinition(bg, def, owner, nil, DocumentObserver{Advance: func(_ context.Context, call DocumentCall) error { *frames = append(*frames, call); return nil }, End: func(WatchEnd) error { return nil }})
		if err != nil || !found {
			t.Fatal(found, err)
		}
		return observation
	}
	left, right := attach(&leftFrames), attach(&rightFrames)
	defer left.Detach()
	defer right.Detach()
	if left.Value != right.Value {
		t.Fatal("concurrent observations copied their baseline")
	}
	before := immutableViewMember(t, left.Value, "keep")
	if _, err := s.Commit(bg, func(tx *Tx) (any, error) {
		doc, err := tx.AcquireDocument(def, owner, nil, nil)
		if err != nil {
			return nil, err
		}
		return nil, doc.ApplyOperations([]Operation{{"s", []any{"count"}, 2}})
	}); err != nil {
		t.Fatal(err)
	}
	if len(leftFrames) != 1 || len(rightFrames) != 1 || leftFrames[0].Value != rightFrames[0].Value {
		t.Fatal("observers do not share their adopted revision")
	}
	if immutableViewMember(t, leftFrames[0].Value, "keep") != before || leftFrames[0].Value == left.Value {
		t.Fatal("persistent document branch identity mismatch")
	}
	lateFrames := []DocumentCall{}
	late := attach(&lateFrames)
	defer late.Detach()
	if late.Value != leftFrames[0].Value {
		t.Fatal("late observation did not reuse the adopted readonly revision")
	}
	detached := leftFrames[0].Value.Copy().(map[string]any)
	detached["keep"].(map[string]any)["text"] = "mutated"
	if immutableViewMember(t, before, "text").String() != "original" {
		t.Fatal("copy exposed shared mutation authority")
	}
	if reflect.ValueOf(left.Value).Kind() == reflect.Pointer {
		t.Fatal("readonly document exposes overwrite authority")
	}
}
