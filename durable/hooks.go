package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
)

// HookAPI carries the invocation runtime, as in the pinned hook contract.
// Embedded native methods share its commit/ownership/read authority fences;
// escaped hooks cannot retain authority after the invocation ends.
type HookAPI struct{ *TaskRuntime }

// InvocationReader is the narrower committed-read capability supplied to prompt
// renderers and environment factories. It deliberately exposes no Commit or
// owned-work operations even though runtime hooks receive those operations.
type InvocationReader struct{ runtime *TaskRuntime }

func (a *InvocationReader) TaskID() ID         { return a.runtime.TaskID() }
func (a *InvocationReader) ConversationID() ID { return a.runtime.ConversationID() }
func (a *InvocationReader) ContextView(ctx context.Context, conversation, at ID) (ContextView, error) {
	return a.runtime.ContextView(ctx, conversation, at)
}
func (a *InvocationReader) SnapshotDefinition(ctx context.Context, def *DocumentDefinition, owner ID, key *string) (JSON, bool, error) {
	return a.runtime.SnapshotDefinition(ctx, def, owner, key)
}
func (a *InvocationReader) SnapshotDefinitionAsOf(ctx context.Context, def *DocumentDefinition, owner ID, key *string, at ID) (JSON, bool, error) {
	return a.runtime.SnapshotDefinitionAsOf(ctx, def, owner, key, at)
}
func (a *InvocationReader) Entry(ctx context.Context, id ID) (Entry, bool, error) {
	return a.runtime.Entry(ctx, id)
}
func (a *InvocationReader) TypedEntry(ctx context.Context, def *EntryDefinition, id ID) (Entry, bool, error) {
	return a.runtime.TypedEntry(ctx, def, id)
}

// Hooks run off the Session line on detached request/response data.
type GenerationHooks struct {
	BeforeRequest func(context.Context, []MessageReceipt, *HookAPI) ([]MessageReceipt, error)
	AfterResponse func(context.Context, MessageReceipt, *HookAPI) error
	// AfterTools retains the native detached-receipt callback.
	AfterTools func(context.Context, []MessageReceipt, *HookAPI) error
	// AfterToolEntries exposes the pinned committed assistant/result identities
	// for hooks that need entry attribution or historical reads.
	AfterToolEntries func(context.Context, ID, []ID, *HookAPI) error
	OnYield          func(context.Context, MessageReceipt, *HookAPI) (*Input, error)
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
func (r *Registry) selectedGeneration(agent agentState, limits Limits, reports ...func(error)) ([]toolOffer, map[string]registeredTool, []PromptSection, []GenerationHooks, error) {
	r.mu.RLock()
	extensions := append([]installedExtension(nil), r.selectedExtensionsLocked(agent)...)
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
	ordered := append([]string{}, r.toolOrder...)
	known := map[string]bool{}
	for _, name := range ordered {
		known[name] = true
	}
	sections := []PromptSection{}
	hooks := []GenerationHooks{}
	for _, extension := range extensions {
		for _, name := range extension.toolOrder {
			tools[name] = extension.tools[name]
			if !known[name] {
				ordered = append(ordered, name)
				known[name] = true
			}
		}
		for _, section := range extension.sections {
			index := -1
			for i, existing := range sections {
				if existing.Key == section.Key {
					index = i
					break
				}
			}
			if index < 0 {
				sections = append(sections, section)
			} else {
				sections[index] = section
			}
		}
		hooks = append(hooks, extension.hooks)
	}
	r.mu.RUnlock()
	// Host callbacks may reenter the registry. Compose from captured definitions,
	// never while holding its lock. Reports are delivered by the harness later.
	for _, extension := range extensions {
		for _, wrap := range extension.wraps {
			if wrap.Tool != "" {
				tool, exists := tools[wrap.Tool]
				if !exists {
					continue
				}
				next, err := applyToolWrap(wrap, tool, limits)
				if err != nil {
					delete(tools, wrap.Tool)
					reportSelectionError(reports, err)
				} else {
					tools[wrap.Tool] = next
				}
			} else {
				index := -1
				for i, section := range sections {
					if section.Key == wrap.Section {
						index = i
						break
					}
				}
				if index < 0 {
					continue
				}
				next, err := applySectionWrap(wrap, sections[index])
				if err != nil {
					sections = append(sections[:index], sections[index+1:]...)
					reportSelectionError(reports, err)
				} else {
					sections[index] = next
				}
			}
		}
	}
	offers := []toolOffer{}
	pins := map[string]registeredTool{}
	names := ordered
	if agent.Tools != nil {
		names = append([]string{}, (*agent.Tools)...)
	}
	for _, name := range names {
		if !selected(agent.Tools, name) {
			continue
		}
		if agent.ToolsRemoved != nil {
			excluded := false
			for _, removed := range *agent.ToolsRemoved {
				if name == removed {
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}
		}
		tool, exists := tools[name]
		if !exists {
			continue
		}
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
	for _, extension := range r.selectedExtensionsLocked(agent) {
		hooks = append(hooks, extension.toolHooks)
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
func callAfterToolEntries(ctx context.Context, hook func(context.Context, ID, []ID, *HookAPI) error, assistant ID, results []ID, r *TaskRuntime) (err error) {
	defer func() {
		if recover() != nil {
			err = reject("afterTools hook panic")
		}
	}()
	return hook(ctx, assistant, append([]ID(nil), results...), &HookAPI{r})
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
		Content      string              `json:"content"`
		Blocks       []goai.ContentBlock `json:"blocks"`
		Details      JSON                `json:"details"`
		DetailsValue any                 `json:"detailsValue"`
		HasDetails   bool                `json:"hasDetails"`
		Usage        *goai.Usage         `json:"usage"`
		Diagnostics  []ToolDiagnostic    `json:"diagnostics"`
		IsError      bool                `json:"isError"`
		Control      *ToolControl        `json:"control"`
	}{result.Content, result.Blocks, result.Details, result.DetailsValue, result.HasDetails, result.Usage, result.Diagnostics, result.IsError, result.Control}, limits)
	if err != nil {
		return ToolResult{}, err
	}
	var copy struct {
		Content      string              `json:"content"`
		Blocks       []goai.ContentBlock `json:"blocks"`
		Details      JSON                `json:"details"`
		DetailsValue any                 `json:"detailsValue"`
		HasDetails   bool                `json:"hasDetails"`
		Usage        *goai.Usage         `json:"usage"`
		Diagnostics  []ToolDiagnostic    `json:"diagnostics"`
		IsError      bool                `json:"isError"`
		Control      *ToolControl        `json:"control"`
	}
	if err = fromObject(value, &copy, limits); err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Content: copy.Content, Blocks: copy.Blocks, Details: copy.Details, DetailsValue: copy.DetailsValue, HasDetails: copy.HasDetails, Usage: copy.Usage, Diagnostics: copy.Diagnostics, IsError: copy.IsError, Control: copy.Control, Commit: result.Commit, usesRetainedOutput: result.usesRetainedOutput, usesReportedDiagnostics: result.usesReportedDiagnostics}, nil
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
		answerID := id
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
		cp.AssistantEntry = answerID
		if _, _, err := h.handoffToolGeneration(tx, current, cp); err != nil {
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
	for _, extension := range r.selectedExtensionsLocked(agent) {
		hooks = append(hooks, extension.hooks)
	}
	return hooks
}

func (r *Registry) selectedCompactionHooks(agent agentState) []CompactionHooks {
	r.mu.RLock()
	defer r.mu.RUnlock()
	hooks := []CompactionHooks{}
	for _, extension := range r.selectedExtensionsLocked(agent) {
		hooks = append(hooks, extension.compactionHooks)
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
