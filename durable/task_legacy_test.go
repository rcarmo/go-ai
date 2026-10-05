package durable

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"path/filepath"
	"strings"
	"testing"
)

func reopenStoreAfterHarnessClose(t *testing.T, store Storage) Storage {
	t.Helper()
	switch native := store.(type) {
	case *MemoryStorage:
		reopened, err := OpenMemory(MemoryOptions{Image: native.image})
		if err != nil {
			t.Fatal(err)
		}
		return reopened
	case *JournalStorage:
		reopened, err := OpenJournal(filepath.Dir(native.path), JournalOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return reopened
	default:
		t.Fatalf("unsupported storage %T", store)
		return nil
	}
}

// These literal old DTOs are supplementary synthetic fixtures. A genuine
// f2c75-produced journal requires its separate named generation/validation slot.
func TestTaskLegacyFoundationAbsentExecutionDoesNotInferTask(t *testing.T) {
	l := DefaultLimits()
	for _, text := range []string{
		`{"id":2,"conversation":1,"kind":"legacy.raw","status":"pending","checkpoint":{"phase":"work","input":null,"version":99}}`,
		`{"id":2,"conversation":1,"kind":"legacy.raw","status":"completing","checkpoint":{"abort":true,"value":[1,null]}}`,
		`{"id":2,"conversation":1,"kind":"legacy.raw","status":"done","checkpoint":{"result":{"value":"old"}}}`,
	} {
		var task Task
		if err := decodeStrict([]byte(text), l, l.MaxRecordBytes, &task); err != nil {
			t.Fatal(err)
		}
		owned, err := copyTask(task, l)
		if err != nil || owned.Execution != nil {
			t.Fatal("old DTO changed", err)
		}
		view, err := CanonicalTask(owned, l)
		if err != nil || view.Version != 0 || view.Input != nil || view.State.Outcome != nil {
			t.Fatal("legacy generic authority inferred", view, err)
		}
		encoded, err := encodeBounded(owned, l, l.MaxRecordBytes)
		if err != nil {
			t.Fatal(err)
		}
		var object JSON
		if err = decodeStrict(encoded, l, l.MaxRecordBytes, &object); err != nil {
			t.Fatal(err)
		}
		if _, exists := object["execution"]; exists {
			t.Fatal("legacy execution attached")
		}
		if !equalJSONValue(object["id"], json.Number("2")) {
			t.Fatal("legacy ID changed")
		}
	}
}

func TestTaskLegacyFoundationCompletingAbortIsLiveJoin(t *testing.T) {
	l := DefaultLimits()
	checkpoint, err := dtoObject(generationCheckpoint{Phase: "tools", Submission: 2, Abort: true, Children: []ID{4, 5}}, l)
	if err != nil {
		t.Fatal(err)
	}
	legacy := Task{ID: 3, Conversation: 1, Kind: "pi.generation", Status: "completing", Checkpoint: checkpoint}
	view, err := CanonicalTask(legacy, l)
	if err != nil || !view.AbortRequested || view.State.Status != "waiting" || view.State.Outcome != nil || len(view.State.On) != 2 || taskHasDecidedOutcome(legacy) {
		t.Fatal("legacy completing/abort reinterpreted", view, err)
	}
	legacy.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: &BuiltinTaskExecution{}}
	view, err = CanonicalTask(legacy, l)
	if err != nil || !view.AbortRequested || taskHasDecidedOutcome(legacy) {
		t.Fatal("metadata erased old cp.Abort", view, err)
	}
}

func TestTaskLegacyFoundationExplicitOldReaderForwardUnsupported(t *testing.T) {
	// Exact pre-S2e field set. Its strict decoder rejects the additive schema;
	// this is an asserted downgrade boundary, not a forward-compatibility pass.
	type oldTask struct {
		ID           ID     `json:"id"`
		Conversation ID     `json:"conversation"`
		Owner        ID     `json:"owner,omitempty"`
		Kind         string `json:"kind"`
		Status       string `json:"status"`
		Checkpoint   JSON   `json:"checkpoint"`
	}
	task := foundationTask(2, nil)
	encoded, err := encodeBounded(task, DefaultLimits(), DefaultLimits().MaxRecordBytes)
	if err != nil {
		t.Fatal(err)
	}
	var old oldTask
	if err := decodeStrict(encoded, DefaultLimits(), DefaultLimits().MaxRecordBytes, &old); err == nil {
		t.Fatal("old decoder unexpectedly accepts new envelope")
	}
	legacy := task
	legacy.Execution = nil
	legacy.Kind = "legacy.raw"
	encoded, err = encodeBounded(legacy, DefaultLimits(), DefaultLimits().MaxRecordBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err = decodeStrict(encoded, DefaultLimits(), DefaultLimits().MaxRecordBytes, &old); err != nil {
		t.Fatal("old field set no longer readable", err)
	}
}

func TestTaskLegacyToolErrorRawDoneCanonicalFailedMapping(t *testing.T) {
	for _, code := range []string{"tool_error", "invalid_usage", "interrupted", "aborted", ""} {
		t.Run(code, func(t *testing.T) {
			cp := toolCheckpoint{CallID: "call", ErrorCode: code, Result: &MessageReceipt{Role: "toolResult", ToolCallID: "call", ErrorCode: code, IsError: code != ""}}
			out := TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: nil}}
			expected := "done"
			if code != "" {
				out = TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: code}, Result: &TaskValue{Present: true, Value: nil}}
			}
			if code == "aborted" {
				expected = "aborted"
				out = TaskOutcome{Status: "aborted", Result: &TaskValue{Present: true, Value: nil}}
			}
			status, err := toolReceiptRawStatus(out, cp)
			if err != nil || status != expected {
				t.Fatal("legacy physical/canonical map", status, err)
			}
			cp.Result.IsError = !cp.Result.IsError
			if _, err := toolReceiptRawStatus(out, cp); err == nil {
				t.Fatal("contradictory IsError accepted")
			}
		})
	}
}

func TestTaskLegacyFoundationBuiltinHoldUnionAndReceiptReference(t *testing.T) {
	l := DefaultLimits()
	receipt := MessageReceipt{Role: "toolResult", ToolCallID: "call", ToolName: "tool", Content: nil}
	checkpoint, err := dtoObject(toolCheckpoint{CallID: "call", Result: &receipt}, l)
	if err != nil {
		t.Fatal(err)
	}
	entryValue, err := dtoObject(receipt, l)
	if err != nil {
		t.Fatal(err)
	}
	task := Task{ID: 3, Conversation: 1, Owner: 2, Kind: "pi.tool", Status: "completing", Checkpoint: checkpoint, Execution: &TaskExecution{Tag: builtinTaskTag, Builtin: &BuiltinTaskExecution{Hold: &BuiltinTaskHold{Stage: "held", Action: "tool-receipt", Outcome: TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: JSON{"entryId": 4}}}, FinalStatus: "done", Entry: 4, Conversation: 1, Owner: 2, CallID: "call"}}}}
	owned, err := copyTask(task, l)
	if err != nil {
		t.Fatal(err)
	}
	state := initialState()
	state.Tasks[2] = Task{ID: 2, Conversation: 1, Kind: "legacy.owner", Status: "pending", Checkpoint: JSON{}}
	state.Tasks[3] = owned
	state.Entries[4] = Entry{ID: 4, Conversation: 1, Kind: "message", Value: entryValue, ByTask: 3}
	if err := validateTaskReferences(state, owned, l); err != nil {
		t.Fatal(err)
	}
	view, err := CanonicalTask(owned, l)
	if err != nil || view.State.Status != "completing" || view.State.Checkpoint != nil || view.State.Outcome == nil {
		t.Fatal("held receipt canonical view", err)
	}
	for name, mutate := range map[string]func(*Task){
		"callback-field-tag": func(t *Task) { t.Execution.Tag = "callback" },
		"missing-entry":      func(t *Task) { t.Execution.Builtin.Hold.Entry = 0 },
		"wrong-kind":         func(t *Task) { t.Kind = "legacy.raw" },
		"contradict-result":  func(t *Task) { t.Checkpoint = JSON{"callId": "other"} },
		"held-with-memos":    func(t *Task) { t.Execution.Builtin.Memos = map[string]*TaskValue{"keep": {Present: true, Value: nil}} },
		"raw-terminal-held":  func(t *Task) { t.Status = "done" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid, err := copyTask(task, l)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&invalid)
			if _, err := copyTask(invalid, l); err == nil {
				t.Fatal("invalid hold accepted")
			}
		})
	}
	wrong := state.Entries[4]
	wrong.ByTask = 2
	state.Entries[4] = wrong
	if err := validateTaskReferences(state, owned, l); err == nil {
		t.Fatal("unrelated same-conversation producing task accepted")
	}
	wrong.ByTask = 3
	state.Entries[4] = wrong
	badStatus, _ := copyTask(task, l)
	badStatus.Execution.Builtin.Hold.FinalStatus = "aborted"
	if _, err := copyTask(badStatus, l); err == nil {
		t.Fatal("opposite held final status accepted")
	}
	changed, _ := copyTask(task, l)
	changed.Checkpoint["output"] = "replaced"
	if err := validateTaskReplacement(owned, changed, l); err == nil {
		t.Fatal("adopted held checkpoint replaced")
	}
	marked, _ := copyTask(task, l)
	marked.Execution.Builtin.AbortRequested = true
	marked.Checkpoint["abort"] = true
	if err := validateTaskReplacement(owned, marked, l); err != nil {
		t.Fatal("fresh mark changed decided outcome", err)
	}
	final, _ := copyTask(marked, l)
	final.Execution.Builtin.Hold.Stage = "final"
	final.Status = "done"
	if err := validateTaskReplacement(marked, final, l); err != nil {
		t.Fatal("held->final", err)
	}
	if err := validateTaskReplacement(final, marked, l); err == nil {
		t.Fatal("final->held regression")
	}
	if err := validateTaskReplacement(final, final, l); err == nil {
		t.Fatal("repeated terminal mutation")
	}
	delete(state.Entries, 4)
	if err := validateTaskReferences(state, owned, l); err == nil {
		t.Fatal("missing adopted receipt accepted")
	}
}

// Supplementary synthetic old DTO, NOT the separately gated f2c75 journal
// producer fixture: that producer deliberately omitted receipt ByTask.
func TestTaskLegacyHistoricalUnattributedToolRoundAndCorruptSuffix(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		parentID, childID, entryID := mint(t, b.store), mint(t, b.store), mint(t, b.store)
		receipt := MessageReceipt{Role: goai.RoleToolResult, ToolCallID: "historical-call", ToolName: "historical", Content: []goai.ContentBlock{{Type: "text", Text: "old result"}}, Details: JSON{}}
		cp := toolCheckpoint{Offer: toolOffer{Name: "historical", Implementation: "legacy", Version: 1}, CallID: receipt.ToolCallID, Arguments: JSON{}, Started: true, Output: "old result", Result: &receipt}
		toolValue, err := dtoObject(cp, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		parentCP := generationCheckpoint{Phase: "tools", Children: []ID{childID}}
		parentValue, err := dtoObject(parentCP, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		entryValue, err := dtoObject(receipt, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		parent := Task{ID: parentID, Conversation: 1, Kind: "pi.generation", Status: "completing", Checkpoint: parentValue}
		child := Task{ID: childID, Conversation: 1, Owner: parentID, Kind: "pi.tool", Status: "done", Checkpoint: toolValue}
		apply(t, b.store, Write{Op: "put-task", Task: &parent}, Write{Op: "put-task", Task: &child}, Write{Op: "append-entry", Entry: &Entry{ID: entryID, Conversation: 1, Kind: "message", Value: entryValue}})
		h := taskTestHarness(t, b.store)
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.scheduler.toolRoundChildren(state, parent, parentCP); err != nil {
			t.Fatal("old ByTask0 receipt rejected", err)
		}
		if !h.scheduler.runnable(state, parent) {
			t.Fatal("historical tool-landed round stranded")
		}
		for _, suffix := range []ID{childID, childID + 1000} {
			bad := parentCP
			bad.Children = []ID{childID, suffix}
			if _, err := h.scheduler.toolRoundChildren(state, parent, bad); err == nil {
				t.Fatal("invalid suffix admitted", suffix)
			}
		}
		// A pending first child cannot bypass a malformed later member either.
		child.Status = "pending"
		cp.Result = nil
		child.Checkpoint, err = dtoObject(cp, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		state.Tasks[childID] = child
		for _, suffix := range []ID{childID, childID + 1000} {
			bad := parentCP
			bad.Children = []ID{childID, suffix}
			parent.Checkpoint, err = dtoObject(bad, DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			state.Tasks[parentID] = parent
			if h.scheduler.runnable(state, child) {
				t.Fatal("first effect admitted before suffix validation", suffix)
			}
		}
	})
}

func TestTaskLegacyAbortMarkedUnknownRemainsBlocked(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		id := mint(t, b.store)
		task := Task{ID: id, Conversation: 1, Kind: "legacy.raw", Status: "pending", Checkpoint: JSON{"abort": true}}
		apply(t, b.store, Write{Op: "put-task", Task: &task})
		h := taskTestHarness(t, b.store)
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		// Explicit real scheduler selection passes are supplementary to Resume:
		// unknown raw tasks must NEVER reserve, with or without an abort field.
		for k := 0; k < 3; k++ {
			reservations, err := h.scheduler.reserve()

			discardUnstartedTaskReservations(t, h, reservations)
			if err != nil || len(reservations) != 0 {
				t.Fatal("unknown raw dispatch", reservations, err)
			}
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if state.Tasks[id].Status != "pending" || state.Tasks[id].Execution != nil {
			t.Fatal("raw rewritten", state.Tasks[id])
		}
		inspection, err := h.InspectTasks(bg)
		if err != nil || len(inspection.Tasks) != 1 || inspection.Tasks[0].Reason != "legacy_raw_task" {
			t.Fatal("unknown raw not blocked", inspection, err)
		}
		if _, err := h.AbortTask(bg, id); err == nil {
			t.Fatal("unknown raw abort adapter invented")
		}
		after, err := h.Snapshot(bg)
		if err != nil || after.Seq != state.Seq {
			t.Fatal("raw abort wrote outcome", after.Seq, state.Seq, err)
		}
	})
}

// Supplementary private scheduler disposition seam: this validates deterministic
// stored-prefix cleanup only. It does NOT establish a production scheduler-tool
// fault/orphan creator, which is tracked separately in the concrete ledger.
func TestTaskLegacySchedulerToolFinalPrefixBoundAndNoReplay(t *testing.T) {
	for _, outcome := range []string{"faulted", "orphaned"} {
		for _, prefix := range []string{"prefix", "at-bound"} {
			t.Run(outcome+"/"+prefix, func(t *testing.T) {
				backends(t, func(t *testing.T, b backend) {
					h := taskTestHarness(t, b.store)
					var parentID, toolID ID
					text := "confirmed prefix"
					if prefix == "at-bound" {
						text = strings.Repeat("p", MaxToolOutputBytes)
					}
					_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
						var err error
						parentID, err = tx.MintID()
						if err != nil {
							return err
						}
						toolID, err = tx.MintID()
						if err != nil {
							return err
						}
						parent := foundationTask(parentID, nil)
						if err := tx.stage(Write{Op: "put-task", Task: &parent}); err != nil {
							return err
						}
						cp, err := dtoObject(toolCheckpoint{Offer: toolOffer{Name: "stored-tool", Implementation: "stored", Version: 1}, CallID: "stored-call", Arguments: JSON{}, Started: true, Output: text}, tx.limits)
						if err != nil {
							return err
						}
						tool := Task{ID: toolID, Owner: parentID, Conversation: 1, Kind: "pi.tool", Status: "pending", Checkpoint: cp}
						return tx.PutTask(tool)
					})
					if err != nil {
						t.Fatal(err)
					}
					_, err = h.session.taskCommit(bg, func(tx *Tx) error {
						decision := TaskOutcome{Status: "faulted", Error: &TaskOutcomeError{Message: "bounded_fault"}}
						if outcome == "orphaned" {
							decision = TaskOutcome{Status: "orphaned", Reason: "missing_builtin_adapter"}
						}
						return h.scheduler.builtinDecision(tx, tx.state.Tasks[toolID], decision)
					})
					if err != nil {
						t.Fatal(err)
					}
					state, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					var cp toolCheckpoint
					if err := fromObject(state.Tasks[toolID].Checkpoint, &cp, h.session.limits); err != nil {
						t.Fatal(err)
					}
					code := "faulted"
					if outcome == "orphaned" {
						code = "missing_builtin_adapter"
					}
					want := text + "\n" + code
					if prefix == "at-bound" {
						want = code
					}
					if state.Tasks[toolID].Status != "failed" || cp.Result == nil || cp.Result.Usage != nil || !cp.Result.IsError || cp.Result.ErrorCode != code || cp.Result.ToolCallID != "stored-call" || cp.Result.ToolName != "stored-tool" || len(cp.Result.Content) != 1 || cp.Result.Content[0].Text != want {
						t.Fatal("scheduler-tool stored receipt", state.Tasks[toolID], cp.Result)
					}
					if len(state.Entries) != 1 {
						t.Fatal("scheduler cleanup receipt count")
					}
					if err := h.Close(bg); err != nil {
						t.Fatal(err)
					}
					second := taskTestHarness(t, reopenStoreAfterHarnessClose(t, b.store))
					opened, err := second.Snapshot(bg)
					if err != nil || len(opened.Entries) != 1 || opened.Tasks[toolID].Status != "failed" {
						t.Fatal("scheduler-tool cleanup replay", err)
					}
				})
			})
		}
	}
}
