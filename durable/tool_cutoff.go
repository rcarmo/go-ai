package durable

import "fmt"

// f9 tool.ts endCutOff/afterHarnessResult. Exactly at the limit the slot's
// result still passes hooks; beyond it a hook may itself be the crash loop.
func (h *Harness) endToolCutoff(task Task, cp toolCheckpoint, r *TaskRuntime, agent agentState) error {
	message := fmt.Sprintf("Tool %s was interrupted %s in a row; it is not run again", cp.Offer.Name, cutoffCount(task.Restarts))
	cp.CutoffMessage = message
	result := ToolResult{Content: cp.Output, IsError: true, usesRetainedOutput: true, usesReportedDiagnostics: true, Diagnostics: append(append([]ToolDiagnostic{}, cp.Diagnostics...), ToolDiagnostic{Severity: "error", Code: "restart_limit", Message: message})}
	if cp.HasDetails {
		result.DetailsValue, result.HasDetails = cp.Details, true
	}
	if task.Restarts <= h.RestartLimit() {
		registry := r.registry.selection
		if registry == nil {
			registry = NewRegistry()
		}
		// Resolution failure uses defaults; hooks still run on the harness result.
		cp.Offer.OutputLimits = nil
		if _, pins, _, _, err := registry.selectedGeneration(h.selectionAgent(agent), h.session.limits, r.reportError); err == nil {
			if tool, ok := pins[cp.Offer.Name]; ok && toolCallerAllowed(cp.Offer, "model") {
				cp.Offer.OutputLimits = tool.offer.OutputLimits
			}
		}
		for _, hook := range registry.selectedToolHooks(h.selectionAgent(agent)) {
			if hook.AfterTool == nil {
				continue
			}
			owned, err := detachToolResult(result, h.session.limits)
			if err != nil {
				return &invalidToolResult{reason: "invalid_tool_result"}
			}
			args, err := copyScopedToolArguments(cp.Arguments, cp.StreamingArguments, h.session.limits)
			if err != nil {
				return err
			}
			next, err := callAfterTool(r.context, hook.AfterTool, cp.argumentCall(args), owned, r)
			if err != nil {
				if r.context.Err() != nil {
					return r.context.Err()
				}
				r.reportExtensionError(err, hook.extension)
				continue
			}
			if next != nil {
				retained := result.usesRetainedOutput && next.Content == result.Content && equalJSONValue(next.Blocks, result.Blocks)
				result, err = detachToolResult(*next, h.session.limits)
				if err != nil {
					return &invalidToolResult{reason: "invalid_tool_result"}
				}
				result.usesRetainedOutput = retained
			}
		}
	}
	// These fallbacks were applied before the hook, not again after replacement.
	cp.HasDetails, cp.Details = false, nil
	result.usesReportedDiagnostics = true
	result.harnessResult = true
	result.StructuredOutput, result.HasStructuredOutput = nil, false
	err := h.finishTool(task, cp, result, "restart_limit")
	if err == nil {
		r.reportCutoff(task.Restarts, message)
	}
	return err
}
