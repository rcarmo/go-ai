package durable

import (
	"context"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ToolTask is the source pi.tool v3 token. Its durable state contains only the
// call/execute union; transient host selection and progress are not checkpoints.
var canonicalToolOnce sync.Once
var canonicalToolDefinition *TaskDefinition

func ToolTask() *TaskDefinition {
	canonicalToolOnce.Do(func() {
		var err error
		canonicalToolDefinition, err = DefineTask(TaskDefinitionOptions{Kind: "pi.tool", Version: 3, Initial: func(any) (JSON, error) { return JSON{"phase": "call"}, nil }, Migrate: func(input, checkpoint any, version uint64) (any, JSON, error) {
			if version == 1 {
				out := JSON{"kind": "model"}
				for key, value := range generationObject(input) {
					out[key] = value
				}
				input = out
			}
			return input, taskCheckpointMap(checkpoint), nil
		}, Phases: map[string]TaskPhase{"call": canonicalToolCallPhase, "execute": canonicalToolExecutePhase}, Abort: canonicalToolAbort})
		if err != nil {
			panic(err)
		}
	})
	return canonicalToolDefinition
}

type canonicalToolInput struct {
	Kind         string            `json:"kind"`
	Assistant    ID                `json:"assistant,omitempty"`
	CallID       string            `json:"callId,omitempty"`
	Unoffered    bool              `json:"unoffered,omitempty"`
	Parent       ID                `json:"parent,omitempty"`
	ParentCallID string            `json:"parentCallId,omitempty"`
	Key          string            `json:"key,omitempty"`
	Call         goai.ContentBlock `json:"call,omitempty"`
	Progress     *bool             `json:"progress,omitempty"`
}

func readCanonicalTool(ctx context.Context, task TaskRecord, r *TaskRuntime) (canonicalToolInput, goai.ToolCall, error) {
	var input canonicalToolInput
	if task.Input == nil {
		return input, goai.ToolCall{}, reject("task record input is absent")
	}
	if err := fromObject(taskCheckpointMap(task.Input.Value), &input, r.harness.session.limits); err != nil {
		return input, goai.ToolCall{}, err
	}
	block := input.Call
	if input.Kind != "nested" {
		entry, found, err := r.Entry(ctx, input.Assistant)
		if err != nil {
			return input, goai.ToolCall{}, err
		}
		foundCall := false
		if found && entry.Kind == assistantEntryKind && len(entry.Model) > 0 && entry.Model[0].Role == goai.RoleAssistant {
			for _, value := range entry.Model[0].Content {
				if value.Type == "toolCall" && value.ID == input.CallID {
					block = value
					foundCall = true
					break
				}
			}
		}
		if !foundCall {
			return input, goai.ToolCall{}, fmt.Errorf("Entry %d has no tool call %s", input.Assistant, input.CallID)
		}
	}
	call := goai.ToolCall{ID: block.ID, Name: block.Name, Arguments: block.Arguments}
	if block.HasStreamingArguments() {
		call.SetStreamingArguments(block.Arguments, block.ArgumentsError)
	}
	r.toolTelemetry(call.Name, call.ID)
	return input, call, nil
}

func canonicalToolSelection(ctx context.Context, r *TaskRuntime, input canonicalToolInput, call goai.ToolCall) (agentState, registeredTool, bool, error) {
	selected, err := r.Agent(ctx)
	if err != nil {
		return agentState{}, registeredTool{}, false, err
	}
	state, err := r.harness.agent(selected.Configuration)
	if err != nil {
		return state, registeredTool{}, false, err
	}
	if input.Kind == "model" && input.Unoffered {
		return state, registeredTool{}, false, nil
	}
	var pins map[string]registeredTool
	if input.Kind == "nested" {
		_, pins, _, _, err = r.phaseSelection().selectedTools(r.harness.selectionAgent(state), r.harness.session.limits, false, r.reportError)
	} else {
		_, pins, _, _, err = r.phaseSelection().selectedGeneration(r.harness.selectionAgent(state), r.harness.session.limits, r.reportError)
	}
	reg, found := pins[call.Name]
	caller := "model"
	if input.Kind == "nested" {
		caller = "tools"
	}
	return state, reg, found && toolCallerAllowed(reg.offer, caller), err
}

func canonicalToolError(code, message string) ToolResult {
	diagnostic := ToolDiagnostic{Severity: "error", Code: code, Message: message}
	// Preparation/validation errors are generated in the same argument domain
	// as parser diagnostics; preserve an original isolated UTF-16 message.
	if code == "invalid_arguments" && !utf8.ValidString(message) {
		diagnostic.argumentError = true
	}
	return ToolResult{IsError: true, Blocks: []goai.ContentBlock{}, harnessResult: true, Diagnostics: []ToolDiagnostic{diagnostic}}
}

func canonicalToolHooks(ctx context.Context, r *TaskRuntime, agent agentState, call goai.ToolCall, result ToolResult) (ToolResult, error) {
	for _, hook := range r.phaseSelection().selectedToolHooks(r.harness.selectionAgent(agent)) {
		if hook.AfterTool == nil {
			continue
		}
		owned, err := detachToolResult(result, r.harness.session.limits)
		if err != nil {
			return ToolResult{}, err
		}
		next, err := callAfterTool(ctx, hook.AfterTool, call, owned, r)
		if err != nil {
			if ctx.Err() != nil {
				return ToolResult{}, ctx.Err()
			}
			r.reportExtensionError(err, hook.extension)
			continue
		}
		if next != nil {
			// A copied hook result retains the omitted-output arm only while
			// it keeps that output; replacing it must discard its truncation.
			retained := result.usesRetainedOutput && next.usesRetainedOutput && next.Content == result.Content && next.Blocks == nil && result.Blocks == nil
			result, err = detachToolResult(*next, r.harness.session.limits)
			result.usesRetainedOutput = retained
			if err != nil {
				return ToolResult{}, err
			}
		}
	}
	return result, nil
}

func canonicalToolCallPhase(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
	input, call, err := readCanonicalTool(ctx, task, r)
	if err != nil {
		return err
	}
	agent, reg, found, err := canonicalToolSelection(ctx, r, input, call)
	if err != nil {
		return err
	}
	if task.Restarts >= r.harness.RestartLimit() {
		return canonicalToolCutOff(ctx, task, r, input, call, agent, reg)
	}
	rejectCall := func(result ToolResult) error {
		result, err := canonicalToolHooks(ctx, r, agent, call, result)
		if err != nil {
			return err
		}
		result.harnessResult = true
		return settleCanonicalTool(ctx, r, input, call, reg, result, "completed", "", nil)
	}
	if !found {
		return rejectCall(canonicalToolError("tool_unavailable", fmt.Sprintf("Tool %s is not available", call.Name)))
	}
	if call.ArgumentsError != "" {
		return rejectCall(ToolResult{IsError: true, harnessResult: true, Blocks: []goai.ContentBlock{}, Diagnostics: []ToolDiagnostic{{Severity: "error", Code: "invalid_arguments", Message: fmt.Sprintf(`Invalid arguments for tool "%s": %s`, call.Name, call.ArgumentsError), argumentError: true}}})
	}
	args, err := validateScopedArguments(ctx, reg, call.Arguments, r.harness.session.limits, call.HasStreamingArguments())
	if err != nil {
		return rejectCall(canonicalToolError("invalid_arguments", err.Error()))
	}
	for _, hook := range r.phaseSelection().selectedToolHooks(r.harness.selectionAgent(agent)) {
		if hook.BeforeTool == nil {
			continue
		}
		copy, err := copyScopedToolValue(args, call.HasStreamingArguments(), r.harness.session.limits)
		if err != nil {
			return err
		}
		checked := call
		checked.Arguments = toolCallArgumentValue(copy)
		decision, err := callBeforeTool(ctx, hook.BeforeTool, checked, r)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return rejectCall(canonicalToolError("blocked", "Tool call blocked: "+err.Error()))
		}
		if decision != nil {
			if decision.Block != "" {
				return rejectCall(canonicalToolError("blocked", "Tool call blocked: "+decision.Block))
			}
			if decision.Arguments != nil {
				args = decision.Arguments
			}
		}
	}
	checked := reg
	checked.prepare = nil
	args, err = validateScopedArguments(ctx, checked, args, r.harness.session.limits, call.HasStreamingArguments())
	if err != nil {
		return rejectCall(canonicalToolError("invalid_arguments", err.Error()))
	}
	if err := r.commitProgress(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		return nil, canonicalToolSlotUpdate(tx, r.taskID, r.conversation, func(slot JSON) {
			slot["status"] = "running"
			if input.Kind == "nested" {
				slot["arguments"] = args
			}
		})
	}); err != nil {
		return err
	}
	replay := "unsafe"
	if reg.offer.ReplaySafe {
		replay = "safe"
	}
	var checkpointArguments any = args
	if call.HasStreamingArguments() {
		checkpointArguments = sourceToolArguments{args}
	}
	if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
		return &TaskState{Status: "running", Checkpoint: JSON{"phase": "execute", "arguments": checkpointArguments, "replay": replay}}, nil
	}); err != nil {
		return err
	}
	return runCanonicalTool(ctx, r, input, call, agent, reg, args)
}

func canonicalToolExecutePhase(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
	input, call, err := readCanonicalTool(ctx, task, r)
	if err != nil {
		return err
	}
	agent, reg, found, err := canonicalToolSelection(ctx, r, input, call)
	if err != nil {
		return err
	}
	if task.Restarts >= r.harness.RestartLimit() {
		return canonicalToolCutOff(ctx, task, r, input, call, agent, reg)
	}
	cp := taskCheckpointMap(task.State.Checkpoint)
	if cp["replay"] == "safe" && found && reg.offer.ReplaySafe {
		args := unwrapToolArgumentValue(cp["arguments"])
		// Recovery removes stale output durably before another effect. This
		// source Runtime.commit is immediate, not a progress publication.
		if err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
			return nil, canonicalToolSlotUpdate(tx, r.taskID, r.conversation, func(slot JSON) {
				for _, key := range []string{"output", "details", "diagnostics", "droppedBytes", "droppedLines"} {
					delete(slot, key)
				}
				slot["status"] = "running"
				if input.Kind == "nested" {
					slot["arguments"] = args
				}
			})
		}); err != nil {
			return err
		}
		return runCanonicalTool(ctx, r, input, call, agent, reg, args)
	}
	message := fmt.Sprintf("Tool %s was interrupted and may have partially run", call.Name)
	result, err := canonicalToolFromSlot(ctx, r, "interrupted", message)
	if err != nil {
		return err
	}
	result, err = canonicalToolHooks(ctx, r, agent, call, result)
	if err != nil {
		return err
	}
	return settleCanonicalTool(ctx, r, input, call, reg, result, "failed", message, nil)
}

func canonicalToolAbort(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
	input, call, err := readCanonicalTool(ctx, task, r)
	if err != nil {
		return err
	}
	code, message := "aborted", fmt.Sprintf("Tool %s was aborted", call.Name)
	if input.Unoffered {
		code, message = "tool_unavailable", fmt.Sprintf("Tool %s is not available", call.Name)
	} else if task.AbortReason == "restart" {
		code, message = "abandoned", fmt.Sprintf("Tool %s was not started: its caller ended with a restart", call.Name)
		if taskCheckpointMap(task.State.Checkpoint)["phase"] == "execute" {
			code, message = "interrupted", fmt.Sprintf("Tool %s was interrupted by a restart and may have partially run", call.Name)
		}
	}
	result, err := canonicalToolFromSlot(ctx, r, code, message)
	if err != nil {
		return err
	}
	var reg registeredTool
	if task.Restarts+1 < r.harness.RestartLimit() {
		agent, selected, _, selectErr := canonicalToolSelection(ctx, r, input, call)
		if selectErr == nil {
			reg = selected
			result, err = canonicalToolHooks(ctx, r, agent, call, result)
			if err != nil {
				return err
			}
		}
	}
	return settleCanonicalTool(ctx, r, input, call, reg, result, "aborted", "", nil)
}
func canonicalToolCutOff(ctx context.Context, task TaskRecord, r *TaskRuntime, input canonicalToolInput, call goai.ToolCall, agent agentState, reg registeredTool) error {
	message := fmt.Sprintf("Tool %s was interrupted %s; it is not run again", call.Name, canonicalCutOffCount(task.Restarts))
	result, err := canonicalToolFromSlot(ctx, r, "restart_limit", message)
	if err != nil {
		return err
	}
	if task.Restarts <= r.harness.RestartLimit() {
		result, err = canonicalToolHooks(ctx, r, agent, call, result)
		if err != nil {
			return err
		}
	}
	return settleCanonicalTool(ctx, r, input, call, reg, result, "failed", message, nil, "restart_limit")
}

func canonicalToolSlot(value JSON, id ID) JSON {
	for _, key := range []string{"tools", "nestedTools"} {
		items, _ := value[key].([]any)
		for _, raw := range items {
			slot := generationObject(raw)
			if submissionEntryID(slot["taskId"]) == id {
				return slot
			}
		}
	}
	return nil
}
func canonicalToolSlotUpdate(tx *Tx, id, conversation ID, change func(JSON)) error {
	live, err := builtin(tx, conversation, "pi.live")
	if err != nil {
		return err
	}
	return live.Update(func(value JSON) error {
		if slot := canonicalToolSlot(value, id); slot != nil {
			change(slot)
		}
		return nil
	})
}
func canonicalToolFromSlot(ctx context.Context, r *TaskRuntime, code, message string) (ToolResult, error) {
	result := canonicalToolError(code, message)
	err := r.harness.session.readTasks(ctx, func(state Snapshot) error {
		for _, doc := range state.Documents {
			if doc.Kind == "pi.live" && doc.Owner == r.conversation && !doc.Retired {
				slot := canonicalToolSlot(doc.Value, r.taskID)
				if slot != nil {
					result.Content, _ = slot["output"].(string)
					if result.Content != "" {
						result.Blocks = []goai.ContentBlock{{Type: "text", Text: result.Content}}
					}
					if details, found := slot["details"]; found {
						result.DetailsValue, result.HasDetails = details, true
					}
					if raw, found := slot["diagnostics"]; found {
						var value struct {
							Items []ToolDiagnostic `json:"items"`
						}
						if err := fromObject(JSON{"items": raw}, &value, r.harness.session.limits); err != nil {
							return err
						}
						result.Diagnostics = append(value.Items, result.Diagnostics...)
					}
				}
				break
			}
		}
		return nil
	})
	return result, err
}

func runCanonicalTool(ctx context.Context, r *TaskRuntime, input canonicalToolInput, call goai.ToolCall, agent agentState, reg registeredTool, args any) (finalErr error) {
	api := &ToolAPI{h: r.harness, task: Task{ID: r.taskID, Conversation: r.conversation}, checkpoint: toolCheckpoint{Offer: reg.offer, CallID: call.ID}, active: true, ctx: ctx, runtime: r, canonicalInput: input}
	interval := int64(100)
	if p := r.harness.resolvedSettings(RequestSettings{}).Progress; p.OutputIntervalMs != nil {
		interval = *p.OutputIntervalMs
	}
	api.progress = newRuntimeToolProgress(api.publishCanonicalProgress, func(err error) {
		if ctx.Err() == nil {
			r.reportError(err)
		}
	}, r, progressInterval(interval))
	defer func() { settleProgress(api.pendingProgress, finalErr) }()
	api.environment, api.environmentErr = resolveInvocationEnvironment(ctx, r.harness, r, r.conversation, true)
	api.environmentResolved = true
	var result ToolResult
	err := api.environmentErr
	var duration *int64
	if err == nil {
		owned, copyErr := copyScopedToolValue(args, call.HasStreamingArguments(), r.harness.session.limits)
		err = copyErr
		if err == nil {
			started := time.Now()
			func() {
				defer func() {
					if value := recover(); value != nil {
						err = fmt.Errorf("%v", value)
					}
				}()
				result, err = reg.execute(ctx, owned, api)
			}()
			elapsed := time.Since(started).Round(time.Millisecond).Milliseconds()
			duration = &elapsed
		}
	}
	executionFailed := err != nil
	if sealErr := api.seal(); err == nil {
		err = sealErr
	}
	// Source aborts immediately when execute/env throws after its signal was
	// cancelled. Only a normal late return proceeds through finalResult hooks.
	if executionFailed && ctx.Err() != nil {
		return ctx.Err()
	}
	api.mu.Lock()
	if result.Content == "" && result.Blocks == nil {
		result.Content = api.checkpoint.Output
		result.usesRetainedOutput = true
	}
	droppedBytes, droppedLines := api.checkpoint.DroppedBytes, api.checkpoint.DroppedLines
	if result.Details == nil && !result.HasDetails && api.checkpoint.HasDetails {
		result.DetailsValue, result.HasDetails = api.checkpoint.Details, true
	}
	result.Diagnostics = append(append([]ToolDiagnostic{}, api.checkpoint.Diagnostics...), result.Diagnostics...)
	api.mu.Unlock()
	if err != nil {
		result.IsError = true
		result.Diagnostics = append(result.Diagnostics, ToolDiagnostic{Severity: "error", Code: "tool_error", Message: err.Error()})
	}
	ending, message := "completed", ""
	if err != nil {
		ending, message = "failed", fmt.Sprintf("Tool %s threw", call.Name)
	}
	result, err = canonicalToolHooks(ctx, r, agent, call, result)
	if err != nil {
		return err
	}
	// Source finalResult still runs afterTool for a tool returning after abort.
	// The cancellation fence prevents its result from being committed afterwards.
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Source finalResult adds retained-output truncation after afterTool, and
	// only while the hook kept that output. Returned content replaces progress.
	if result.usesRetainedOutput && droppedBytes > 0 {
		result.Diagnostics = append(result.Diagnostics, truncatedToolOutput(droppedBytes, droppedLines, resolvedToolOutputLimits(reg.offer.OutputLimits).Retain))
	}
	return settleCanonicalTool(ctx, r, input, call, reg, result, ending, message, duration)
}

func (a *ToolAPI) publishCanonicalProgress() (int, error) {
	a.mu.Lock()
	value, err := dtoObject(a.checkpoint, a.h.session.limits)
	a.mu.Unlock()
	if err != nil {
		return 0, err
	}
	var cp toolCheckpoint
	if err := fromObject(value, &cp, a.h.session.limits); err != nil {
		return 0, err
	}
	if a.canonicalInput.Kind == "nested" && a.canonicalInput.Progress != nil && !*a.canonicalInput.Progress {
		return 0, nil
	}
	written := 0
	err = a.runtime.commitProgress(a.ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		var copyErr error
		err := canonicalToolSlotUpdate(tx, a.task.ID, a.task.Conversation, func(slot JSON) {
			previous, _ := slot["output"].(string)
			if strings.HasPrefix(cp.Output, previous) {
				written += len(cp.Output) - len(previous)
			} else {
				written += len(cp.Output)
			}
			for _, key := range []string{"details", "diagnostics"} {
				if !equalJSONValue(slot[key], value[key]) {
					bytes, err := encodeBounded(value[key], tx.limits, tx.limits.MaxDocumentBytes)
					if err != nil {
						copyErr = err
						return
					}
					written += len(bytes)
				}
			}
			setToolPresentation(slot, value, cp.HasDetails)
		})
		if err == nil {
			err = copyErr
		}
		return nil, err
	})
	return written, err
}

func settleCanonicalTool(ctx context.Context, r *TaskRuntime, input canonicalToolInput, call goai.ToolCall, reg registeredTool, result ToolResult, ending, message string, duration *int64, failureReason ...string) error {
	l := r.harness.session.limits
	owned, err := detachToolResult(result, l)
	if err != nil {
		return err
	}
	if err := validateToolDiagnostics(owned.Diagnostics, l); err != nil {
		return err
	}
	if err := validateToolControl(owned.Control, l); err != nil {
		return err
	}
	limits := resolvedToolOutputLimits(reg.offer.OutputLimits)
	content := owned.Blocks
	if content == nil {
		content = []goai.ContentBlock{}
		if owned.Content != "" {
			content = []goai.ContentBlock{{Type: "text", Text: owned.Content}}
		}
	}
	var original strings.Builder
	for _, block := range content {
		if block.Type == "text" {
			original.WriteString(block.Text)
		}
	}
	bounded := boundToolText(original.String(), limits)
	if len(bounded) < original.Len() {
		owned.Diagnostics = append(owned.Diagnostics, truncatedToolOutput(uint64(original.Len()-len(bounded)), outputLines(original.String())-outputLines(bounded), limits.Retain))
	}
	content = boundToolBlocks(content, limits)
	if input.Kind == "nested" {
		if result.harnessResult {
			owned.StructuredOutput, owned.HasStructuredOutput = nil, false
		}
		if reg.offer.StructuredOutputSchema == nil {
			if !result.harnessResult || !owned.IsError {
				owned.StructuredOutput = nestedOutputValue(ToolResult{Blocks: content})
				owned.HasStructuredOutput = true
			}
		} else if !result.harnessResult {
			owned, _ = validateNestedStructuredResult(owned, reg.offer, l)
		}
	} else {
		owned = r.harness.validateModelStructuredResult(owned, reg.offer, func(err error) { r.reportExtensionError(err, reg.extension) })
	}
	if len(owned.Diagnostics) > 0 {
		block := goai.ContentBlock{Type: "text", Text: renderToolDiagnostics(owned.Diagnostics)}
		for _, diagnostic := range owned.Diagnostics {
			if diagnostic.argumentError {
				block.SetToolArgumentDiagnostic(block.Text)
				break
			}
		}
		content = append(content, block)
	}
	// Keep non-text output verbatim (tool.ts finalResult/boundContent). Large
	// transcript images use conversation-owned chunks, just as codemode output
	// does, so the bounded entry envelope never drops their payload.
	if input.Kind == "model" && len(owned.imageChunks) == 0 {
		content, owned.imageChunks, err = r.chunkTranscriptImages(ctx, content)
		if err != nil {
			return err
		}
	}
	// Abort every live nested child before waiting on any sibling.
	children, err := r.OwnedTasks(ctx)
	if err != nil {
		return err
	}
	for _, child := range children {
		if child.Kind == "pi.tool" && generationObject(child.Input.Value)["kind"] == "nested" {
			if _, err := r.harness.AbortTask(ctx, child.ID); err != nil {
				return err
			}
		}
	}
	return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		if owned.Commit != nil {
			if err := callToolOutcome(owned.Commit, tx); err != nil {
				return nil, err
			}
		}
		var settled JSON
		if input.Kind == "nested" {
			diagnostics, err := dtoObject(toolCheckpoint{Diagnostics: owned.Diagnostics}, l)
			if err != nil {
				return nil, err
			}
			items := diagnostics["diagnostics"]
			if items == nil {
				items = []any{}
			}
			nested := JSON{"taskId": r.taskID, "isError": owned.IsError, "diagnostics": items}
			if duration != nil {
				nested["durationMs"] = *duration
			}
			if owned.HasStructuredOutput {
				nested["structuredOutput"] = owned.StructuredOutput
			}
			if owned.HasDetails {
				nested["details"] = owned.DetailsValue
			} else if owned.Details != nil {
				nested["details"] = owned.Details
			}
			if owned.Usage != nil {
				usage, err := dtoObject(*owned.Usage, l)
				if err != nil {
					return nil, err
				}
				nested["usage"] = usage
			}
			doc, err := tx.AcquireDocument(canonicalNestedCallDoc, input.Parent, &input.Key, nil)
			if err != nil {
				return nil, err
			}
			value, err := doc.Get()
			if err != nil {
				return nil, err
			}
			if submissionEntryID(value["taskId"]) != r.taskID {
				//lint:ignore ST1005 Exact pinned tool.ts nested-index error.
				return nil, fmt.Errorf("Nested call %s is not in its caller's index", call.ID)
			}
			value["result"] = nested
			if err := doc.Set(value); err != nil {
				return nil, err
			}
			settled = JSON{"kind": "nested"}
		} else {
			receipt := MessageReceipt{ImageChunks: owned.imageChunks, Role: goai.RoleToolResult, ToolCallID: call.ID, ToolName: call.Name, Content: content, IsError: owned.IsError, Diagnostics: owned.Diagnostics, Timestamp: r.harness.session.now(), DurationMs: duration, Details: owned.Details, DetailsValue: owned.DetailsValue, HasDetails: owned.HasDetails, Usage: owned.Usage}
			for _, diagnostic := range owned.Diagnostics {
				if diagnostic.argumentError {
					receipt.ErrorCode = "invalid_arguments"
					break
				}
			}
			wire, err := dtoObject(receipt, l)
			if err != nil {
				return nil, err
			}
			id, err := tx.mintID()
			if err != nil {
				return nil, err
			}
			if err := tx.appendMessage(id, r.conversation, r.taskID, wire); err != nil {
				return nil, err
			}
			settled = JSON{"kind": "model", "entryId": id}
			if owned.Control != nil && ending == "completed" {
				control, err := dtoObject(*owned.Control, l)
				if err != nil {
					return nil, err
				}
				settled["control"] = control
			}
		}
		if owned.Usage != nil {
			doc, err := builtin(tx, r.conversation, "pi.usage")
			if err != nil {
				return nil, err
			}
			value, err := doc.Get()
			if err != nil {
				return nil, err
			}
			if err := addToolUsage(value, call.Name, owned.Usage, l); err != nil {
				return nil, err
			}
			if err := doc.Set(value); err != nil {
				return nil, err
			}
		}
		if err := canonicalToolSlotUpdate(tx, r.taskID, r.conversation, func(slot JSON) {
			slot["status"] = "done"
			for _, key := range []string{"output", "details", "diagnostics", "droppedBytes", "droppedLines"} {
				delete(slot, key)
			}
			if input.Kind != "nested" {
				slot["entry"] = settled["entryId"]
			}
		}); err != nil {
			return nil, err
		}
		outcome := &TaskOutcome{Status: ending, Result: &TaskValue{Present: true, Value: settled}}
		if ending == "failed" {
			outcome.Error = &TaskOutcomeError{Message: message}
			if len(failureReason) > 0 {
				outcome.Error.Detail = &TaskValue{Present: true, Value: JSON{"reason": failureReason[0]}}
			}
		}
		return &TaskState{Status: "terminal", Outcome: outcome}, nil
	})
}

var canonicalNestedCallDoc = &DocumentDefinition{options: DefinitionOptions{Kind: "pi.tool.nested-call", Version: 1, Scope: "task", Family: true, Initial: func(JSON) (JSON, error) { return JSON{}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }}}

func canonicalToolRecord(task Task) bool {
	return task.Kind == "pi.tool" && task.Execution != nil && task.Execution.Native != nil
}
