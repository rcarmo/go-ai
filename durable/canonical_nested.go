package durable

import (
	"context"
	"fmt"
	goai "github.com/rcarmo/go-ai"
)

func (r *TaskRuntime) startCanonicalTool(ctx context.Context, key, name string, args JSON, sequential, native bool) (*NestedToolHandle, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	l := r.harness.session.limits
	if (!sequential && !validNestedKey(key) || sequential && !validSequentialKey(key)) || len(key) > l.MaxRequestIDBytes || !validToolName(name) {
		return nil, reject("invalid nested call key/name")
	}
	owned, err := copyScopedToolArguments(args, native, l)
	if err != nil {
		return nil, err
	}
	current, found, err := r.Task(ctx, r.taskID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrSealed
	}
	_, parentCall, err := readCanonicalTool(ctx, current, r)
	if err != nil {
		return nil, err
	}
	// Source admits first; the child resolves its own callable tool in call.
	// Restart abandonment follows the caller's replay policy, not the callee's.
	abandon := taskCheckpointMap(current.State.Checkpoint)["replay"] != "safe"
	if _, err := r.MemoCandidate(ctx, "pi.tool.madeNestedCalls", true); err != nil {
		return nil, err
	}
	callID := parentCall.ID + "/" + key
	handle := &NestedToolHandle{runtime: r}
	replayed := false
	err = r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		doc, err := tx.AcquireDocument(canonicalNestedCallDoc, r.taskID, &key, nil)
		if err != nil {
			return nil, err
		}
		value, err := doc.Get()
		if err != nil {
			return nil, err
		}
		handle.document = doc.id
		if existing := submissionEntryID(value["taskId"]); existing != 0 {
			record, found, err := tx.Task(existing)
			if err != nil {
				return nil, err
			}
			var same canonicalToolInput
			if found && record.Input != nil {
				if err := fromObject(taskCheckpointMap(record.Input.Value), &same, l); err != nil {
					return nil, err
				}
			}
			if !found || same.Kind != "nested" || same.Call.Name != name || !equalJSONValue(same.Call.Arguments, owned) {
				//lint:ignore ST1005 Exact pinned tool.ts nested-call identity error.
				return nil, fmt.Errorf("Nested call %s was already made with another tool or other arguments", callID)
			}
			handle.task = existing
			replayed = value["result"] != nil
			return nil, nil
		}
		taskInput := JSON{"kind": "nested", "parent": r.taskID, "parentCallId": parentCall.ID, "key": key, "call": JSON{"type": "toolCall", "id": callID, "name": name, "arguments": owned}}
		id, err := tx.CreateTask(ToolTask(), taskInput, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.taskID}, AbandonOnRestart: abandon})
		if err != nil {
			return nil, err
		}
		value["taskId"] = id
		handle.task = id
		if err := doc.Set(value); err != nil {
			return nil, err
		}
		live, err := builtin(tx, r.conversation, "pi.live")
		if err != nil {
			return nil, err
		}
		return nil, live.Update(func(value JSON) error {
			slots, _ := value["nestedTools"].([]any)
			value["nestedTools"] = append(slots, JSON{"callId": callID, "parentCallId": parentCall.ID, "parentTaskId": r.taskID, "name": name, "taskId": id, "arguments": owned, "status": "pending"})
			return nil
		})
	})
	if err == nil && replayed {
		r.telemetryEvent("pi.durable.replay", map[string]any{"tool.name": name})
	}
	return handle, err
}
func (h *NestedToolHandle) waitCanonicalTool(ctx context.Context, record TaskRecord) (ToolResult, error) {
	var value JSON
	err := h.runtime.harness.session.readTasks(ctx, func(state Snapshot) error {
		if err := h.runtime.check(); err != nil {
			return err
		}
		doc, found := state.Documents[h.document]
		if !found || doc.Owner != h.runtime.taskID {
			return reject("nested result missing")
		}
		value = generationObject(doc.Value["result"])
		return nil
	})
	if err != nil {
		return ToolResult{}, err
	}
	if value == nil {
		name := ""
		if record.Input != nil {
			var input canonicalToolInput
			if fromObject(taskCheckpointMap(record.Input.Value), &input, h.runtime.harness.session.limits) == nil {
				name = input.Call.Name
			}
		}
		outcome := record.State.Outcome
		message := fmt.Sprintf("Tool %s ended without a result", name)
		status := "faulted"
		if outcome != nil {
			status = outcome.Status
			if outcome.Status == "faulted" && outcome.Error != nil {
				message = fmt.Sprintf("Tool %s failed: %s", name, outcome.Error.Message)
			} else if outcome.Status == "orphaned" {
				message = fmt.Sprintf("Tool %s could not resume: %s", name, outcome.Reason)
			}
		}
		return canonicalToolError(status, message), nil
	}
	result := ToolResult{}
	if structured, found := value["structuredOutput"]; found {
		result.StructuredOutput, result.HasStructuredOutput = structured, true
	}
	if details, found := value["details"]; found {
		result.DetailsValue, result.HasDetails = details, true
	}
	result.IsError, _ = value["isError"].(bool)
	var fields struct {
		Diagnostics []ToolDiagnostic `json:"diagnostics"`
		Usage       *goai.Usage      `json:"usage"`
	}
	selected := JSON{}
	for _, key := range []string{"diagnostics", "usage"} {
		if value, present := value[key]; present {
			selected[key] = value
		}
	}
	if err := fromObject(selected, &fields, h.runtime.harness.session.limits); err != nil {
		return ToolResult{}, err
	}
	result.Diagnostics, result.Usage = fields.Diagnostics, fields.Usage
	return result, nil
}
