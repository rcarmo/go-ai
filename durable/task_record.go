package durable

import "strings"

const (
	nativeTaskTag  = "native-task-v1"
	builtinTaskTag = "builtin-task-v1"
)

// copyTask is the complete task ownership/codec boundary. Caller JSON is walked
// strictly BEFORE the trusted DTO envelope can encode it. It also serves replay,
// raw queries, image cloning and pre-admission publication through prepare.
func copyTask(task Task, l Limits) (Task, error) {
	task.StartedAt = copyTaskTime(task.StartedAt)
	task.EndedAt = copyTaskTime(task.EndedAt)
	checkpoint, err := copyObject(task.Checkpoint, l)
	if err != nil {
		return Task{}, err
	}
	task.Checkpoint = checkpoint
	if task.Execution != nil {
		execution := *task.Execution
		task.Execution = &execution
		switch execution.Tag {
		case nativeTaskTag:
			if execution.Native == nil || execution.Builtin != nil {
				return Task{}, reject("native task execution union")
			}
			native := *execution.Native
			execution.Native = &native
			if native.Input, err = copyTaskValue(native.Input, l); err != nil {
				return Task{}, err
			}
			if native.Memos, err = copyTaskMemos(native.Memos, l); err != nil {
				return Task{}, err
			}
			if native.State, err = copyTaskState(native.State, l); err != nil {
				return Task{}, err
			}
		case builtinTaskTag:
			if execution.Builtin == nil || execution.Native != nil {
				return Task{}, reject("builtin task execution union")
			}
			builtin := *execution.Builtin
			execution.Builtin = &builtin
			if builtin.Memos, err = copyTaskMemos(builtin.Memos, l); err != nil {
				return Task{}, err
			}
			if builtin.Hold != nil {
				hold := *builtin.Hold
				builtin.Hold = &hold
				outcome, e := copyTaskOutcome(&hold.Outcome, l)
				if e != nil {
					return Task{}, e
				}
				hold.Outcome = *outcome
			}
		default:
			return Task{}, reject("unsupported task execution tag")
		}
	}
	if err = validateTaskRecord(task, l); err != nil {
		return Task{}, err
	}
	// Every mutable field above is independently detached and strict. Encode
	// once for aggregate envelope/depth/member/byte limits; decoding that owned
	// envelope again adds no ownership protection.
	if _, err := encodeBounded(task, l, l.MaxRecordBytes); err != nil {
		return Task{}, err
	}
	return task, nil
}

func copyTaskValue(value *TaskValue, l Limits) (*TaskValue, error) {
	if value == nil || !value.Present {
		return nil, reject("task value requires explicit presence")
	}
	owned, err := ownJSONValue(value.Value, l)
	if err != nil {
		return nil, err
	}
	return &TaskValue{Present: true, Value: owned}, nil
}

func copyTaskMemos(memos map[string]*TaskValue, l Limits) (map[string]*TaskValue, error) {
	if memos == nil {
		return nil, nil
	}
	if len(memos) > l.MaxMembers {
		return nil, reject("task memo count limit")
	}
	owned := make(map[string]*TaskValue, len(memos))
	for key, value := range memos {
		if err := (&jsonBudget{l: l}).str(key); err != nil {
			return nil, err
		}
		copy, err := copyTaskValue(value, l)
		if err != nil {
			return nil, err
		}
		owned[key] = copy
	}
	return owned, nil
}

func copyTaskState(state TaskState, l Limits) (TaskState, error) {
	if state.Checkpoint != nil {
		checkpoint, err := copyObject(state.Checkpoint, l)
		if err != nil {
			return TaskState{}, err
		}
		state.Checkpoint = checkpoint
	}
	if len(state.On) > l.MaxMembers {
		return TaskState{}, reject("task wait count limit")
	}
	state.On = append([]ID(nil), state.On...)
	if state.Outcome != nil {
		outcome, err := copyTaskOutcome(state.Outcome, l)
		if err != nil {
			return TaskState{}, err
		}
		state.Outcome = outcome
	}
	return state, nil
}

func copyTaskOutcome(outcome *TaskOutcome, l Limits) (*TaskOutcome, error) {
	if outcome == nil {
		return nil, reject("task outcome missing")
	}
	owned := *outcome
	if outcome.Result != nil {
		result, err := copyTaskValue(outcome.Result, l)
		if err != nil {
			return nil, err
		}
		owned.Result = result
	}
	if outcome.Error != nil {
		error := *outcome.Error
		owned.Error = &error
		if error.Detail != nil {
			detail, err := copyTaskValue(error.Detail, l)
			if err != nil {
				return nil, err
			}
			error.Detail = detail
		}
	}
	if err := validateTaskOutcome(&owned); err != nil {
		return nil, err
	}
	return &owned, nil
}

func validateTaskOutcome(outcome *TaskOutcome) error {
	if outcome == nil {
		return reject("task outcome missing")
	}
	if outcome.Result != nil && !outcome.Result.Present {
		return reject("task result presence")
	}
	if outcome.Error != nil && outcome.Error.Detail != nil && !outcome.Error.Detail.Present {
		return reject("task error detail presence")
	}
	switch outcome.Status {
	case "completed":
		if outcome.Result == nil || outcome.Error != nil || outcome.Reason != "" {
			return reject("completed task outcome union")
		}
	case "failed":
		if outcome.Error == nil || outcome.Error.Message == "" || outcome.Reason != "" {
			return reject("failed task outcome union")
		}
	case "aborted":
		if outcome.Error != nil {
			return reject("aborted task outcome union")
		}
	case "faulted":
		if outcome.Error == nil || outcome.Error.Message == "" || outcome.Result != nil || outcome.Reason != "" {
			return reject("faulted task outcome union")
		}
	case "orphaned":
		if outcome.Reason == "" || outcome.Error != nil || outcome.Result != nil {
			return reject("orphaned task outcome union")
		}
	default:
		return reject("unknown task outcome")
	}
	return nil
}

func validateTaskState(state TaskState) error {
	switch state.Status {
	case "pending", "running", "waiting":
		phase, ok := state.Checkpoint["phase"].(string)
		if state.Checkpoint == nil || !ok || !validKind(phase) || state.Outcome != nil {
			return reject("live task state union")
		}
		if state.Status == "waiting" {
			if state.Policy != "allSettled" && state.Policy != "failFast" {
				return reject("task waiting policy")
			}
			for _, id := range state.On {
				if id == 0 || uint64(id) > MaxID {
					return reject("task waiting member")
				}
			}
		} else if len(state.On) != 0 || state.Policy != "" {
			return reject("nonwaiting task has wait")
		}
	case "completing", "terminal":
		if state.Checkpoint != nil || len(state.On) != 0 || state.Policy != "" {
			return reject("decided task state union")
		}
		return validateTaskOutcome(state.Outcome)
	default:
		return reject("unknown task state")
	}
	return nil
}

func outcomeRawStatus(outcome *TaskOutcome) string {
	if outcome == nil {
		return ""
	}
	switch outcome.Status {
	case "completed":
		return "done"
	case "aborted":
		return "aborted"
	case "failed", "faulted", "orphaned":
		return "failed"
	}
	return ""
}

func validateTaskRecord(task Task, l Limits) error {
	if task.Checkpoint == nil || !validKind(task.Kind) || !validStatus(task.Status) || uint64(task.Conversation) > MaxID || uint64(task.Owner) > MaxID || task.Owner == task.ID && task.ID != 0 {
		return reject("invalid task record")
	}
	if task.Execution == nil {
		return nil // exact legacy object-checkpoint contract, no inferred envelope
	}
	switch task.Execution.Tag {
	case nativeTaskTag:
		native := task.Execution.Native
		if native == nil || task.Execution.Builtin != nil || strings.HasPrefix(task.Kind, "pi.") || len(task.Checkpoint) != 0 || native.Version == 0 || native.Version > MaxID || native.Input == nil || !native.Input.Present {
			return reject("invalid native task execution")
		}
		if err := validateTaskState(native.State); err != nil {
			return err
		}
		status := native.State.Status
		if status == "terminal" {
			status = outcomeRawStatus(native.State.Outcome)
		}
		if status != task.Status || native.Background && task.Owner != 0 {
			return reject("native task status/ownership disagreement")
		}
		if (native.State.Status == "terminal" || native.State.Status == "completing") && native.Memos != nil {
			return reject("decided task has memos")
		}
	case builtinTaskTag:
		builtin := task.Execution.Builtin
		if builtin == nil || task.Execution.Native != nil || task.Kind != "pi.generation" && task.Kind != "pi.tool" || builtin.Background && task.Owner != 0 {
			return reject("invalid builtin task execution")
		}
		if builtin.Hold != nil {
			if builtin.Memos != nil {
				return reject("held builtin has memos")
			}
			return validateBuiltinHold(task, builtin.Hold, l)
		}
		if terminalStatus(task.Status) && builtin.Memos != nil {
			return reject("terminal builtin has memos")
		}
	default:
		return reject("unsupported task execution tag")
	}
	return nil
}

func validateBuiltinHold(task Task, hold *BuiltinTaskHold, l Limits) error {
	if err := validateTaskOutcome(&hold.Outcome); err != nil {
		return err
	}
	if hold.Stage != "held" && hold.Stage != "final" || !terminalStatus(hold.FinalStatus) || hold.Conversation != task.Conversation || hold.Owner != task.Owner || uint64(hold.Entry) > MaxID || uint64(hold.Submission) > MaxID {
		return reject("builtin hold identity/stage")
	}
	status := "completing"
	if hold.Stage == "final" {
		status = hold.FinalStatus
	}
	if task.Status != status {
		return reject("builtin hold raw status disagreement")
	}
	switch hold.Action {
	case "generation-receipt", "generation-handoff", "generation-reset", "generation-abort", "generation-no-model", "generation-model-failure", "scheduler-generation":
		if task.Kind != "pi.generation" || hold.Submission == 0 || hold.CallID != "" {
			return reject("generation hold union")
		}
		var cp generationCheckpoint
		if err := fromObject(task.Checkpoint, &cp, l); err != nil {
			return err
		}
		if (hold.Action == "generation-no-model" || hold.Action == "generation-model-failure") && (hold.Outcome.Status != "failed" || hold.FinalStatus != "failed" || cp.Phase != "terminal" || hold.Entry != 0) {
			return reject("generation no-model hold disagreement")
		}
		if hold.Action == "generation-abort" && (hold.Outcome.Status != "aborted" || hold.FinalStatus != "aborted" || cp.Phase != "terminal") {
			return reject("generation abort hold disagreement")
		}
		if hold.Action == "generation-reset" && (!cp.Reset || hold.Outcome.Status != "completed" || hold.FinalStatus != "done" || cp.AssistantEntry != hold.Entry) {
			return reject("generation reset hold disagreement")
		}
		if cp.Submission != hold.Submission || (hold.Action == "generation-receipt" || hold.Action == "generation-handoff" || hold.Action == "generation-reset") && (hold.Entry == 0 || cp.Phase != "terminal") || hold.Action == "scheduler-generation" && hold.Stage == "held" && hold.Entry != 0 {
			return reject("generation hold checkpoint disagreement")
		}
	case "tool-receipt", "scheduler-tool", "scheduler-tool-missing":
		if task.Kind != "pi.tool" || hold.Submission != 0 || hold.CallID == "" || hold.Owner == 0 {
			return reject("tool hold union")
		}
		var cp toolCheckpoint
		if err := fromObject(task.Checkpoint, &cp, l); err != nil {
			return err
		}
		if hold.Action == "tool-receipt" {
			raw, err := toolReceiptRawStatus(hold.Outcome, cp)
			if err != nil || raw != hold.FinalStatus {
				return reject("tool receipt final status/code/outcome disagreement")
			}
		}
		if cp.CallID != hold.CallID || hold.Action == "tool-receipt" && (hold.Entry == 0 || cp.Result == nil) || hold.Action == "scheduler-tool" && hold.Stage == "held" && (hold.Entry != 0 || cp.Result != nil) || hold.Action == "scheduler-tool" && hold.Stage == "final" && (hold.Entry == 0 || cp.Result == nil) {
			return reject("tool hold checkpoint disagreement")
		}
		if hold.Action == "scheduler-tool-missing" && (hold.Entry != 0 || cp.Result != nil) {
			return reject("scheduler missing tool result disagreement")
		}
	default:
		return reject("unsupported builtin hold action")
	}
	if hold.Action != "tool-receipt" && hold.FinalStatus != outcomeRawStatus(&hold.Outcome) {
		return reject("builtin hold final status/outcome disagreement")
	}
	if strings.HasPrefix(hold.Action, "scheduler-") && hold.Outcome.Status != "faulted" && hold.Outcome.Status != "orphaned" {
		return reject("scheduler hold outcome")
	}
	return nil
}

// Call-phase harness errors complete with an error result. Host execution
// throws/interruption fail and retain cancellation intent.
func toolCallCompletedError(code string) bool {
	return code == "tool_unavailable" || code == "invalid_arguments" || code == "blocked"
}

func toolReceiptRawStatus(outcome TaskOutcome, cp toolCheckpoint) (string, error) {
	wantError := cp.ErrorCode != "" || cp.ReportedError
	if cp.FinalIsError != nil {
		wantError = *cp.FinalIsError
	}
	if cp.Result == nil || cp.Result.ErrorCode != cp.ErrorCode || cp.Result.IsError != wantError {
		return "", reject("tool receipt error union")
	}
	if cp.ErrorCode == "aborted" {
		if outcome.Status != "aborted" {
			return "", reject("tool abort outcome")
		}
		return "aborted", nil
	}
	if toolCallCompletedError(cp.ErrorCode) {
		if outcome.Status != "completed" || outcome.Error != nil {
			return "", reject("completed tool call error disagreement")
		}
		return "done", nil
	}
	if cp.ErrorCode != "" {
		if outcome.Status != "failed" || outcome.Error == nil || (cp.ErrorCode != "tool_error" && outcome.Error.Message != cp.ErrorCode) {
			return "", reject("tool failed receipt outcome")
		}
		return "done", nil
	}
	if outcome.Status != "completed" {
		return "", reject("tool success receipt outcome")
	}
	return "done", nil
}

func taskBackground(task Task) bool {
	if task.Execution == nil {
		return false
	}
	if task.Execution.Native != nil {
		return task.Execution.Native.Background
	}
	if task.Execution.Builtin != nil {
		return task.Execution.Builtin.Background
	}
	return false
}

// Replacements cannot erase an admitted execution envelope or mutate ownership.
// Historical raw legacy ownership is preserved on first metadata attachment.
func validateTaskReplacement(old, next Task, l Limits) error {
	if old.StartedAt != nil && (next.StartedAt == nil || *old.StartedAt != *next.StartedAt) || old.EndedAt != nil && (next.EndedAt == nil || *old.EndedAt != *next.EndedAt) {
		return reject("task lifecycle time replacement")
	}
	if old.Conversation != next.Conversation || old.Kind != next.Kind {
		return reject("task identity changed")
	}
	if old.Execution != nil || next.Execution != nil {
		if old.Owner != next.Owner || taskBackground(old) != taskBackground(next) || old.Execution != nil && (next.Execution == nil || old.Execution.Tag != next.Execution.Tag) {
			return reject("task execution ownership/format changed")
		}
		if old.Execution == nil && next.Execution.Native != nil {
			return reject("legacy task cannot become generic execution")
		}
		if old.Execution != nil && terminalStatus(old.Status) {
			return reject("terminal execution task cannot be replaced")
		}
		// Confirmed execution marks are monotonic in every live/held/final
		// transition, not just after outcome selection. Preserve each admitted
		// builtin mark representation as well as its canonical OR projection.
		// Raw absentExecution replacements retain historical policy; a first
		// metadata attachment cannot erase their existing logical mark.
		if taskAborted(old) && !taskAborted(next) {
			return reject("task abort mark cannot be cleared")
		}
		if old.Execution != nil {
			if old.Execution.Native != nil && old.Execution.Native.AbortRequested && !next.Execution.Native.AbortRequested {
				return reject("native task abort mark cannot be cleared")
			}
			if old.Execution.Builtin != nil {
				before, after := old.Execution.Builtin, next.Execution.Builtin
				oldCheckpointMark, _ := old.Checkpoint["abort"].(bool)
				newCheckpointMark, _ := next.Checkpoint["abort"].(bool)
				if before.AbortRequested && !after.AbortRequested || oldCheckpointMark && !newCheckpointMark {
					return reject("builtin task abort mark cannot be cleared")
				}
			}
		}
		if old.Execution != nil && taskHasDecidedOutcome(old) {
			if !taskHasDecidedOutcome(next) || terminalStatus(old.Status) {
				return reject("decided task cannot resume or replace terminal")
			}
			if old.Execution.Native != nil {
				before, after := old.Execution.Native, next.Execution.Native
				if before.Version != after.Version || !equalTaskValue(before.Input, after.Input, l) || before.AbortRequested && !after.AbortRequested || !equalTaskOutcome(before.State.Outcome, after.State.Outcome, l) {
					return reject("held native outcome changed")
				}
			} else {
				before, after := old.Execution.Builtin, next.Execution.Builtin
				if before.AbortRequested && !after.AbortRequested || !sameBuiltinHold(before.Hold, after.Hold, l) || !heldBuiltinCheckpointEqual(old, next, l) {
					return reject("held builtin disposition/checkpoint changed")
				}
			}
		}
	}
	return nil
}

func equalTaskValue(left, right *TaskValue, l Limits) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	a, err := ownJSONValue(left.Value, l)
	if err != nil {
		return false
	}
	b, err := ownJSONValue(right.Value, l)
	return err == nil && left.Present == right.Present && equalDeltaJSON(a, b)
}
func equalTaskOutcome(left, right *TaskOutcome, l Limits) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	if left.Status != right.Status || left.Reason != right.Reason || !equalTaskValue(left.Result, right.Result, l) {
		return false
	}
	if left.Error == nil || right.Error == nil {
		return left.Error == nil && right.Error == nil
	}
	return left.Error.Message == right.Error.Message && equalTaskValue(left.Error.Detail, right.Error.Detail, l)
}
func sameBuiltinHold(left, right *BuiltinTaskHold, l Limits) bool {
	if left == nil || right == nil {
		return false
	}
	// A scheduler cleanup may add its receipt only in the held->final step.
	entryMatches := left.Entry == right.Entry || strings.HasPrefix(left.Action, "scheduler-") && left.Stage == "held" && right.Stage == "final" && left.Entry == 0
	stageMatches := left.Stage == "held" && (right.Stage == "held" || right.Stage == "final")
	return stageMatches && entryMatches && left.Action == right.Action && left.FinalStatus == right.FinalStatus && left.Submission == right.Submission && left.Conversation == right.Conversation && left.Owner == right.Owner && left.CallID == right.CallID && equalTaskOutcome(&left.Outcome, &right.Outcome, l)
}

// A decided receipt holds its single legacy checkpoint immutably. A later mark
// may add cp.Abort; scheduler-generation final cleanup alone consumes Partial.
func heldBuiltinCheckpointEqual(old, next Task, l Limits) bool {
	before := make(JSON, len(old.Checkpoint))
	after := make(JSON, len(next.Checkpoint))
	for k, v := range old.Checkpoint {
		before[k] = v
	}
	for k, v := range next.Checkpoint {
		after[k] = v
	}
	oldAbort, _ := before["abort"].(bool)
	newAbort, _ := after["abort"].(bool)
	if oldAbort && !newAbort {
		return false
	}
	delete(before, "abort")
	delete(after, "abort")
	hold, nextHold := old.Execution.Builtin.Hold, next.Execution.Builtin.Hold
	if hold.Stage == "held" && nextHold.Stage == "final" {
		if hold.Action == "scheduler-generation" {
			delete(before, "partial")
			delete(after, "partial")
		}
		if hold.Action == "scheduler-tool" {
			delete(before, "result")
			delete(after, "result")
			delete(before, "errorCode")
			delete(after, "errorCode")
		}
	}
	return equalTaskValue(&TaskValue{Present: true, Value: before}, &TaskValue{Present: true, Value: after}, l)
}

func taskHasDecidedOutcome(task Task) bool {
	if task.Execution == nil {
		return false // legacy completing cp.Abort/tool-join is still runnable
	}
	if task.Execution.Native != nil {
		status := task.Execution.Native.State.Status
		return status == "completing" || status == "terminal"
	}
	return task.Execution.Builtin != nil && task.Execution.Builtin.Hold != nil
}

// Cross-record validation runs on the FINAL candidate. Staging may contain
// forward task/conversation/entry references; those are not resolved early.
func validateTaskReferences(s Snapshot, task Task, l Limits) error {
	if task.Execution == nil {
		return nil
	}
	if task.Owner != 0 {
		owner, ok := s.Tasks[task.Owner]
		if !ok || owner.Conversation != task.Conversation || taskBackground(task) {
			return reject("native child task owner/conversation")
		}
	}
	if task.Execution.Native != nil && task.Execution.Native.State.Status == "waiting" {
		state := task.Execution.Native.State
		owners, err := taskOwnerChain(s, task)
		if err != nil {
			return err
		}
		for _, id := range state.On {
			member, ok := s.Tasks[id]
			if !ok || id == task.ID || owners[id] || state.Policy == "failFast" && member.Owner != task.ID {
				return reject("native task wait references")
			}
		}
	}
	if builtin := task.Execution.Builtin; builtin != nil && builtin.Hold != nil {
		hold := builtin.Hold
		if hold.Submission != 0 {
			submission, ok := s.Submissions[hold.Submission]
			if !ok || submission.Conversation != task.Conversation {
				return reject("hold submission missing")
			}
		}
		if hold.Action == "generation-handoff" {
			// The full task/checkpoint JSON ownership budget is already validated.
			// Inspect only these scalar references; decoding both retained request
			// transcripts again on every later commit creates quadratic churn.
			id, valid := exactNumber(task.Checkpoint["successor"])
			if !valid || !id.IsInt() || id.Sign() <= 0 || !id.Num().IsUint64() || id.Num().Uint64() > MaxID {
				return reject("generation handoff successor id")
			}
			successor, ok := s.Tasks[ID(id.Num().Uint64())]
			if successor.ID <= task.ID || !ok || successor.Kind != "pi.generation" || successor.Conversation != task.Conversation || successor.Owner != 0 || hold.FinalStatus != "done" || hold.Outcome.Status != "completed" {
				return reject("generation handoff successor disagreement")
			}
			submission, valid := exactNumber(successor.Checkpoint["submission"])
			if !valid || !submission.IsInt() || !submission.Num().IsUint64() || ID(submission.Num().Uint64()) != hold.Submission {
				return reject("generation handoff input disagreement")
			}
		}
		if hold.Entry != 0 {
			entry, ok := s.Entries[hold.Entry]
			if !ok || entry.Conversation != task.Conversation || entry.Kind != "message" || entry.ByTask != task.ID {
				return reject("hold receipt entry missing")
			}
			var receipt MessageReceipt
			if err := fromObject(entry.Value, &receipt, l); err != nil {
				return err
			}
			if task.Kind == "pi.tool" {
				var cp toolCheckpoint
				if err := fromObject(task.Checkpoint, &cp, l); err != nil {
					return err
				}
				if receipt.ToolCallID != hold.CallID || receipt.Role != "toolResult" || cp.Result == nil {
					return reject("hold tool receipt disagreement")
				}
				left, err := dtoObject(receipt, l)
				if err != nil {
					return err
				}
				right, err := dtoObject(cp.Result, l)
				if err != nil || !equalTaskValue(&TaskValue{Present: true, Value: left}, &TaskValue{Present: true, Value: right}, l) {
					return reject("hold tool result disagreement")
				}
			} else {
				var cp generationCheckpoint
				if err := fromObject(task.Checkpoint, &cp, l); err != nil {
					return err
				}
				if receipt.Role != "assistant" || cp.Submission != hold.Submission || cp.Model != nil && (receipt.Api != cp.Model.Api || receipt.Provider != cp.Model.Provider || receipt.Model != cp.Model.ID) {
					return reject("hold generation receipt/attribution disagreement")
				}
				if hold.Stage == "final" && hold.Action == "generation-receipt" {
					sub := s.Submissions[hold.Submission]
					raw, ok := sub.Value["message"].(map[string]any)
					if !ok || sub.Status != hold.FinalStatus || !equalTaskValue(&TaskValue{Present: true, Value: entry.Value}, &TaskValue{Present: true, Value: raw}, l) {
						return reject("final hold submission/receipt disagreement")
					}
				}
			}
		}
	}
	return nil
}

// Ownership follows tasks AND conversations, even through terminal owners.
// The visited type tag distinguishes an ID's role, though IDs are globally unique.
func taskOwnerChain(s Snapshot, task Task) (map[ID]bool, error) {
	owners := map[ID]bool{}
	type node struct {
		task bool
		id   ID
	}
	seen := map[node]bool{{true, task.ID}: true}
	next := node{false, task.Conversation}
	if task.Owner != 0 {
		next = node{true, task.Owner}
	}
	for next.id != 0 {
		if seen[next] {
			return nil, reject("combined task/conversation ownership cycle")
		}
		seen[next] = true
		if next.task {
			owner, ok := s.Tasks[next.id]
			if !ok {
				return nil, reject("ownership task missing")
			}
			owners[next.id] = true
			next = node{false, owner.Conversation}
			if owner.Owner != 0 {
				next = node{true, owner.Owner}
			}
		} else {
			conversation, ok := s.Conversations[next.id]
			if !ok {
				return nil, reject("ownership conversation missing")
			}
			next = node{true, conversation.Owner}
		}
	}
	return owners, nil
}
