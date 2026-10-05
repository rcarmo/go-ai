package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
)

// HookAPI exposes committed reads and invocation memos, but no task or
// conversation mutation authority. Escaped hooks are fenced on actual return.
type HookAPI struct{ runtime *TaskRuntime }

func (a *HookAPI) TaskID() ID         { return a.runtime.TaskID() }
func (a *HookAPI) ConversationID() ID { return a.runtime.ConversationID() }
func (a *HookAPI) Memo(ctx context.Context, name string) (any, bool, error) {
	return a.runtime.Memo(ctx, name)
}
func (a *HookAPI) MemoCandidate(ctx context.Context, name string, value any) (any, error) {
	return a.runtime.MemoCandidate(ctx, name, value)
}
func (a *HookAPI) ContextView(ctx context.Context, conversation, at ID) (ContextView, error) {
	return a.runtime.ContextView(ctx, conversation, at)
}
func (a *HookAPI) SnapshotDefinition(ctx context.Context, def *DocumentDefinition, owner ID, key *string) (JSON, bool, error) {
	return a.runtime.SnapshotDefinition(ctx, def, owner, key)
}

// Hooks run off the Session line on detached request/response data.
type GenerationHooks struct {
	BeforeRequest func(context.Context, []MessageReceipt, *HookAPI) ([]MessageReceipt, error)
	AfterResponse func(context.Context, MessageReceipt, *HookAPI) error
	AfterTools    func(context.Context, []MessageReceipt, *HookAPI) error
	OnYield       func(context.Context, MessageReceipt, *HookAPI) (*Input, error)
}
type ToolHooks struct {
	BeforeTool func(context.Context, goai.ToolCall, *HookAPI) (*BeforeToolResult, error)
	AfterTool  func(context.Context, goai.ToolCall, ToolResult, *HookAPI) (*ToolResult, error)
}
type CompactionHooks struct {
	BeforeCompact func(context.Context, CompactionInput, *HookAPI) (*CompactionDecision, error)
}
type CompactionInput struct {
	Reason       string
	Entries      []Entry
	Messages     []MessageReceipt
	FirstKept    ID
	Instructions string
}
type CompactionDecision struct {
	Decline bool
	Summary *string
}

type BeforeToolResult struct {
	Arguments JSON
	Block     string
}

func callBeforeRequest(ctx context.Context, hook func(context.Context, []MessageReceipt, *HookAPI) ([]MessageReceipt, error), messages []MessageReceipt, r *TaskRuntime) (result []MessageReceipt, err error) {
	defer func() {
		if recover() != nil {
			err = reject("beforeRequest hook panic")
		}
	}()
	return hook(ctx, messages, &HookAPI{r})
}
func callAfterResponse(ctx context.Context, hook func(context.Context, MessageReceipt, *HookAPI) error, message MessageReceipt, r *TaskRuntime) (err error) {
	defer func() {
		if recover() != nil {
			err = reject("afterResponse hook panic")
		}
	}()
	return hook(ctx, message, &HookAPI{r})
}
func detachReceipts(messages []MessageReceipt, limits Limits) ([]MessageReceipt, error) {
	// Strict JSON envelope supplies aggregate node/depth/byte limits and copies
	// all nested content, sections and tool schemas without sharing host slices.
	value, err := dtoObject(struct {
		Messages []MessageReceipt `json:"messages"`
	}{messages}, limits)
	if err != nil {
		return nil, err
	}
	var copy struct {
		Messages []MessageReceipt `json:"messages"`
	}
	err = fromObject(value, &copy, limits)
	return copy.Messages, err
}
func (r *Registry) selectedGeneration(agent agentState, limits Limits) ([]toolOffer, map[string]registeredTool, []PromptSection, []GenerationHooks, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	selected := func(names *[]string, name string) bool {
		if names == nil {
			return true
		}
		for _, item := range *names {
			if item == name {
				return true
			}
		}
		return false
	}
	tools := make(map[string]registeredTool, len(r.tools))
	for name, tool := range r.tools {
		tools[name] = tool
	}
	sections := []PromptSection{}
	hooks := []GenerationHooks{}
	for _, extension := range r.extensions {
		if !selected(agent.Extensions, extension.name) {
			continue
		}
		for name, tool := range extension.tools {
			tools[name] = tool
		}
		sections = append(sections, extension.sections...)
		hooks = append(hooks, extension.hooks)
	}
	offers := []toolOffer{}
	pins := map[string]registeredTool{}
	for _, name := range sortedToolNames(tools) {
		if !selected(agent.Tools, name) {
			continue
		}
		tool := tools[name]
		schema, err := copyObject(tool.offer.Schema, limits)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		tool.offer.Schema = schema
		offers = append(offers, tool.offer)
		pins[name] = tool
	}
	return offers, pins, sections, hooks, nil
}

func (r *Registry) selectedToolHooks(agent agentState) []ToolHooks {
	r.mu.RLock()
	defer r.mu.RUnlock()
	hooks := []ToolHooks{}
	for _, extension := range r.extensions {
		selected := agent.Extensions == nil
		if agent.Extensions != nil {
			for _, name := range *agent.Extensions {
				if name == extension.name {
					selected = true
					break
				}
			}
		}
		if selected {
			hooks = append(hooks, extension.toolHooks)
		}
	}
	return hooks
}
func callBeforeTool(ctx context.Context, hook func(context.Context, goai.ToolCall, *HookAPI) (*BeforeToolResult, error), call goai.ToolCall, r *TaskRuntime) (result *BeforeToolResult, err error) {
	defer func() {
		if recover() != nil {
			err = reject("beforeTool hook panic")
		}
	}()
	return hook(ctx, call, &HookAPI{r})
}
func callAfterTool(ctx context.Context, hook func(context.Context, goai.ToolCall, ToolResult, *HookAPI) (*ToolResult, error), call goai.ToolCall, result ToolResult, r *TaskRuntime) (next *ToolResult, err error) {
	defer func() {
		if recover() != nil {
			err = reject("afterTool hook panic")
		}
	}()
	return hook(ctx, call, result, &HookAPI{r})
}
func callAfterTools(ctx context.Context, hook func(context.Context, []MessageReceipt, *HookAPI) error, results []MessageReceipt, r *TaskRuntime) (err error) {
	defer func() {
		if recover() != nil {
			err = reject("afterTools hook panic")
		}
	}()
	return hook(ctx, results, &HookAPI{r})
}
func detachToolResult(result ToolResult, limits Limits) (ToolResult, error) {
	value, err := dtoObject(struct {
		Content string              `json:"content"`
		Blocks  []goai.ContentBlock `json:"blocks"`
		Details JSON                `json:"details"`
		Usage   *goai.Usage         `json:"usage"`
	}{result.Content, result.Blocks, result.Details, result.Usage}, limits)
	if err != nil {
		return ToolResult{}, err
	}
	var copy struct {
		Content string              `json:"content"`
		Blocks  []goai.ContentBlock `json:"blocks"`
		Details JSON                `json:"details"`
		Usage   *goai.Usage         `json:"usage"`
	}
	if err = fromObject(value, &copy, limits); err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Content: copy.Content, Blocks: copy.Blocks, Details: copy.Details, Usage: copy.Usage, Commit: result.Commit}, nil
}

func callOnYield(ctx context.Context, hook func(context.Context, MessageReceipt, *HookAPI) (*Input, error), message MessageReceipt, r *TaskRuntime) (input *Input, err error) {
	defer func() {
		if recover() != nil {
			err = reject("onYield hook panic")
		}
	}()
	return hook(ctx, message, &HookAPI{r})
}
func (h *Harness) continueYield(runtime *TaskRuntime, task Task, cp generationCheckpoint, message MessageReceipt, input Input) (continued bool, err error) {
	if input.Type != "" && input.Type != "follow-up" || input.Entry != nil || input.RequestID != "" {
		h.scheduler.report(reject("invalid onYield continuation"))
		return false, nil
	}
	if len(input.Content) > h.session.limits.MaxStringBytes {
		h.scheduler.report(reject("onYield continuation too large"))
		return false, nil
	}
	_, err = h.session.invocationCommit(runtime.context, task.ID, func(tx *Tx) error {
		if h.closing.Load() || taskAborted(tx.state.Tasks[task.ID]) {
			return ErrSealed
		}
		inbox, err := builtin(tx, task.Conversation, "pi.inbox")
		if err != nil {
			return err
		}
		value, err := inbox.Get()
		if err != nil {
			return err
		}
		items, ok := value["items"].([]any)
		if !ok {
			return reject("inbox shape")
		}
		for _, item := range items {
			object, ok := item.(map[string]any)
			if !ok {
				return reject("inbox item")
			}
			if object["mode"] != "write" {
				return nil
			}
		}
		answer, err := dtoObject(message, tx.limits)
		if err != nil {
			return err
		}
		id, err := tx.MintID()
		if err != nil {
			return err
		}
		if err = tx.AppendEntry(Entry{ID: id, Conversation: task.Conversation, Kind: "message", ByTask: task.ID, Value: answer}); err != nil {
			return err
		}
		usage, err := builtin(tx, task.Conversation, "pi.usage")
		if err != nil {
			return err
		}
		if err = usage.Update(func(value JSON) error { return addModelUsage(value, message, tx.limits) }); err != nil {
			return err
		}
		if err = h.placeQueuedWrites(tx, task.Conversation); err != nil {
			return err
		}
		id, err = tx.MintID()
		if err != nil {
			return err
		}
		receipt, err := inputReceipt(input.Content, input.Blocks, tx.limits)
		if err != nil {
			return err
		}
		user, err := dtoObject(receipt, tx.limits)
		if err != nil {
			return err
		}
		if err = tx.AppendEntry(Entry{ID: id, Conversation: task.Conversation, Kind: "message", ByTask: task.ID, Value: user}); err != nil {
			return err
		}
		state, err := tx.current()
		if err != nil {
			return err
		}
		cp.Messages, err = contextReceipts(state, task.Conversation, tx.limits)
		if err != nil {
			return err
		}
		cp.Phase, cp.Partial, cp.RetryCount = "intent", nil, 0
		current, err := copyTask(tx.state.Tasks[task.ID], tx.limits)
		if err != nil {
			return err
		}
		current.Status = "pending"
		current.Checkpoint, err = dtoObject(cp, tx.limits)
		if err != nil {
			return err
		}
		if err = tx.stage(Write{Op: "put-task", Task: &current}); err != nil {
			return err
		}
		continued = true
		return nil
	})
	return continued, err
}

func (r *Registry) selectedHooks(agent agentState) []GenerationHooks {
	r.mu.RLock()
	defer r.mu.RUnlock()
	hooks := []GenerationHooks{}
	for _, extension := range r.extensions {
		selected := agent.Extensions == nil
		if agent.Extensions != nil {
			for _, name := range *agent.Extensions {
				if name == extension.name {
					selected = true
					break
				}
			}
		}
		if selected {
			hooks = append(hooks, extension.hooks)
		}
	}
	return hooks
}

func (r *Registry) selectedCompactionHooks(agent agentState) []CompactionHooks {
	r.mu.RLock()
	defer r.mu.RUnlock()
	hooks := []CompactionHooks{}
	for _, extension := range r.extensions {
		selected := agent.Extensions == nil
		if agent.Extensions != nil {
			for _, name := range *agent.Extensions {
				if name == extension.name {
					selected = true
					break
				}
			}
		}
		if selected {
			hooks = append(hooks, extension.compactionHooks)
		}
	}
	return hooks
}
func callBeforeCompact(ctx context.Context, hook func(context.Context, CompactionInput, *HookAPI) (*CompactionDecision, error), input CompactionInput, r *TaskRuntime) (result *CompactionDecision, err error) {
	defer func() {
		if recover() != nil {
			err = reject("beforeCompact hook panic")
		}
	}()
	return hook(ctx, input, &HookAPI{r})
}
