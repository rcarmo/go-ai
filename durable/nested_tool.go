package durable

import (
	"context"
	"errors"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"time"
)

const nestedToolKind = "task.pi.nested-tool"
const nestedCallDocument = "pi.tool.nested-call"

type nestedToolInput struct {
	Sequential   bool      `json:"sequential,omitempty"`
	ParentCallID string    `json:"parentCallId,omitempty"`
	Parent       ID        `json:"parent"`
	Key          string    `json:"key"`
	Name         string    `json:"name"`
	Arguments    JSON      `json:"arguments"`
	Offer        toolOffer `json:"offer"`
}
type nestedCallState struct {
	Task      ID                 `json:"task"`
	Name      string             `json:"name"`
	Arguments JSON               `json:"arguments"`
	Result    *nestedToolReceipt `json:"result,omitempty"`
}
type nestedToolReceipt struct {
	StructuredOutput    any                 `json:"structuredOutput,omitempty"`
	HasStructuredOutput bool                `json:"hasStructuredOutput"`
	Blocks              []goai.ContentBlock `json:"blocks,omitempty"`
	Diagnostics         []ToolDiagnostic    `json:"diagnostics,omitempty"`
	DetailsValue        any                 `json:"detailsValue,omitempty"`
	HasDetails          bool                `json:"hasDetails,omitempty"`
	Usage               *goai.Usage         `json:"usage,omitempty"`
	Content             string              `json:"content"`
	Details             JSON                `json:"details,omitempty"`
	IsError             bool                `json:"isError"`
	Code                string              `json:"code,omitempty"`
	DurationMs          int64               `json:"durationMs"`
}

// NestedToolHandle is bound to the caller invocation. Its durable key/result is
// owned by the caller task; it grants no authority after that invocation ends.
type NestedToolHandle struct {
	runtime  *TaskRuntime
	task     ID
	document ID
}

func (h *NestedToolHandle) ID() ID {
	if h == nil {
		return 0
	}
	return h.task
}
func (h *NestedToolHandle) Wait(ctx context.Context) (ToolResult, error) {
	if h == nil || h.runtime == nil {
		return ToolResult{}, ErrSealed
	}
	r := h.runtime
	record, err := r.WaitForTask(ctx, h.task)
	if err != nil {
		return ToolResult{}, err
	}
	if record.State.Outcome != nil && record.State.Outcome.Status == "faulted" {
		return ToolResult{}, fmt.Errorf("nested task fault: %+v", record.State.Outcome.Error)
	}
	var receipt *nestedToolReceipt
	err = r.harness.session.readTasks(ctx, func(state Snapshot) error {
		if err := r.check(); err != nil {
			return err
		}
		doc, ok := state.Documents[h.document]
		if !ok || doc.Owner != r.taskID {
			return reject("nested result missing")
		}
		var call nestedCallState
		if err := fromObject(doc.Value, &call, r.harness.session.limits); err != nil {
			return err
		}
		receipt = call.Result
		if receipt != nil {
			value, err := dtoObject(receipt, r.harness.session.limits)
			if err != nil {
				return err
			}
			var owned nestedToolReceipt
			if err = fromObject(value, &owned, r.harness.session.limits); err != nil {
				return err
			}
			receipt = &owned
		}
		if receipt == nil {
			return reject("nested result unsettled")
		}
		return nil
	})
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{StructuredOutput: receipt.StructuredOutput, HasStructuredOutput: receipt.HasStructuredOutput, Content: receipt.Content, Blocks: receipt.Blocks, Diagnostics: receipt.Diagnostics, Details: receipt.Details, DetailsValue: receipt.DetailsValue, HasDetails: receipt.HasDetails, Usage: receipt.Usage, IsError: receipt.IsError}, nil
}

// StartTool atomically admits a keyed nested call. Reuse requires exactly the
// same name and JSON arguments, including after replay; no executor runs here.
func (r *TaskRuntime) StartTool(ctx context.Context, key, name string, arguments JSON) (*NestedToolHandle, error) {
	return r.startTool(ctx, key, name, arguments, false)
}

// StartToolAuto allocates invocation-local sequential keys. Replayed callers
// issuing the same sequence reuse existing durable tasks in that order.
func (r *TaskRuntime) StartToolAuto(ctx context.Context, name string, args JSON) (*NestedToolHandle, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	n := r.nestedSequence.Add(1)
	if n == 0 || n > MaxID {
		return nil, reject("nested sequence exhausted")
	}
	return r.startTool(ctx, fmt.Sprint(n), name, args, true)
}
func (r *TaskRuntime) CallToolAuto(ctx context.Context, name string, args JSON) (ToolResult, error) {
	handle, err := r.StartToolAuto(ctx, name, args)
	if err != nil {
		return ToolResult{}, err
	}
	return handle.Wait(ctx)
}
func (r *TaskRuntime) startTool(ctx context.Context, key, name string, arguments JSON, sequential bool) (*NestedToolHandle, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, reject("nil nested call context")
	}
	l := r.harness.session.limits
	if (!sequential && !validNestedKey(key) || sequential && !validSequentialKey(key)) || len(key) > l.MaxRequestIDBytes || !validKind(name) {
		return nil, reject("invalid nested call key/name")
	}
	owned, err := copyObject(arguments, l)
	if err != nil {
		return nil, err
	}
	if owned == nil {
		owned = JSON{}
	}
	agent, err := r.Agent(ctx)
	if err != nil {
		return nil, err
	}
	allowed := false
	for _, tool := range agent.Callable {
		if tool.Name == name {
			allowed = true
		}
	}
	if !allowed {
		return nil, reject("tool is not callable by tools")
	}
	// Agent selection is phase-local. Select the same captured revision when
	// choosing executable registration; do not consult a later live registry.
	var state agentState
	value, err := dtoObject(agent.Configuration, l)
	if err != nil {
		return nil, err
	}
	if err = fromObject(value, &state, l); err != nil {
		return nil, err
	}
	_, pins, _, _, err := r.phaseSelection().selectedTools(r.harness.selectionAgent(state), l, false, r.reportError)
	if err != nil {
		return nil, err
	}
	reg, ok := pins[name]
	if !ok || !toolCallerAllowed(reg.offer, "tools") {
		return nil, reject("tool is not callable by tools")
	}
	handle := &NestedToolHandle{runtime: r}
	err = r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		for _, doc := range tx.state.Documents {
			if !doc.Retired && doc.Scope == "task" && doc.Owner == r.taskID && doc.Kind == nestedCallDocument && doc.Family && doc.Key == key {
				var call nestedCallState
				if err := fromObject(doc.Value, &call, l); err != nil {
					return nil, err
				}
				if call.Name != name || !equalJSONValue(call.Arguments, owned) {
					return nil, reject("nested call key reused with different tool/arguments")
				}
				handle.task = call.Task
				handle.document = doc.ID
				return nil, nil
			}
		}
		parentCallID, err := nestedParentCallID(tx.state.Tasks[r.taskID], tx.limits)
		if err != nil {
			return nil, err
		}
		input := nestedToolInput{ParentCallID: parentCallID, Sequential: sequential, Parent: r.taskID, Key: key, Name: name, Arguments: owned, Offer: reg.offer}
		object, err := dtoObject(input, l)
		if err != nil {
			return nil, err
		}
		id, err := tx.CreateTask(nestedToolDefinition(), object, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.taskID}, AbandonOnRestart: !reg.offer.ReplaySafe})
		if err != nil {
			return nil, err
		}
		docID, err := tx.MintID()
		if err != nil {
			return nil, err
		}
		call, err := dtoObject(nestedCallState{Task: id, Name: name, Arguments: owned}, l)
		if err != nil {
			return nil, err
		}
		_, err = tx.CreateDocument(Document{ID: docID, Scope: "task", Owner: r.taskID, Kind: nestedCallDocument, Family: true, Key: key, Version: 1, Value: call})
		handle.task = id
		handle.document = docID
		return nil, err
	})
	if err != nil {
		return nil, err
	}
	return handle, nil
}
func (r *TaskRuntime) CallTool(ctx context.Context, key, name string, args JSON) (ToolResult, error) {
	handle, err := r.StartTool(ctx, key, name, args)
	if err != nil {
		return ToolResult{}, err
	}
	return handle.Wait(ctx)
}
func (a *ToolAPI) StartToolAuto(ctx context.Context, name string, args JSON) (*NestedToolHandle, error) {
	a.mu.Lock()
	active := a.active
	a.mu.Unlock()
	if !active {
		return nil, ErrSealed
	}
	return a.runtime.StartToolAuto(ctx, name, args)
}
func (a *ToolAPI) CallToolAuto(ctx context.Context, name string, args JSON) (ToolResult, error) {
	handle, err := a.StartToolAuto(ctx, name, args)
	if err != nil {
		return ToolResult{}, err
	}
	return handle.Wait(ctx)
}
func (a *ToolAPI) StartTool(ctx context.Context, key, name string, args JSON) (*NestedToolHandle, error) {
	a.mu.Lock()
	active := a.active
	a.mu.Unlock()
	if !active {
		return nil, ErrSealed
	}
	return a.runtime.StartTool(ctx, key, name, args)
}
func (a *ToolAPI) CallTool(ctx context.Context, key, name string, args JSON) (ToolResult, error) {
	a.mu.Lock()
	active := a.active
	a.mu.Unlock()
	if !active {
		return ToolResult{}, ErrSealed
	}
	return a.runtime.CallTool(ctx, key, name, args)
}

var nestedDefinition = func() *TaskDefinition {
	definition, err := DefineTask(TaskDefinitionOptions{Kind: nestedToolKind, Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "execute", "started": false}, nil }, Phases: map[string]TaskPhase{"execute": runNestedTool}, Abort: abortNestedTool})
	if err != nil {
		panic(err)
	}
	return definition
}()

func nestedToolDefinition() *TaskDefinition { return nestedDefinition }
func nestedInput(record TaskRecord, l Limits) (nestedToolInput, error) {
	var input nestedToolInput
	if record.Input == nil {
		return input, reject("nested input missing")
	}
	object, ok := record.Input.Value.(map[string]any)
	if !ok {
		return input, reject("nested input shape")
	}
	err := fromObject(JSON(object), &input, l)
	return input, err
}
func abortNestedTool(ctx context.Context, record TaskRecord, r *TaskRuntime) error {
	code := "aborted"
	if record.AbortReason == "restart" {
		code = "abandoned"
		if started, _ := record.State.Checkpoint["started"].(bool); started {
			code = "interrupted"
		}
	}
	result := nestedProgressResult(record, r.harness.session.limits)
	result.IsError = true
	if result.Content == "" && len(result.Blocks) == 0 {
		result.Content = code
	}
	result.Diagnostics = append(result.Diagnostics, ToolDiagnostic{Severity: "error", Code: code, Message: code})
	return settleNestedTool(ctx, record, r, result, code, 0)
}
func runNestedTool(ctx context.Context, record TaskRecord, r *TaskRuntime) error {
	l := r.harness.session.limits
	input, err := nestedInput(record, l)
	if err != nil {
		return err
	}
	agent, err := r.Agent(ctx)
	if err != nil {
		return err
	}
	var agentState agentState
	value, err := dtoObject(agent.Configuration, l)
	if err != nil {
		return err
	}
	if err = fromObject(value, &agentState, l); err != nil {
		return err
	}
	_, pins, _, _, err := r.phaseSelection().selectedTools(r.harness.selectionAgent(agentState), l, false, r.reportError)
	if err != nil {
		return err
	}
	reg, exists := pins[input.Name]
	started, _ := record.State.Checkpoint["started"].(bool)
	if !exists || !toolCallerAllowed(reg.offer, "tools") || !toolCallerAllowed(input.Offer, "tools") {
		return settleNestedTool(ctx, record, r, ToolResult{Content: "Tool unavailable", IsError: true}, "tool_unavailable", 0)
	}
	if started && (!input.Offer.ReplaySafe || !reg.offer.ReplaySafe) {
		return settleNestedTool(ctx, record, r, ToolResult{Content: "Tool interrupted", IsError: true}, "interrupted", 0)
	}
	args := input.Arguments
	if started {
		if object, ok := record.State.Checkpoint["arguments"].(map[string]any); ok {
			args = JSON(object)
		}
		// Each replayed executor attempt starts a fresh progress buffer.
		if err = r.Commit(ctx, func(_ *Tx, current TaskRecord) (*TaskState, error) {
			cp := current.State.Checkpoint
			delete(cp, "progress")
			return &TaskState{Status: "running", Checkpoint: cp}, nil
		}); err != nil {
			return err
		}
	} else {
		args, err = validateArguments(ctx, reg, args, l)
		if err != nil {
			return settleNestedTool(ctx, record, r, ToolResult{Content: "Invalid tool arguments", IsError: true}, "invalid_arguments", 0)
		}
		for _, hooks := range r.phaseSelection().selectedToolHooks(r.harness.selectionAgent(agentState)) {
			if hooks.BeforeTool == nil {
				continue
			}
			copy, e := copyObject(args, l)
			if e != nil {
				return e
			}
			decision, e := callBeforeTool(ctx, hooks.BeforeTool, goai.ToolCall{ID: input.Key, Name: input.Name, Arguments: map[string]any(copy)}, r)
			if e != nil || decision != nil && decision.Block != "" {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if e != nil {
					r.reportExtensionError(e, hooks.extension)
				}
				return settleNestedTool(ctx, record, r, ToolResult{Content: "Tool blocked", IsError: true}, "blocked", 0)
			}
			if decision != nil && decision.Arguments != nil {
				trial := reg
				trial.prepare = nil
				args, e = validateArguments(ctx, trial, decision.Arguments, l)
				if e != nil {
					return settleNestedTool(ctx, record, r, ToolResult{Content: "Invalid tool arguments", IsError: true}, "invalid_arguments", 0)
				}
			}
		}
		if err = r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
			return &TaskState{Status: "running", Checkpoint: JSON{"phase": "execute", "started": true, "arguments": args}}, nil
		}); err != nil {
			return err
		}
	}
	// Executor result settlement stays separate from admission and intent. The
	// caller-scoped document and native terminal outcome adopt in one commit.
	api := &ToolAPI{h: r.harness, task: Task{ID: r.taskID, Conversation: record.Conversation}, checkpoint: toolCheckpoint{Offer: reg.offer}, ctx: ctx, active: true, runtime: r, nested: true}
	api.environment, api.environmentErr = resolveInvocationEnvironment(ctx, r.harness, r, record.Conversation, true)
	api.environmentResolved = true
	if api.environmentErr != nil {
		_ = api.seal()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return settleNestedTool(ctx, record, r, ToolResult{Content: "Tool environment failed", IsError: true}, "tool_error", 0)
	}
	// Do not share persisted admission arguments with executor-owned mutations.
	args, err = copyObject(args, l)
	if err != nil {
		_ = api.seal()
		return err
	}
	var result ToolResult
	startedAt := time.Now()
	func() {
		defer func() {
			if recover() != nil {
				err = errors.New("nested tool executor panic")
			}
		}()
		result, err = reg.execute(ctx, args, api)
	}()
	duration := time.Since(startedAt).Round(time.Millisecond).Milliseconds()
	if sealErr := api.seal(); err == nil {
		err = sealErr
	}
	api.mu.Lock()
	if result.Content == "" && len(result.Blocks) == 0 {
		result.Content = api.checkpoint.Output
	}
	if result.Details == nil && !result.HasDetails {
		result.DetailsValue = api.checkpoint.Details
		result.HasDetails = api.checkpoint.Details != nil
	}
	result.Diagnostics = append(append([]ToolDiagnostic{}, api.checkpoint.Diagnostics...), result.Diagnostics...)
	api.mu.Unlock()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		result.Commit = nil
		result.IsError = true
		if result.Content == "" && len(result.Blocks) == 0 {
			result.Content = fmt.Sprintf("Tool %s threw", input.Name)
		}
		result.Diagnostics = append(result.Diagnostics, ToolDiagnostic{Severity: "error", Code: "tool_error", Message: fmt.Sprintf("Tool %s threw", input.Name)})
		result, hookErr := runNestedAfterHooks(ctx, r, agentState, input, args, result)
		if hookErr != nil {
			return hookErr
		}
		return settleNestedTool(ctx, record, r, result, "tool_error", duration)
	}
	result, err = runNestedAfterHooks(ctx, r, agentState, input, args, result)
	if err != nil {
		return err
	}
	return settleNestedTool(ctx, record, r, result, "", duration)
}
func settleNestedTool(ctx context.Context, record TaskRecord, r *TaskRuntime, result ToolResult, code string, duration int64) error {
	l := r.harness.session.limits
	input, err := nestedInput(record, l)
	if err != nil {
		return err
	}
	if result.Usage != nil && !validUsage(result.Usage) {
		result = ToolResult{Content: "Invalid tool usage", IsError: true}
		code = "invalid_usage"
	}
	detached, err := detachToolResult(result, l)
	if err != nil {
		detached = ToolResult{Content: "Invalid tool result", IsError: true}
		code = "invalid_tool_result"
	}
	if err := validateToolDiagnostics(detached.Diagnostics, l); err != nil {
		detached = ToolResult{Content: "Invalid tool diagnostics", IsError: true}
		code = "invalid_tool_result"
	}
	if len(detached.Blocks) > 0 {
		if _, err := contributionReceipt(goai.Message{Role: "toolResult", Content: detached.Blocks}, l); err != nil {
			detached = ToolResult{Content: "Invalid tool content", IsError: true}
			code = "invalid_content"
		}
	}
	limits := resolvedToolOutputLimits(input.Offer.OutputLimits)
	if len(detached.Blocks) > 0 {
		detached.Blocks = boundToolBlocksJoined(detached.Blocks, limits, "\n")
		detached.Content = goai.ContentText(detached.Blocks)
	} else {
		detached.Content = boundToolText(detached.Content, limits)
	}
	// Nested controls never affect the caller's model, but malformed control
	// values still invalidate a host result and cannot run app callbacks.
	if err := validateToolControl(detached.Control, l); err != nil {
		detached = ToolResult{Content: "Invalid tool control", IsError: true}
		code = "invalid_control"
	}
	if code != "" {
		detached.IsError = true
	}
	if code == "" {
		var structuredErr bool
		detached, structuredErr = validateNestedStructuredResult(detached, input.Offer, l)
		if structuredErr {
			code = "invalid_structured_output"
		}
	} else {
		detached.StructuredOutput = nil
		detached.HasStructuredOutput = false
	}
	if code != "" {
		found := false
		for _, diagnostic := range detached.Diagnostics {
			if diagnostic.Severity == "error" && diagnostic.Code == code {
				found = true
			}
		}
		if !found {
			detached.Diagnostics = append(detached.Diagnostics, ToolDiagnostic{Severity: "error", Code: code, Message: detached.Content})
		}
	}
	if err := validateToolDiagnostics(detached.Diagnostics, l); err != nil {
		return err
	}
	// Nested model controls are local results; never patch the caller's model
	// configuration or append standalone nested messages to its transcript.
	receipt := nestedToolReceipt{StructuredOutput: detached.StructuredOutput, HasStructuredOutput: detached.HasStructuredOutput, Content: detached.Content, Blocks: detached.Blocks, Diagnostics: detached.Diagnostics, Details: detached.Details, DetailsValue: detached.DetailsValue, HasDetails: detached.HasDetails, Usage: detached.Usage, IsError: detached.IsError, Code: code, DurationMs: duration}
	return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		if detached.Commit != nil && code == "" {
			offset := len(tx.writes)
			if err := callToolOutcome(detached.Commit, tx); err != nil {
				return nil, err
			}
			for _, write := range tx.writes[offset:] {
				if !((write.Op == "put-document" || write.Op == "retire-document") && write.Document != nil && write.Document.Scope == "conversation" && write.Document.Owner == record.Conversation && !strings.HasPrefix(write.Document.Kind, "pi.")) {
					return nil, reject("tool outcome writes must be application documents")
				}
			}
		}
		found := false
		for _, doc := range tx.state.Documents {
			if doc.Scope == "task" && doc.Owner == input.Parent && doc.Kind == nestedCallDocument && doc.Key == input.Key && !doc.Retired {
				var call nestedCallState
				if err := fromObject(doc.Value, &call, l); err != nil {
					return nil, err
				}
				if call.Task != record.ID {
					return nil, reject("nested result task mismatch")
				}
				call.Result = &receipt
				object, err := dtoObject(call, l)
				if err != nil {
					return nil, err
				}
				handle, err := tx.Document(doc.ID)
				if err != nil {
					return nil, err
				}
				if err = handle.Set(object); err != nil {
					return nil, err
				}
				found = true
				break
			}
		}
		if !found {
			return nil, reject("nested result document missing")
		}
		if detached.Usage != nil {
			handle, err := builtin(tx, record.Conversation, "pi.usage")
			if err != nil {
				return nil, err
			}
			usage, err := handle.Get()
			if err != nil {
				return nil, err
			}
			if err = addToolUsage(usage, input.Name, detached.Usage, l); err != nil {
				return nil, err
			}
			if err = handle.Set(usage); err != nil {
				return nil, err
			}
		}
		status := "completed"
		if code == "aborted" || code == "abandoned" {
			status = "aborted"
		} else if code != "" && code != "tool_unavailable" && code != "invalid_arguments" && code != "blocked" {
			status = "failed"
		}
		outcome := &TaskOutcome{Status: status, Result: &TaskValue{Present: true, Value: JSON{"key": input.Key}}}
		if status == "failed" {
			outcome.Error = &TaskOutcomeError{Message: receipt.Content}
		}
		return &TaskState{Status: "terminal", Outcome: outcome}, nil
	})
}
