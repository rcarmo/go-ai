package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
)

func runNestedAfterHooks(ctx context.Context, r *TaskRuntime, agent agentState, input nestedToolInput, args JSON, result ToolResult) (ToolResult, error) {
	for _, hooks := range r.phaseSelection().selectedToolHooks(r.harness.selectionAgent(agent)) {
		if hooks.AfterTool == nil {
			continue
		}
		detached, err := detachToolResult(result, r.harness.session.limits)
		if err != nil {
			return ToolResult{Content: "Invalid tool result", IsError: true}, nil
		}
		copy, err := copyObject(args, r.harness.session.limits)
		if err != nil {
			return ToolResult{}, err
		}
		replacement, err := callAfterTool(ctx, hooks.AfterTool, goai.ToolCall{ID: input.Key, Name: input.Name, Arguments: map[string]any(copy)}, detached, r)
		if err != nil {
			if ctx.Err() != nil {
				return ToolResult{}, ctx.Err()
			}
			r.harness.scheduler.report(err)
			continue
		}
		if replacement != nil {
			owned, err := detachToolResult(*replacement, r.harness.session.limits)
			if err != nil {
				return ToolResult{Content: "Invalid tool result", IsError: true}, nil
			}
			result = owned
		}
	}
	return result, nil
}

func nestedProgressResult(record TaskRecord, limits Limits) ToolResult {
	value, ok := record.State.Checkpoint["progress"].(map[string]any)
	if !ok {
		return ToolResult{}
	}
	var progress toolCheckpoint
	if err := fromObject(JSON(value), &progress, limits); err != nil {
		return ToolResult{Content: "Invalid nested progress", IsError: true}
	}
	result := ToolResult{Content: progress.Output, DetailsValue: progress.Details, HasDetails: progress.Details != nil, Diagnostics: progress.Diagnostics}
	if value, ok := progress.Details.(map[string]any); ok {
		result.Details = JSON(value)
	}
	return result
}
