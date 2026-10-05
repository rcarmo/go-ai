package durable

import (
	"context"
	"encoding/json"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
)

func foundationTask(id ID, input any) Task {
	return Task{ID: id, Conversation: 1, Kind: "task.foundation", Status: "pending", Checkpoint: JSON{}, Execution: &TaskExecution{Tag: nativeTaskTag, Native: &NativeTaskExecution{Version: 1, Input: &TaskValue{Present: true, Value: input}, State: TaskState{Status: "pending", Checkpoint: JSON{"phase": "work"}}}}}
}

func TestTaskDefinitionConstructorOwnsPhaseMapAndInput(t *testing.T) {
	phase := func(context.Context, TaskRecord, *TaskRuntime) error { return nil }
	phases := map[string]TaskPhase{"work": phase}
	var retained JSON
	definition, err := DefineTask(TaskDefinitionOptions{Kind: "task.definition", Version: 1, Initial: func(input any) (JSON, error) {
		input.(map[string]any)["input"] = "callback"
		retained = JSON{"phase": "work", "nested": JSON{"value": "initial"}}
		return retained, nil
	}, Phases: phases, Abort: phase})
	if err != nil {
		t.Fatal(err)
	}
	delete(phases, "work")
	input := JSON{"input": "original"}
	execution, err := definition.initial(input, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	input["input"] = "caller"
	retained["nested"].(JSON)["value"] = "retained"
	if execution.Input.Value.(map[string]any)["input"] != "original" || execution.State.Checkpoint["nested"].(map[string]any)["value"] != "initial" {
		t.Fatal("definition callback/caller placement escaped")
	}
	for _, mutate := range []func(*TaskDefinitionOptions){
		func(o *TaskDefinitionOptions) { o.Kind = "pi.generation" },
		func(o *TaskDefinitionOptions) { o.Version = 0 },
		func(o *TaskDefinitionOptions) { o.Initial = nil },
		func(o *TaskDefinitionOptions) { o.Abort = nil },
		func(o *TaskDefinitionOptions) { o.Phases = map[string]TaskPhase{} },
		func(o *TaskDefinitionOptions) { o.Phases = map[string]TaskPhase{"work": nil} },
	} {
		options := definition.options
		mutate(&options)
		if _, err := DefineTask(options); err == nil {
			t.Fatal("invalid definition accepted")
		}
	}
}

func TestTaskDefinitionRegistryPublicationIdentityAndDetachedSnapshot(t *testing.T) {
	phase := func(context.Context, TaskRecord, *TaskRuntime) error { return nil }
	makeDefinition := func(version uint64) *TaskDefinition {
		definition, err := DefineTask(TaskDefinitionOptions{Kind: "task.registry", Version: version, Initial: func(any) (JSON, error) { return JSON{"phase": "work"}, nil }, Phases: map[string]TaskPhase{"work": phase}, Abort: phase})
		if err != nil {
			t.Fatal(err)
		}
		return definition
	}
	registry := NewRegistry()
	var wakes int
	stop, err := registry.subscribeTasks(func() { // Snapshot proves notifications are outside the registry lock.
		if _, err := registry.taskSnapshot(DefaultLimits()); err != nil {
			t.Error(err)
		}
		wakes++
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if _, err := registry.RegisterTask(&TaskDefinition{}); err == nil || wakes != 0 {
		t.Fatal("zero token published/woke registry", err, wakes)
	}
	if _, err := (&TaskDefinition{}).initial(nil, DefaultLimits()); err == nil {
		t.Fatal("zero task token invoked initializer")
	}
	first := makeDefinition(1)
	disposeFirst, err := registry.RegisterTask(first)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := registry.taskSnapshot(DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	disposeSame, err := registry.RegisterTask(first)
	if err != nil {
		t.Fatal(err)
	}
	disposeFirst()
	current, err := registry.taskSnapshot(DefaultLimits())
	if err != nil || current.Task(first.Kind()) != first {
		t.Fatal("old disposal removed same-token new registration", err)
	}
	second := makeDefinition(2)
	disposeSecond, err := registry.RegisterTask(second)
	if err != nil {
		t.Fatal(err)
	}
	disposeSame()
	current, err = registry.taskSnapshot(DefaultLimits())
	if err != nil || current.Task(first.Kind()) != second || snapshot.Task(first.Kind()) != first {
		t.Fatal("snapshot/replacement identity", err)
	}
	disposeSecond()
	disposeSecond()
	current, err = registry.taskSnapshot(DefaultLimits())
	if err != nil || current.Task(first.Kind()) != nil || wakes != 4 {
		t.Fatal("registry wake/dispose", wakes, err)
	}
}

func TestTaskDefinitionCodecStrictValuesAndUnions(t *testing.T) {
	l := DefaultLimits()
	for name, value := range map[string]any{"null": nil, "bool": true, "string": "input", "number": json.Number("1.25"), "array": []any{nil, "one", JSON{"key": "value"}}, "object": JSON{"toString": nil, "nested": []any{1}}} {
		t.Run(name, func(t *testing.T) {
			task := foundationTask(2, value)
			task.Execution.Native.Memos = map[string]*TaskValue{"toString": {Present: true, Value: value}}
			owned, err := copyTask(task, l)
			if err != nil || !equalTaskValue(task.Execution.Native.Input, owned.Execution.Native.Input, l) {
				t.Fatal("strict input roundtrip", err)
			}
			owned.Execution.Native.State = TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: value}}}
			owned.Execution.Native.Memos = nil
			owned.Status = "done"
			terminal, err := copyTask(owned, l)
			if err != nil || terminal.Execution.Native.State.Outcome.Result == nil || !terminal.Execution.Native.State.Outcome.Result.Present || !equalTaskValue(owned.Execution.Native.State.Outcome.Result, terminal.Execution.Native.State.Outcome.Result, l) {
				t.Fatal("strict result roundtrip", err)
			}
		})
	}
	for name, mutate := range map[string]func(*Task){
		"wrong-tag":                  func(t *Task) { t.Execution.Tag = "future-v2" },
		"two-variants":               func(t *Task) { t.Execution.Builtin = &BuiltinTaskExecution{} },
		"nil-input":                  func(t *Task) { t.Execution.Native.Input = nil },
		"absent-value":               func(t *Task) { t.Execution.Native.Input.Present = false },
		"function-input":             func(t *Task) { t.Execution.Native.Input.Value = func() {} },
		"nil-memo":                   func(t *Task) { t.Execution.Native.Memos = map[string]*TaskValue{"nil": nil} },
		"native-physical-checkpoint": func(t *Task) { t.Checkpoint = JSON{"phase": "other"} },
		"nil-physical-checkpoint":    func(t *Task) { t.Checkpoint = nil },
		"live-with-outcome":          func(t *Task) { t.Execution.Native.State.Outcome = &TaskOutcome{Status: "aborted"} },
		"nonwaiting-on":              func(t *Task) { t.Execution.Native.State.On = []ID{3} },
		"wrong-raw-status":           func(t *Task) { t.Status = "done" },
		"unknown-phase":              func(t *Task) { t.Execution.Native.State.Checkpoint["phase"] = 1 },
		"background-child":           func(t *Task) { t.Owner = 3; t.Execution.Native.Background = true },
		"terminal-with-checkpoint": func(t *Task) {
			t.Status = "done"
			t.Execution.Native.State.Status = "terminal"
			t.Execution.Native.State.Outcome = &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: nil}}
		},
		"invalid-wait-id": func(t *Task) {
			t.Status = "waiting"
			t.Execution.Native.State.Status = "waiting"
			t.Execution.Native.State.Policy = "allSettled"
			t.Execution.Native.State.On = []ID{0, 3}
		},
	} {
		t.Run(name, func(t *testing.T) {
			task := foundationTask(2, nil)
			mutate(&task)
			if _, err := copyTask(task, l); err == nil {
				t.Fatal("invalid task union accepted")
			}
		})
	}
	cycle := JSON{}
	cycle["self"] = cycle
	task := foundationTask(2, cycle)
	if _, err := copyTask(task, l); err == nil {
		t.Fatal("task JSON cycle accepted")
	}
}

func TestTaskDefinitionFoundationRawStoreSnapshotImageAndTxDetach(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		id, err := b.store.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		shared := JSON{"label": "owned"}
		task := foundationTask(id, shared)
		task.Execution.Native.Memos = map[string]*TaskValue{"winner": {Present: true, Value: shared}}
		if _, err = b.store.Apply(bg, Batch{Writes: []Write{{Op: "put-task", Task: &task}}}); err != nil {
			t.Fatal(err)
		}
		shared["label"] = "caller"
		first, err := b.store.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		read := first.Tasks[id]
		read.Execution.Native.Input.Value.(map[string]any)["label"] = "reader"
		if read.Execution.Native.Memos["winner"].Value.(map[string]any)["label"] != "owned" {
			t.Fatal("input/memo placements shared")
		}
		page, err := b.store.Tasks(bg, Query{Limit: 1})
		if err != nil || len(page) != 1 || page[0].Execution.Native.Input.Value.(map[string]any)["label"] != "owned" {
			t.Fatal("raw Tasks detached", err)
		}
		page[0].Execution.Native.Memos["winner"].Value.(map[string]any)["label"] = "page"
		session, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close(bg)
		_, err = session.Commit(bg, func(tx *Tx) error {
			v := tx.state.Tasks[id]
			v.Execution.Native.State.Checkpoint["count"] = 1
			// Raw public Tx.PutTask cannot replace native execution authority.
			if err := tx.PutTask(v); err == nil {
				return errors.New("raw execution replacement accepted")
			}
			// Private adapter staging still performs the full copy boundary.
			if err := tx.stage(Write{Op: "put-task", Task: &v}); err != nil {
				return err
			}
			v.Execution.Native.State.Checkpoint["count"] = 9
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		state, err := session.Snapshot(bg)
		if err != nil || state.Tasks[id].Execution.Native.Input.Value.(map[string]any)["label"] != "owned" || state.Tasks[id].Execution.Native.Memos["winner"].Value.(map[string]any)["label"] != "owned" || !equalJSONValue(state.Tasks[id].Execution.Native.State.Checkpoint["count"], json.Number("1")) {
			t.Fatal("Tx/snapshot envelope detached", err)
		}
	})
	image := &MemoryImage{}
	store, err := OpenMemory(MemoryOptions{Image: image})
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.MintID(bg)
	if err != nil {
		t.Fatal(err)
	}
	task := foundationTask(id, []any{JSON{"label": "image"}})
	if _, err = store.Apply(bg, Batch{Writes: []Write{{Op: "put-task", Task: &task}}}); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(bg); err != nil {
		t.Fatal(err)
	}
	task.Execution.Native.Input.Value.([]any)[0].(JSON)["label"] = "after-close"
	reopened, err := OpenMemory(MemoryOptions{Image: image})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(bg)
	state, err := reopened.Snapshot(bg)
	if err != nil || state.Tasks[id].Execution.Native.Input.Value.([]any)[0].(map[string]any)["label"] != "image" {
		t.Fatal("image envelope detached", err)
	}
}

func TestTaskDefinitionFoundationCombinedForwardOwnershipAndHeldImmutable(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		childID, _ := b.store.MintID(bg)
		ownerID, _ := b.store.MintID(bg)
		child, owner := foundationTask(childID, nil), foundationTask(ownerID, nil)
		child.Owner = ownerID
		if _, err := b.store.Apply(bg, Batch{Writes: []Write{{Op: "put-task", Task: &child}, {Op: "put-task", Task: &owner}}}); err != nil {
			t.Fatal("valid forward owner", err)
		}
		conversationID, _ := b.store.MintID(bg)
		cycle := owner
		cycle.Conversation = conversationID
		before, _ := b.store.Snapshot(bg)
		if _, err := b.store.Apply(bg, Batch{Writes: []Write{{Op: "create-conversation", Conversation: &Conversation{ID: conversationID, Owner: ownerID}}, {Op: "put-task", Task: &cycle}}}); err == nil {
			t.Fatal("combined ownership/move accepted")
		}
		after, _ := b.store.Snapshot(bg)
		if before.Seq != after.Seq {
			t.Fatal("failed ownership admission changed sequence")
		}
		// Direct final ownership cycle in a fresh same-batch forward graph.
		newTaskID, _ := b.store.MintID(bg)
		newConvID, _ := b.store.MintID(bg)
		newTask := foundationTask(newTaskID, nil)
		newTask.Conversation = newConvID
		if _, err := b.store.Apply(bg, Batch{Writes: []Write{{Op: "put-task", Task: &newTask}, {Op: "create-conversation", Conversation: &Conversation{ID: newConvID, Owner: newTaskID}}}}); err == nil {
			t.Fatal("combined cycle accepted")
		}
		held := owner
		held.Status = "completing"
		held.Execution = &TaskExecution{Tag: nativeTaskTag, Native: &NativeTaskExecution{Version: 1, Input: &TaskValue{Present: true, Value: nil}, State: TaskState{Status: "completing", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: "held"}}}}}
		if _, err := b.store.Apply(bg, Batch{Writes: []Write{{Op: "put-task", Task: &held}}}); err != nil {
			t.Fatal(err)
		}
		changed, err := copyTask(held, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		changed.Execution.Native.State.Outcome.Result.Value = "changed"
		if _, err := b.store.Apply(bg, Batch{Writes: []Write{{Op: "put-task", Task: &changed}}}); err == nil {
			t.Fatal("held result replaced")
		}
	})
}

func TestTaskDefinitionPublicCreateTaskBoundOwnershipAndSealed(t *testing.T) {
	phase := func(context.Context, TaskRecord, *TaskRuntime) error { return nil }
	definition, err := DefineTask(TaskDefinitionOptions{Kind: "task.creation", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "work"}, nil }, Phases: map[string]TaskPhase{"work": phase}, Abort: phase})
	if err != nil {
		t.Fatal(err)
	}
	backends(t, func(t *testing.T, b backend) {
		session, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close(bg)
		beforeZero, _ := session.Snapshot(bg)
		_, err = session.Commit(bg, func(tx *Tx) error {
			_, err := tx.CreateTask(&TaskDefinition{}, nil, TaskOptions{Conversation: 1, Ownership: TaskOwnership{Kind: "conversation"}})
			return err
		})
		afterZero, _ := session.Snapshot(bg)
		if err == nil || beforeZero.Seq != afterZero.Seq || beforeZero.HighWater != afterZero.HighWater {
			t.Fatal("zero token minted/wrote", err)
		}
		var parent, child ID
		var escaped *Tx
		_, err = session.Commit(bg, func(tx *Tx) error {
			escaped = tx
			var err error
			parent, err = tx.CreateTask(definition, nil, TaskOptions{Conversation: 1, Ownership: TaskOwnership{Kind: "conversation"}})
			if err != nil {
				return err
			}
			child, err = tx.CreateTask(definition, []any{nil, "child"}, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: parent}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		state, err := session.Snapshot(bg)
		if err != nil || state.Tasks[child].Owner != parent || state.Tasks[child].Conversation != 1 {
			t.Fatal("public owned creation", err)
		}
		if _, err = escaped.CreateTask(definition, nil, TaskOptions{Conversation: 1, Ownership: TaskOwnership{Kind: "conversation"}}); !errors.Is(err, ErrSealed) {
			t.Fatal("escaped task creation", err)
		}
		before := state.Seq
		_, err = session.Commit(bg, func(tx *Tx) error {
			_, err := tx.CreateTask(definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: parent}, Background: true})
			return err
		})
		if err == nil {
			t.Fatal("background child accepted")
		}
		state, _ = session.Snapshot(bg)
		if state.Seq != before {
			t.Fatal("invalid task creation admitted")
		}
	})
}

func TestTaskDefinitionOptionalFailedAbortedResultAndRawForgery(t *testing.T) {
	for _, outcome := range []TaskOutcome{
		{Status: "failed", Error: &TaskOutcomeError{Message: "domain"}, Result: &TaskValue{Present: true, Value: []any{nil, "failed"}}},
		{Status: "aborted", Reason: "stop", Result: &TaskValue{Present: true, Value: nil}},
	} {
		task := foundationTask(2, nil)
		task.Execution.Native.State = TaskState{Status: "terminal", Outcome: &outcome}
		task.Status = outcomeRawStatus(&outcome)
		if _, err := copyTask(task, DefaultLimits()); err != nil {
			t.Fatal("valid optional result rejected", err)
		}
	}
	backends(t, func(t *testing.T, b backend) {
		session, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close(bg)
		_, err = session.Commit(bg, func(tx *Tx) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			forged := foundationTask(id, nil)
			forged.Status = "failed"
			forged.Execution.Native.State = TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "orphaned", Reason: "forged"}}
			return tx.PutTask(forged)
		})
		if err == nil {
			t.Fatal("public Tx forged private scheduler outcome")
		}
		state, err := session.Snapshot(bg)
		if err != nil || len(state.Tasks) != 0 {
			t.Fatal("forged authority persisted", err)
		}
	})
}

func TestTaskDefinitionFoundationBudgetsAndNilRemainStrict(t *testing.T) {
	l := DefaultLimits()
	l.MaxRecordBytes = 512
	l.MaxDocumentBytes = 256
	task := foundationTask(2, make([]any, 400))
	if _, err := copyTask(task, l); err == nil {
		t.Fatal("complete task budget ignored")
	}
	legacy := Task{ID: 2, Conversation: 1, Kind: "legacy.raw", Status: "completing", Checkpoint: nil}
	if _, err := copyTask(legacy, l); err == nil {
		t.Fatal("nil legacy checkpoint relaxed")
	}
	_, err := copyTaskValue(&TaskValue{Value: nil}, DefaultLimits())
	if err == nil {
		t.Fatal("null confused with absence")
	}
	if !errors.As(err, new(*StorageRejected)) {
		t.Fatal(err)
	}
}

func TestTaskDefinitionRaisedPersistedLimitsHeldEquality(t *testing.T) {
	for _, name := range []string{"memory", "journal"} {
		t.Run(name, func(t *testing.T) {
			l := DefaultLimits()
			l.MaxStringBytes = 512 << 10
			l.MaxRecordBytes = 1 << 20     // independent 300KiB input/result placements
			l.MaxDocumentBytes = 512 << 10 // builtin checkpoint DTO uses this policy
			l.MaxDepth = 64
			var store Storage
			var err error
			if name == "memory" {
				store, err = OpenMemory(MemoryOptions{Limits: &l})
			} else {
				store, err = OpenJournal(t.TempDir()+"/private", JournalOptions{Limits: &l})
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close(bg) })
			large := strings.Repeat("x", 300<<10)
			id := mint(t, store)
			held := foundationTask(id, large)
			held.Status = "completing"
			held.Execution.Native.State = TaskState{Status: "completing", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: large}}}
			apply(t, store, Write{Op: "put-task", Task: &held})
			marked := markTask(snap(t, store).Tasks[id])
			apply(t, store, Write{Op: "put-task", Task: &marked})
			changed, err := copyTask(marked, l)
			if err != nil {
				t.Fatal(err)
			}
			changed.Execution.Native.State.Outcome.Result.Value = large + "y"
			_, err = store.Apply(bg, Batch{Writes: []Write{{Op: "put-task", Task: &changed}}})
			rejected(t, err)
			changed, err = copyTask(marked, l)
			if err != nil {
				t.Fatal(err)
			}
			changed.Execution.Native.Input.Value = large + "y"
			_, err = store.Apply(bg, Batch{Writes: []Write{{Op: "put-task", Task: &changed}}})
			rejected(t, err)
			final, err := copyTask(marked, l)
			if err != nil {
				t.Fatal(err)
			}
			final.Status = "done"
			final.Execution.Native.State.Status = "terminal"
			apply(t, store, Write{Op: "put-task", Task: &final})
			// Built-in retained checkpoints also obey this store's policy, not
			// DefaultLimits. Scheduler-generation permits no arbitrary mutation.
			parentID, subID := mint(t, store), mint(t, store)
			cp := generationCheckpoint{Phase: "intent", Submission: subID, Input: large}
			checkpoint, err := dtoObject(cp, l)
			if err != nil {
				t.Fatal(err)
			}
			builtin := Task{ID: parentID, Conversation: 1, Kind: "pi.generation", Status: "completing", Checkpoint: checkpoint, Execution: &TaskExecution{Tag: builtinTaskTag, Builtin: &BuiltinTaskExecution{Hold: &BuiltinTaskHold{Stage: "held", Action: "scheduler-generation", FinalStatus: "failed", Conversation: 1, Submission: subID, Outcome: TaskOutcome{Status: "faulted", Error: &TaskOutcomeError{Message: "bounded"}}}}}}
			apply(t, store, Write{Op: "put-submission", Submission: &Submission{ID: subID, Conversation: 1, Type: "follow-up", Status: "pending", Value: JSON{}}}, Write{Op: "put-task", Task: &builtin})
			marked = markTask(snap(t, store).Tasks[parentID])
			apply(t, store, Write{Op: "put-task", Task: &marked})
			changed, err = copyTask(marked, l)
			if err != nil {
				t.Fatal(err)
			}
			changed.Checkpoint["input"] = large + "changed"
			_, err = store.Apply(bg, Batch{Writes: []Write{{Op: "put-task", Task: &changed}}})
			rejected(t, err)
			final, err = copyTask(marked, l)
			if err != nil {
				t.Fatal(err)
			}
			final.Status = "failed"
			final.Execution.Builtin.Hold.Stage = "final"
			apply(t, store, Write{Op: "put-task", Task: &final})
		})
	}
}

func TestTaskDefinitionAbortMarkMonotonicAcrossLiveReplacementAndSettlement(t *testing.T) {
	for _, kind := range []string{"native", "builtin"} {
		for _, status := range []string{"pending", "running", "waiting"} {
			for _, mark := range []string{"metadata", "checkpoint", "both"} {
				if kind == "native" && mark != "metadata" {
					continue
				}
				t.Run(kind+"/"+status+"/"+mark, func(t *testing.T) {
					backends(t, func(t *testing.T, b backend) {
						id := mint(t, b.store)
						var subID ID
						if kind == "builtin" {
							subID = mint(t, b.store)
							apply(t, b.store, Write{Op: "put-submission", Submission: &Submission{ID: subID, Conversation: 1, Type: "follow-up", Status: "pending", Value: JSON{}}})
						}
						var err error
						original := foundationTask(id, JSON{"input": "kept"})
						original.Status = status
						original.Execution.Native.State.Status = status
						if kind == "builtin" {
							original.Kind = "pi.generation"
							original.Checkpoint, err = dtoObject(generationCheckpoint{Phase: "intent", Submission: subID, Input: "kept"}, DefaultLimits())
							if err != nil {
								t.Fatal(err)
							}
							original.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: &BuiltinTaskExecution{}}
						} else if status == "waiting" {
							original.Execution.Native.State.Policy = "allSettled" // Empty wait is valid.
						}
						apply(t, b.store, Write{Op: "put-task", Task: &original})
						// Synthetic scheduler-generation Hold identity is separate from
						// receipt production. No scheduler-tool creator is inferred.
						decideBuiltin := func(task *Task, stage string) {
							task.Status = "completing"
							if stage == "final" {
								task.Status = "failed"
							}
							task.Execution.Builtin.Hold = &BuiltinTaskHold{Stage: stage, Action: "scheduler-generation", Outcome: TaskOutcome{Status: "faulted", Error: &TaskOutcomeError{Message: "synthetic mark invariant"}}, FinalStatus: "failed", Conversation: 1, Submission: subID}
						}
						marked, err := copyTask(original, DefaultLimits())
						if err != nil {
							t.Fatal(err)
						}
						if kind == "native" {
							marked.Execution.Native.AbortRequested = true
						} else {
							marked.Execution.Builtin.AbortRequested = mark != "checkpoint"
							if mark != "metadata" {
								marked.Checkpoint["abort"] = true
							}
						}
						apply(t, b.store, Write{Op: "put-task", Task: &marked}) // Adding mark is legal.
						apply(t, b.store, Write{Op: "put-task", Task: &marked}) // Same confirmed mark is legal.
						for _, transition := range []string{"live", "held", "final"} {
							removals := []string{"clear-all"}
							if kind == "builtin" && mark == "both" {
								removals = append(removals, "clear-metadata-only", "clear-checkpoint-only", "drop-checkpoint-only")
							}
							for _, removal := range removals {
								t.Run(transition+"/"+removal, func(t *testing.T) {
									before := snap(t, b.store)
									candidate, err := copyTask(before.Tasks[id], DefaultLimits())
									if err != nil {
										t.Fatal(err)
									}
									if kind == "native" {
										candidate.Execution.Native.AbortRequested = false
										if transition != "live" {
											candidate.Execution.Native.Memos = nil
											candidate.Execution.Native.State = TaskState{Status: "completing", Outcome: &TaskOutcome{Status: "aborted", Reason: "mark"}}
											candidate.Status = "completing"
											if transition == "final" {
												candidate.Status = "aborted"
												candidate.Execution.Native.State.Status = "terminal"
											}
										}
									} else {
										switch removal {
										case "clear-all":
											candidate.Execution.Builtin.AbortRequested = false
											delete(candidate.Checkpoint, "abort")
										case "clear-metadata-only":
											candidate.Execution.Builtin.AbortRequested = false
										case "clear-checkpoint-only":
											candidate.Checkpoint["abort"] = false
										case "drop-checkpoint-only":
											delete(candidate.Checkpoint, "abort")
										}
										if transition != "live" {
											stage := "held"
											if transition == "final" {
												stage = "final"
											}
											decideBuiltin(&candidate, stage)
										}
									}
									// Validate independently before Apply: malformed Hold/DTO
									// or references must not masquerade as mark rejection.
									if _, err := copyTask(candidate, DefaultLimits()); err != nil {
										t.Fatal("invalid negative fixture", err)
									}
									probe := candidateTables(before)
									probe.Tasks[id] = candidate
									if err := validateTaskReferences(probe, candidate, DefaultLimits()); err != nil {
										t.Fatal("invalid negative references", err)
									}
									_, err = b.store.Apply(bg, Batch{Writes: []Write{{Op: "put-task", Task: &candidate}}})
									var markRejection *StorageRejected
									if !errors.As(err, &markRejection) || !strings.Contains(markRejection.Reason, "abort mark cannot be cleared") {
										t.Fatal("replacement failed for unrelated reason", err)
									}
									after := snap(t, b.store)
									if before.Seq != after.Seq || after.Tasks[id].Status != status || !taskAborted(after.Tasks[id]) {
										t.Fatal("mark rejection changed adopted state")
									}
									if !equalTaskValue(&TaskValue{Present: true, Value: before.Tasks[id].Checkpoint}, &TaskValue{Present: true, Value: after.Tasks[id].Checkpoint}, DefaultLimits()) {
										t.Fatal("rejected mark changed checkpoint")
									}
									if kind == "native" && !equalTaskValue(before.Tasks[id].Execution.Native.Input, after.Tasks[id].Execution.Native.Input, DefaultLimits()) {
										t.Fatal("rejected mark changed input")
									}
								})
							}
						}
						// Mark-preserving live -> Hold -> terminal settlement is legal.
						preserved, err := copyTask(snap(t, b.store).Tasks[id], DefaultLimits())
						if err != nil {
							t.Fatal(err)
						}
						if kind == "native" {
							preserved.Status = "completing"
							preserved.Execution.Native.Memos = nil
							preserved.Execution.Native.State = TaskState{Status: "completing", Outcome: &TaskOutcome{Status: "aborted", Reason: "mark"}}
							apply(t, b.store, Write{Op: "put-task", Task: &preserved})
							preserved.Status = "aborted"
							preserved.Execution.Native.State.Status = "terminal"
						} else {
							decideBuiltin(&preserved, "held")
							apply(t, b.store, Write{Op: "put-task", Task: &preserved})
							decideBuiltin(&preserved, "final")
						}
						apply(t, b.store, Write{Op: "put-task", Task: &preserved})
						if !taskAborted(snap(t, b.store).Tasks[id]) {
							t.Fatal("final logical mark missing")
						}
					})
				})
			}
		}
	}
}

func TestTaskDefinitionLegacyMarkFirstBuiltinMetadataAttachment(t *testing.T) {
	for _, initiallyMarked := range []bool{false, true} {
		backends(t, func(t *testing.T, b backend) {
			id := mint(t, b.store)
			raw := Task{ID: id, Conversation: 1, Kind: "pi.generation", Status: "pending", Checkpoint: JSON{"phase": "queued", "input": "legacy"}}
			if initiallyMarked {
				raw.Checkpoint["abort"] = true
			}
			apply(t, b.store, Write{Op: "put-task", Task: &raw})
			attached, err := copyTask(raw, DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			attached.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: &BuiltinTaskExecution{}}
			if initiallyMarked {
				delete(attached.Checkpoint, "abort")
				before := snap(t, b.store)
				_, err = b.store.Apply(bg, Batch{Writes: []Write{{Op: "put-task", Task: &attached}}})
				rejected(t, err)
				if after := snap(t, b.store); after.Seq != before.Seq || !taskAborted(after.Tasks[id]) {
					t.Fatal("firstmetadata erased legacy confirmed mark")
				}
				// First attachment may preserve the canonical OR in metadata alone.
				attached.Execution.Builtin.AbortRequested = true
			}
			apply(t, b.store, Write{Op: "put-task", Task: &attached})
			if taskAborted(snap(t, b.store).Tasks[id]) != initiallyMarked {
				t.Fatal("firstmetadata invented/erased mark")
			}
			if !initiallyMarked {
				attached = markTask(snap(t, b.store).Tasks[id])
				apply(t, b.store, Write{Op: "put-task", Task: &attached})
			}
		})
	}
	backends(t, func(t *testing.T, b backend) {
		id := mint(t, b.store)
		raw := Task{ID: id, Conversation: 1, Kind: "legacy.rawmark", Status: "pending", Checkpoint: JSON{"abort": true}}
		apply(t, b.store, Write{Op: "put-task", Task: &raw})
		raw.Checkpoint = JSON{}
		apply(t, b.store, Write{Op: "put-task", Task: &raw})
		if taskAborted(snap(t, b.store).Tasks[id]) {
			t.Fatal("absentExecution raw replacement policy expanded")
		}
	})
}

func TestTaskDefinitionBuiltinReceiptAbortMarksSurviveHeldAndFinalReplacement(t *testing.T) {
	for _, mark := range []string{"metadata", "checkpoint", "both"} {
		t.Run(mark, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				ownerID, id, entryID := mint(t, b.store), mint(t, b.store), mint(t, b.store)
				owner := Task{ID: ownerID, Conversation: 1, Kind: "legacy.receipt-parent", Status: "pending", Checkpoint: JSON{}}
				cp := toolCheckpoint{Offer: toolOffer{Name: "fixture", Implementation: "fixture.tool", Version: 1, Schema: JSON{"type": "object"}}, CallID: "fixture-call", Arguments: JSON{}, Abort: mark != "metadata"}
				checkpoint, err := dtoObject(cp, DefaultLimits())
				if err != nil {
					t.Fatal(err)
				}
				live := Task{ID: id, Conversation: 1, Owner: ownerID, Kind: "pi.tool", Status: "running", Checkpoint: checkpoint, Execution: &TaskExecution{Tag: builtinTaskTag, Builtin: &BuiltinTaskExecution{AbortRequested: mark != "checkpoint"}}}
				apply(t, b.store, Write{Op: "put-task", Task: &owner}, Write{Op: "put-task", Task: &live})
				// Synthetic valid ordinary abort receipt with real entry identity.
				// It exercises DTO/storage invariants, never production reachability.
				receipt := messageReceipt{Role: goai.RoleToolResult, ToolCallID: cp.CallID, ToolName: cp.Offer.Name, IsError: true, ErrorCode: "aborted", Content: []goai.ContentBlock{{Type: "text", Text: "aborted"}}}
				value, err := dtoObject(receipt, DefaultLimits())
				if err != nil {
					t.Fatal(err)
				}
				entry := Entry{ID: entryID, Conversation: 1, Kind: "message", Value: value, ByTask: id}
				makeHeld := func(stage string) Task {
					candidate, err := copyTask(live, DefaultLimits())
					if err != nil {
						t.Fatal(err)
					}
					result := cp
					result.ErrorCode, result.Result = "aborted", &receipt
					candidate.Checkpoint, err = dtoObject(result, DefaultLimits())
					if err != nil {
						t.Fatal(err)
					}
					candidate.Status = "completing"
					if stage == "final" {
						candidate.Status = "aborted"
					}
					candidate.Execution.Builtin.Hold = &BuiltinTaskHold{Stage: stage, Action: "tool-receipt", Outcome: TaskOutcome{Status: "aborted", Reason: "aborted", Result: &TaskValue{Present: true, Value: JSON{"entryId": entryID}}}, FinalStatus: "aborted", Entry: entryID, Conversation: 1, Owner: ownerID, CallID: cp.CallID}
					return candidate
				}
				for _, stage := range []string{"held", "final"} {
					candidate := makeHeld(stage)
					candidate.Execution.Builtin.AbortRequested = false
					delete(candidate.Checkpoint, "abort")
					// Receipt cp+stored entry remain coherent after removing the mark
					// flags: rejection must come from replacement monotonicity only.
					if _, err := copyTask(candidate, DefaultLimits()); err != nil {
						t.Fatal("receipt negative malformed", err)
					}
					before := snap(t, b.store)
					probe := candidateTables(before)
					probe.Tasks[id], probe.Entries[entryID] = candidate, entry
					if err := validateTaskReferences(probe, candidate, DefaultLimits()); err != nil {
						t.Fatal("receipt negative references", err)
					}
					_, err := b.store.Apply(bg, Batch{Writes: []Write{{Op: "append-entry", Entry: &entry}, {Op: "put-task", Task: &candidate}}})
					var markError *StorageRejected
					if !errors.As(err, &markError) || !strings.Contains(markError.Reason, "abort mark cannot be cleared") {
						t.Fatal("receipt removal rejected for unrelated cause", err)
					}
					after := snap(t, b.store)
					if after.Seq != before.Seq || len(after.Entries) != len(before.Entries) || !taskAborted(after.Tasks[id]) {
						t.Fatal("receipt failed replacement adopted prefix")
					}
				}
				held := makeHeld("held")
				apply(t, b.store, Write{Op: "append-entry", Entry: &entry}, Write{Op: "put-task", Task: &held})
				final := makeHeld("final")
				apply(t, b.store, Write{Op: "put-task", Task: &final})
				state := snap(t, b.store)
				if !taskAborted(state.Tasks[id]) || state.Tasks[id].Execution.Builtin.Hold.Stage != "final" || state.Tasks[id].Execution.Builtin.Hold.Entry != entryID {
					t.Fatal("receipt final lost mark/entry identity")
				}
			})
		})
	}
}
