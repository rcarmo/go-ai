package durable

import (
	"context"
	"fmt"

	goai "github.com/rcarmo/go-ai"
)

func invokeDecisionHook(call func() (*HookTasksResult, error)) (result *HookTasksResult, err error) {
	defer func() {
		if recover() != nil {
			err = reject("generation decision hook panic")
		}
	}()
	return call()
}

func createGenerationHookTasks(tx *Tx, r *TaskRuntime, prepared []preparedRunEndTask) ([]ID, []any, error) {
	ids := make([]ID, 0, len(prepared))
	values := make([]any, 0, len(prepared))
	for _, item := range prepared {
		id, err := tx.createTask(item.definition, item.execution.Input.Value, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.taskID}}, item.execution)
		if err != nil {
			return nil, nil, err
		}
		ids, values = append(ids, id), append(values, id)
	}
	return ids, values, nil
}

func generationTaskIDs(value any) []ID {
	raw, _ := value.([]any)
	ids := make([]ID, len(raw))
	for i, id := range raw {
		ids[i] = submissionEntryID(id)
	}
	return ids
}

func canonicalGenerationAfterTools(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
	if task.Restarts >= r.harness.RestartLimit() {
		return canonicalGenerationCutOff(ctx, task, r)
	}
	cp := taskCheckpointMap(task.State.Checkpoint)
	ids := generationTaskIDs(cp["tasks"])
	outcomes, err := r.Outcomes(ctx, ids)
	if err != nil {
		return err
	}
	terminate := false
	for index, outcome := range outcomes {
		if outcome.Status != "completed" || outcome.Result == nil || outcome.Result.Value == nil {
			continue
		}
		decision := generationObject(outcome.Result.Value)
		if len(decision) == 1 && decision["terminate"] == true {
			terminate = true
			continue
		}
		if decision != nil && len(decision) == 0 {
			continue
		}
		child, _, err := r.Task(ctx, ids[index])
		if err != nil {
			return err
		}
		extension := r.registry.installer(child.Kind, r.registry.Task(child.Kind))
		r.reportExtensionError(fmt.Errorf("afterTools task %d (%s) completed with no AfterToolsDecision", ids[index], child.Kind), extension)
	}
	return canonicalGenerationToolRound(ctx, r, cp, false, terminate)
}

func canonicalGenerationParkYield(ctx context.Context, r *TaskRuntime, message MessageReceipt, prepared []preparedRunEndTask) error {
	return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		doc, live, err := canonicalGenerationLive(tx, r)
		if err != nil {
			return nil, err
		}
		answer, err := canonicalAppendAssistant(tx, r.conversation, message)
		if err != nil {
			return nil, err
		}
		tasks, values, err := createGenerationHookTasks(tx, r, prepared)
		if err != nil {
			return nil, err
		}
		delete(live, "generation")
		if run := generationObject(live["run"]); submissionEntryID(run["taskId"]) == r.taskID {
			run["yield"] = JSON{"answer": answer, "tasks": values}
		}
		if err := doc.Set(live); err != nil {
			return nil, err
		}
		return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "yield", "answer": answer, "tasks": values}, On: tasks, Policy: "allSettled"}, nil
	})
}

func canonicalGenerationYield(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
	if task.Restarts >= r.harness.RestartLimit() {
		return canonicalGenerationCutOff(ctx, task, r)
	}
	cp := taskCheckpointMap(task.State.Checkpoint)
	outcomes, err := r.Outcomes(ctx, generationTaskIDs(cp["tasks"]))
	if err != nil {
		return err
	}
	var continuation *Input
	for _, outcome := range outcomes {
		if outcome.Status == "completed" && outcome.Result != nil {
			continuation = generationYieldInput(generationObject(outcome.Result.Value)["continue"])
			if continuation != nil {
				break
			}
		}
	}
	hooks, err := afterRunHooks(r)
	if err != nil {
		return err
	}
	after := false
	for _, hook := range hooks {
		after = after || hook.AfterRun != nil
	}
	return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		boundary, err := prepareCanonicalBoundary(tx, r.conversation, r.harness.resolvedSettings(RequestSettings{}))
		if err != nil {
			return nil, err
		}
		doc, live, err := canonicalGenerationLive(tx, r)
		if err != nil {
			return nil, err
		}
		return concludeCanonicalAnswer(tx, r, doc, live, boundary, submissionEntryID(cp["answer"]), continuation, after)
	})
}

func generationYieldInput(value any) *Input {
	if text, ok := value.(string); ok {
		return &Input{Content: text}
	}
	items, _ := value.([]any)
	if len(items) == 0 {
		return nil
	}
	blocks := make([]goai.ContentBlock, 0, len(items))
	for _, item := range items {
		value := generationObject(item)
		switch value["type"] {
		case "text":
			text, ok := value["text"].(string)
			if !ok {
				return nil
			}
			blocks = append(blocks, goai.ContentBlock{Type: "text", Text: text})
		case "image":
			data, dataOK := value["data"].(string)
			mime, mimeOK := value["mimeType"].(string)
			if !dataOK || !mimeOK {
				return nil
			}
			blocks = append(blocks, goai.ContentBlock{Type: "image", Data: data, MimeType: mime})
		default:
			return nil
		}
	}
	return &Input{Blocks: blocks}
}
