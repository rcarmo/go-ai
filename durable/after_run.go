package durable

import (
	"context"
	"fmt"
	"strings"
)

// RunSummary identifies committed history of an answered run, including a
// terminating tool round. Entry identities remain valid across compaction.
type RunSummary struct {
	Reason string `json:"reason"`
	First  ID     `json:"first"`
	Answer ID     `json:"answer"`
	Last   ID     `json:"last"`
}

// RunEndTask refers to a task by kind. The installed phase-local definition,
// not this token's callbacks/version, supplies its initial state and execution.
type RunEndTask struct {
	Task  *TaskDefinition
	Input any
}
type AfterRunResult struct{ Tasks []RunEndTask }

type preparedRunEndTask struct {
	definition *TaskDefinition
	execution  *NativeTaskExecution
}

// Source-defined request diagnostics deliberately retain their capitalization.
type runEndRequestError string

func (e runEndRequestError) Error() string { return string(e) }

type runEndRequestErrors struct{ rejected []error }

func (e *runEndRequestErrors) Error() string {
	return fmt.Sprintf("%d afterRun task requests were rejected", len(e.rejected))
}
func (e *runEndRequestErrors) Unwrap() []error { return append([]error(nil), e.rejected...) }

func callAfterRun(ctx context.Context, hook func(context.Context, RunSummary, *HookAPI) (*AfterRunResult, error), run RunSummary, r *TaskRuntime) (result *AfterRunResult, err error) {
	defer func() {
		if recover() != nil {
			err = reject("afterRun hook panic")
		}
	}()
	return hook(ctx, run, &HookAPI{r})
}
func afterRunHooks(r *TaskRuntime) ([]selectedGenerationHook, error) {
	agent, err := r.Agent(r.context)
	if err != nil {
		return nil, err
	}
	return r.phaseSelection().selectedHooks(r.harness.selectionAgent(agentState{Name: agent.Configuration.Name, Extensions: agent.Configuration.Extensions, ExtensionFilter: agent.Configuration.ExtensionFilter})), nil
}

// Native mapped builtins use task.pi.* names; that is not a reserved public
// application namespace, so do not reject unrelated definitions sharing it.
func afterRunBuiltinKind(name string) bool {
	return strings.HasPrefix(name, "pi.") || name == "task.pi.compaction" || name == nestedToolKind || name == "task.pi.subagent-reporter"
}

func prepareRunEndTask(r *TaskRuntime, request RunEndTask) (preparedRunEndTask, error) {
	name := request.Task.Kind()
	if afterRunBuiltinKind(name) {
		return preparedRunEndTask{}, runEndRequestError(fmt.Sprintf("An afterRun hook cannot start the built-in task %s", name))
	}
	definition := r.registry.Task(name)
	if definition == nil {
		return preparedRunEndTask{}, runEndRequestError(fmt.Sprintf("Task %s is not installed", name))
	}
	// Use the existing strict JSON copier and initializer guards, with a named
	// unknown-phase diagnostic. The temporary token never enters the registry.
	copy := *definition
	copy.options.Initial = func(input any) (JSON, error) {
		cp, err := definition.options.Initial(input)
		if err != nil {
			return nil, err
		}
		cp, err = copyObject(cp, r.harness.session.limits)
		if err != nil {
			return nil, err
		}
		if phase, ok := cp["phase"].(string); ok && definition.options.Phases[phase] == nil {
			return nil, runEndRequestError(fmt.Sprintf("Task %s starts at the unknown phase %s", name, phase))
		}
		return cp, nil
	}
	execution, err := copy.initial(request.Input, r.harness.session.limits)
	return preparedRunEndTask{definition, execution}, err
}
func prepareRunEndTasks(r *TaskRuntime, requests []RunEndTask, prepared *[]preparedRunEndTask) error {
	var rejected []error
	for _, request := range requests {
		task, err := prepareRunEndTask(r, request)
		if err != nil {
			rejected = append(rejected, err)
		} else {
			*prepared = append(*prepared, task)
		}
	}
	if len(rejected) == 1 {
		return rejected[0]
	}
	if len(rejected) > 1 {
		return &runEndRequestErrors{rejected}
	}
	return nil
}

// holdAnsweredRun prepares only private task data. Cleanup settles inputs and
// retains the live run in the same commit as the already appended answer.
func (h *Harness) holdAnsweredRun(tx *Tx, task *Task, cp *generationCheckpoint, summary RunSummary) (bool, error) {
	live, err := builtin(tx, task.Conversation, "pi.live")
	if err != nil {
		return false, err
	}
	value, err := live.Get()
	if err != nil {
		return false, err
	}
	run := generationObject(value["run"])
	if submissionEntryID(run["task"]) != task.ID {
		return false, nil
	}
	summary.First = submissionEntryID(run["first"])
	if summary.First == 0 {
		summary.First = summary.Answer
	}
	cp.AfterRun, cp.AfterRunTasks, cp.Phase = &summary, nil, "finish"
	task.Status = "running"
	checkpoint, err := dtoObject(cp, tx.limits)
	if err != nil {
		return false, err
	}
	task.Checkpoint = checkpoint
	// No decided outcome exists yet: finish must still be able to own children.
	metadata := &BuiltinTaskExecution{}
	if task.Execution != nil && task.Execution.Builtin != nil {
		*metadata = *task.Execution.Builtin
	}
	metadata.Hold = nil
	task.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: metadata}
	return true, nil
}
func (h *Harness) runAfterRun(r *TaskRuntime, task Task, cp generationCheckpoint) error {
	if cp.AfterRun == nil {
		return reject("afterRun summary missing")
	}
	if cp.Abort || taskAborted(task) || cp.Phase == "drain" {
		return h.endAfterRun(r, task, cp)
	}
	hooks, err := afterRunHooks(r)
	if err != nil {
		return err
	}
	var prepared []preparedRunEndTask
	for _, hook := range hooks {
		if hook.AfterRun == nil {
			continue
		}
		result, err := callAfterRun(r.context, hook.AfterRun, *cp.AfterRun, r)
		if err == nil && result != nil {
			err = prepareRunEndTasks(r, result.Tasks, &prepared)
		}
		if err != nil {
			if r.context.Err() != nil {
				return r.context.Err()
			}
			r.reportExtensionError(err, hook.extension)
		}
	}
	if len(prepared) == 0 {
		return h.endAfterRun(r, task, cp)
	}
	_, err = h.session.invocationCommit(r.context, task.ID, func(tx *Tx) error {
		if err := r.check(); err != nil {
			return err
		}
		if h.closing.Load() || r.context.Err() != nil {
			return ErrSealed
		}
		current := tx.state.Tasks[task.ID]
		if taskAborted(current) || taskHasDecidedOutcome(current) {
			return ErrSealed
		}
		var latest generationCheckpoint
		if err := fromObject(current.Checkpoint, &latest, tx.limits); err != nil {
			return err
		}
		if latest.Phase != "finish" || latest.AfterRun == nil {
			return ErrSealed
		}
		children := make([]ID, 0, len(prepared))
		for _, request := range prepared {
			id, err := tx.createTask(request.definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: task.ID}}, request.execution)
			if err != nil {
				return err
			}
			children = append(children, id)
		}
		latest.Phase, latest.AfterRunTasks = "drain", children
		checkpoint, err := dtoObject(latest, tx.limits)
		if err != nil {
			return err
		}
		current.Checkpoint = checkpoint
		if err := tx.stage(Write{Op: "put-task", Task: &current}); err != nil {
			return err
		}
		live, err := builtin(tx, task.Conversation, "pi.live")
		if err != nil {
			return err
		}
		return live.Update(func(value JSON) error {
			run := generationObject(value["run"])
			if submissionEntryID(run["task"]) == task.ID {
				run["afterRun"] = JSON{"tasks": children}
			}
			return nil
		})
	})
	return err
}
func (h *Harness) endAfterRun(r *TaskRuntime, task Task, cp generationCheckpoint) error {
	return h.endAfterRunWithQueue(r, task, cp, false)
}
func (h *Harness) endAfterRunWithQueue(r *TaskRuntime, task Task, cp generationCheckpoint, keepQueued bool) error {
	_, err := h.session.invocationCommit(r.context, task.ID, func(tx *Tx) error {
		if err := r.check(); err != nil {
			return err
		}
		if h.closing.Load() || r.context.Err() != nil {
			return ErrSealed
		}
		current := tx.state.Tasks[task.ID]
		if terminalStatus(current.Status) || taskHasDecidedOutcome(current) {
			return nil
		}
		var latest generationCheckpoint
		if err := fromObject(current.Checkpoint, &latest, tx.limits); err != nil {
			return err
		}
		if latest.AfterRun == nil {
			return reject("afterRun summary missing")
		}
		latest.Phase = "terminal"
		checkpoint, err := dtoObject(latest, tx.limits)
		if err != nil {
			return err
		}
		current.Checkpoint, current.Status = checkpoint, "done"
		hold := &BuiltinTaskHold{Stage: "final", Action: "generation-receipt", Outcome: TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: JSON{"entryId": latest.AfterRun.Answer}}}, FinalStatus: "done", Entry: latest.AfterRun.Answer, Submission: latest.Submission, Conversation: task.Conversation, Owner: task.Owner, KeepQueued: keepQueued}
		metadata := &BuiltinTaskExecution{}
		if current.Execution != nil && current.Execution.Builtin != nil {
			*metadata = *current.Execution.Builtin
		}
		metadata.Hold, metadata.Memos = hold, nil
		current.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: metadata}
		state, err := tx.current()
		if err != nil {
			return err
		}
		if len(h.scheduler.ownedLive(state, current.ID)) > 0 {
			hold.Stage, current.Status = "held", "completing"
		}
		entry, ok := state.Entries[hold.Entry]
		if !ok {
			return reject("afterRun answer missing")
		}
		if _, err := h.cleanupGeneration(tx, &current, hold, entry.Value); err != nil {
			return err
		}
		return tx.stage(Write{Op: "put-task", Task: &current})
	})
	return err
}
