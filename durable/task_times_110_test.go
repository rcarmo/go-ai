package durable

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskTimes110SessionLifecycleLegacyAndReopen(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		root := mint(t, b.store)
		taskID := mint(t, b.store)
		apply(t, b.store, Write{Op: "create-conversation", Conversation: &Conversation{ID: root}}, Write{Op: "put-task", Task: &Task{ID: taskID, Conversation: root, Kind: "legacy", Status: "pending", Checkpoint: JSON{}}})
		old := snap(t, b.store).Tasks[taskID]
		wire, e := json.Marshal(old)
		if e != nil || strings.Contains(string(wire), "startedAt") || strings.Contains(string(wire), "endedAt") {
			t.Fatal(string(wire), e)
		}
		var calls atomic.Int64
		now := int64(0)
		s, e := OpenSessionWithOptions(b.store, SessionOptions{Now: func() int64 { calls.Add(1); return now }})
		if e != nil {
			t.Fatal(e)
		}
		set := func(status string) error {
			_, err := s.Commit(bg, func(tx *Tx) error {
				p, err := tx.ScanTasks(TaskQuery{Conversation: root}, 10, "")
				if err != nil {
					return err
				}
				v := p.Items[0]
				v.Status = status
				return tx.PutTask(v)
			})
			return err
		}
		if e = set("pending"); e != nil || calls.Load() != 0 {
			t.Fatal(e, calls.Load())
		}
		if e = set("running"); e != nil {
			t.Fatal(e)
		}
		v := snap(t, b.store).Tasks[taskID]
		if v.StartedAt == nil || *v.StartedAt != 0 || v.EndedAt != nil || calls.Load() != 1 {
			t.Fatal(v, calls.Load())
		}
		// Zero is present, not a missing timestamp; returned pointers are detached.
		*v.StartedAt = 777
		now = 10
		if e = set("waiting"); e != nil {
			t.Fatal(e)
		}
		if e = set("running"); e != nil {
			t.Fatal(e)
		}
		now = -5 // wall clocks can move backwards; do not invent elapsed time here.
		if e = set("done"); e != nil {
			t.Fatal(e)
		}
		v = snap(t, b.store).Tasks[taskID]
		if v.StartedAt == nil || *v.StartedAt != 0 || v.EndedAt == nil || *v.EndedAt != -5 || calls.Load() != 2 {
			t.Fatal(v, calls.Load())
		}
		canonical, e := CanonicalTask(v, DefaultLimits())
		if e != nil {
			t.Fatal(e)
		}
		*canonical.EndedAt = 55
		if *snap(t, b.store).Tasks[taskID].EndedAt != -5 {
			t.Fatal("canonical timestamp alias")
		}
		if e = s.Close(bg); e != nil {
			t.Fatal(e)
		}
		switch old := b.store.(type) {
		case *MemoryStorage:
			b.store, e = OpenMemory(MemoryOptions{Image: old.image})
		case *JournalStorage:
			b.store, e = OpenJournal(filepath.Dir(old.path), JournalOptions{})
		}
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = b.store.Close(bg) })
		v = snap(t, b.store).Tasks[taskID]
		if v.StartedAt == nil || *v.StartedAt != 0 || v.EndedAt == nil || *v.EndedAt != -5 {
			t.Fatal(v)
		}
		changed := int64(42)
		v.EndedAt = &changed
		if _, e = b.store.Apply(bg, Batch{Writes: []Write{{Op: "put-task", Task: &v}}}); e == nil {
			t.Fatal("accepted replacement of first committed lifecycle time")
		}
	})
}

func TestTaskTimes110StagedFirstAndRollback(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		root := mint(t, b.store)
		taskID := mint(t, b.store)
		apply(t, b.store, Write{Op: "create-conversation", Conversation: &Conversation{ID: root}})
		calls := int64(0)
		s, e := OpenSessionWithOptions(b.store, SessionOptions{Now: func() int64 { calls++; return calls * 100 }})
		if e != nil {
			t.Fatal(e)
		}
		defer s.Close(bg)
		sentinel := errors.New("rollback")
		_, e = s.Commit(bg, func(tx *Tx) error {
			if e := tx.PutTask(Task{ID: taskID, Conversation: root, Kind: "legacy", Status: "running", Checkpoint: JSON{}}); e != nil {
				return e
			}
			return sentinel
		})
		if !errors.Is(e, sentinel) {
			t.Fatal(e)
		}
		if _, ok := snap(t, b.store).Tasks[taskID]; ok {
			t.Fatal("rollback published stamp")
		}
		_, e = s.Commit(bg, func(tx *Tx) error {
			v := Task{ID: taskID, Conversation: root, Kind: "legacy", Status: "running", Checkpoint: JSON{}}
			if e := tx.PutTask(v); e != nil {
				return e
			}
			replacement := int64(999)
			v.StartedAt = &replacement
			v.Status = "done"
			if e := tx.PutTask(v); e != nil {
				return e
			}
			p, e := tx.ScanTasks(TaskQuery{}, 1, "")
			if e != nil {
				return e
			}
			if *p.Items[0].StartedAt != 200 || *p.Items[0].EndedAt != 300 {
				t.Fatal(p)
			}
			return nil
		})
		if e != nil {
			t.Fatal(e)
		}
		v := snap(t, b.store).Tasks[taskID]
		if *v.StartedAt != 200 || *v.EndedAt != 300 || calls != 3 {
			t.Fatal(v, calls)
		}
	})
}

func TestTaskTimes110HarnessNativeTerminal(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var calls atomic.Int64
		definition, e := DefineTask(TaskDefinitionOptions{Kind: "timed-native", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "run"}, nil }, Abort: taskAbort, Phases: map[string]TaskPhase{"run": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("ok"), nil })
		}}})
		if e != nil {
			t.Fatal(e)
		}
		h := taskTestHarnessOptions(t, b.store, Options{Now: func() int64 { return calls.Add(1) * 100 }}, definition)
		if _, e = h.Root(bg, AgentChange{}); e != nil {
			t.Fatal(e)
		}
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		deadline, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		outcome, e := h.WaitForTask(deadline, id)
		if e != nil || outcome.State.Outcome == nil || outcome.State.Outcome.Status != "completed" {
			t.Fatal(outcome, e)
		}
		v := snap(t, b.store).Tasks[id]
		if v.StartedAt == nil || *v.StartedAt != 100 || v.EndedAt == nil || *v.EndedAt != 200 || calls.Load() != 2 {
			t.Fatal(v, calls.Load())
		}
		before := calls.Load()
		if _, e = h.InspectTasks(deadline); e != nil || calls.Load() != before {
			t.Fatal("inspection clock effect", e, calls.Load())
		}
	})
}
