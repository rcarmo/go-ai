package durable

import (
	"reflect"
	"testing"
)

func v150WaitGraphTask(id ID, on []ID, owner ID, background, aborted bool, status string) Task {
	state := TaskState{Status: status, On: on, Policy: "allSettled", Checkpoint: JSON{"phase": "wait"}}
	if status == "done" {
		state = *taskDone(nil)
	}
	return Task{ID: id, Conversation: 1, Owner: owner, Kind: "graph", Status: status, Execution: &TaskExecution{Tag: nativeTaskTag, Native: &NativeTaskExecution{Version: 1, Background: background, AbortRequested: aborted, State: state}}}
}

// Source-derived controls for scheduler.ts:2524. The retained source226 test
// independently exercises rejection through real invocations, commits and SQLite.
func TestV150PersistedWaitGraphEdges(t *testing.T) {
	live := func(id ID, on []ID) Task {
		status := "running"
		if len(on) > 0 {
			status = "waiting"
		}
		return v150WaitGraphTask(id, on, 0, false, false, status)
	}
	for _, tc := range []struct {
		name  string
		on    []ID
		tasks []Task
		want  []ID
	}{
		{"opposite", []ID{2}, []Task{live(1, nil), live(2, []ID{1})}, []ID{2, 1}},
		{"three", []ID{2}, []Task{live(1, nil), live(2, []ID{3}), live(3, []ID{1})}, []ID{2, 3, 1}},
		{"acyclic", []ID{2}, []Task{live(1, nil), live(2, []ID{3}), live(3, nil)}, nil},
		{"diamond", []ID{2, 3}, []Task{live(1, nil), live(2, []ID{4}), live(3, []ID{4}), live(4, nil)}, nil},
		{"ordinary-owned", []ID{2}, []Task{live(1, nil), live(2, nil), v150WaitGraphTask(3, []ID{1}, 2, false, false, "waiting")}, []ID{2, 3, 1}},
		{"background-barrier", []ID{2}, []Task{live(1, nil), live(2, nil), v150WaitGraphTask(3, nil, 2, true, false, "running"), v150WaitGraphTask(4, []ID{1}, 3, false, false, "waiting")}, nil},
		{"terminal-bridge", []ID{2}, []Task{live(1, nil), live(2, nil), v150WaitGraphTask(3, nil, 2, false, false, "done"), v150WaitGraphTask(4, []ID{1}, 3, false, false, "waiting")}, []ID{2, 4, 1}},
		{"abort-drops-wait", []ID{2}, []Task{live(1, nil), v150WaitGraphTask(2, []ID{1}, 0, false, true, "waiting")}, nil},
		{"abort-keeps-owned", []ID{2}, []Task{live(1, nil), v150WaitGraphTask(2, nil, 0, false, true, "running"), v150WaitGraphTask(3, []ID{1}, 2, false, false, "waiting")}, []ID{2, 3, 1}},
		{"terminal-target", []ID{2}, []Task{live(1, nil), v150WaitGraphTask(2, nil, 0, false, false, "done")}, nil},
		{"already-owned-target", []ID{2}, []Task{live(1, nil), v150WaitGraphTask(2, nil, 1, false, false, "running")}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := initialState()
			for _, task := range tc.tasks {
				state.Tasks[task.ID] = task
			}
			before := len(state.Tasks)
			if got := persistedWaitCycle(state, 1, tc.on); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
			if len(state.Tasks) != before {
				t.Fatal("graph scan mutated state")
			}
		})
	}
}

// A mark removes an existing wait edge. Only the newly proposed non-aborted
// wait is cycle-checked: rechecking the marked task's old On would invent an
// edge the pinned scheduler explicitly ignores in its effective graph.
func TestV150WaitAdmissionUsesAbortOverlay(t *testing.T) {
	state := initialState()
	state.HighWater = 4
	// The overlay's tasks live in the reserved root, which its owner creates.
	state.Conversations[1] = Conversation{ID: 1}
	a := v150WaitGraphTask(2, []ID{3}, 0, false, true, "waiting")
	b := v150WaitGraphTask(3, []ID{4}, 0, false, false, "waiting")
	c := v150WaitGraphTask(4, []ID{2}, 0, false, false, "waiting")
	for _, task := range []Task{a, b, c} {
		state.Tasks[task.ID] = task
	}
	writes := []Write{{Op: "put-task", Task: &a}, {Op: "put-task", Task: &c}}
	tx := &Tx{limits: DefaultLimits(), state: state, writes: writes, stagedState: &state, stagedWrites: writes}
	if err := (&taskScheduler{}).prepareTaskWrites(tx); err != nil {
		t.Fatalf("legal abort+wait overlay refused: %v", err)
	}
}
