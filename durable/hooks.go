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

// Private selections keep provenance without changing the public hook structs.
type selectedGenerationHook struct {
	GenerationHooks
	extension string
}
type selectedToolHook struct {
	ToolHooks
	extension string
}
type selectedCompactionHook struct {
	CompactionHooks
	extension string
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
	// Large image strings have their own per-image/aggregate bounds. Copy the
	// remaining envelope under ordinary limits without repeatedly encoding them.
	stripped := append([]MessageReceipt(nil), messages...)
	type imageSlot struct {
		message, block int
		data           string
	}
	var images []imageSlot
	total := 0
	for i := range stripped {
		stripped[i].Content = append([]goai.ContentBlock(nil), messages[i].Content...)
		for j, block := range stripped[i].Content {
			if block.Type != "image" || len(block.Data) <= limits.MaxStringBytes/2 {
				continue
			}
			if err := validateCodemodeImages([]goai.ImageContent{{Type: "image", Data: block.Data, MimeType: block.MimeType}}, limits); err != nil {
				return nil, err
			}
			if total > MaxCodemodeImagesBytes-len(block.Data) {
				return nil, reject("hook images exceed aggregate limit")
			}
			total += len(block.Data)
			images = append(images, imageSlot{i, j, block.Data})
			stripped[i].Content[j].Data = ""
		}
	}
	value, err := dtoObject(struct {
		Messages []MessageReceipt `json:"messages"`
	}{stripped}, limits)
	if err != nil {
		return nil, err
	}
	var copy struct {
		Messages []MessageReceipt `json:"messages"`
	}
	err = fromObject(value, &copy, limits)
	if err != nil {
		return nil, err
	}
	for _, image := range images {
		copy.Messages[image.message].Content[image.block].Data = image.data
	}
	return copy.Messages, nil
}
func (r *Registry) selectedGeneration(agent agentState, limits Limits, reports ...func(error)) ([]toolOffer, map[string]registeredTool, []PromptSection, []selectedGenerationHook, error) {
	return r.selectedTools(agent, limits, true, reports...)
}

// selectedTools composes enabled registrations before optional model-offer
// filtering. Wrappers run once per selection, outside registry locks.
func (r *Registry) selectedTools(agent agentState, limits Limits, modelOnly bool, reports ...func(error)) ([]toolOffer, map[string]registeredTool, []PromptSection, []selectedGenerationHook, error) {
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
	hooks := []selectedGenerationHook{}
	for _, extension := range extensions {
		for _, name := range extension.toolOrder {
			tools[name] = extension.tools[name]
			if !known[name] {
				ordered = append(ordered, name)
				known[name] = true
			}
		}
		for _, section := range extension.sections {
			section = attributedPromptSection(section, extension.name)
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
		hooks = append(hooks, selectedGenerationHook{extension.hooks, extension.name})
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
					reportSelectionError(reports, extensionReportError(err, extension.name))
				} else {
					next.extension = extension.name
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
					reportSelectionError(reports, extensionReportError(err, extension.name))
				} else {
					sections[index] = attributedPromptSection(next, extension.name)
				}
			}
		}
	}
	offers := []toolOffer{}
	pins := map[string]registeredTool{}
	names := append([]string{}, ordered...)
	if agent.Tools != nil {
		names = append([]string{}, (*agent.Tools)...)
	}
	if modelOnly && agent.ModelTools != nil {
		// Default offers retain registry order; explicitly loaded tools follow
		// in branch-local load order, including nondeferred removed/re-added tools.
		added := map[string]bool{}
		for _, name := range agent.ModelTools.Add {
			added[name] = true
		}
		base := []string{}
		for _, name := range names {
			if !added[name] {
				base = append(base, name)
			}
		}
		for _, name := range agent.ModelTools.Add {
			if selected(agent.Tools, name) {
				base = append(base, name)
			}
		}
		names = base
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
		if modelOnly && !modelToolOffered(tool.offer, agent.ModelTools) {
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

func (r *Registry) selectedToolHooks(agent agentState) []selectedToolHook {
	r.mu.RLock()
	defer r.mu.RUnlock()
	hooks := []selectedToolHook{}
	for _, extension := range r.selectedExtensionsLocked(agent) {
		hooks = append(hooks, selectedToolHook{extension.toolHooks, extension.name})
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
	blocks, err := detachReceipts([]MessageReceipt{{Role: goai.RoleToolResult, Content: result.Blocks}}, limits)
	if err != nil {
		return ToolResult{}, err
	}
	originalBlocks := blocks[0].Content
	result.Blocks = nil
	value, err := dtoObject(struct {
		StructuredOutput    any                 `json:"structuredOutput"`
		HasStructuredOutput bool                `json:"hasStructuredOutput"`
		Content             string              `json:"content"`
		Blocks              []goai.ContentBlock `json:"blocks"`
		Details             JSON                `json:"details"`
		DetailsValue        any                 `json:"detailsValue"`
		HasDetails          bool                `json:"hasDetails"`
		Usage               *goai.Usage         `json:"usage"`
		Diagnostics         []ToolDiagnostic    `json:"diagnostics"`
		IsError             bool                `json:"isError"`
		Control             *ToolControl        `json:"control"`
	}{result.StructuredOutput, result.HasStructuredOutput || result.StructuredOutput != nil, result.Content, result.Blocks, result.Details, result.DetailsValue, result.HasDetails, result.Usage, result.Diagnostics, result.IsError, result.Control}, limits)
	if err != nil {
		return ToolResult{}, err
	}
	var copy struct {
		StructuredOutput    any                 `json:"structuredOutput"`
		HasStructuredOutput bool                `json:"hasStructuredOutput"`
		Content             string              `json:"content"`
		Blocks              []goai.ContentBlock `json:"blocks"`
		Details             JSON                `json:"details"`
		DetailsValue        any                 `json:"detailsValue"`
		HasDetails          bool                `json:"hasDetails"`
		Usage               *goai.Usage         `json:"usage"`
		Diagnostics         []ToolDiagnostic    `json:"diagnostics"`
		IsError             bool                `json:"isError"`
		Control             *ToolControl        `json:"control"`
	}
	if err = fromObject(value, &copy, limits); err != nil {
		return ToolResult{}, err
	}
	return ToolResult{imageChunks: result.imageChunks, StructuredOutput: copy.StructuredOutput, HasStructuredOutput: copy.HasStructuredOutput, Content: copy.Content, Blocks: originalBlocks, Details: copy.Details, DetailsValue: copy.DetailsValue, HasDetails: copy.HasDetails, Usage: copy.Usage, Diagnostics: copy.Diagnostics, IsError: copy.IsError, Control: copy.Control, Commit: result.Commit, usesRetainedOutput: result.usesRetainedOutput, usesReportedDiagnostics: result.usesReportedDiagnostics}, nil
}

func callOnYield(ctx context.Context, hook func(context.Context, MessageReceipt, *HookAPI) (*Input, error), message MessageReceipt, r *TaskRuntime) (input *Input, err error) {
	defer func() {
		if recover() != nil {
			err = reject("onYield hook panic")
		}
	}()
	return hook(ctx, message, &HookAPI{r})
}
func (h *Harness) continueYield(runtime *TaskRuntime, task Task, cp generationCheckpoint, message MessageReceipt, input Input, extension string) (continued bool, err error) {
	if input.Type != "" && input.Type != "follow-up" || input.Entry != nil || input.RequestID != "" {
		runtime.reportExtensionError(reject("invalid onYield continuation"), extension)
		return false, nil
	}
	if len(input.Content) > h.session.limits.MaxStringBytes {
		runtime.reportExtensionError(reject("onYield continuation too large"), extension)
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

func (r *Registry) selectedHooks(agent agentState) []selectedGenerationHook {
	r.mu.RLock()
	defer r.mu.RUnlock()
	hooks := []selectedGenerationHook{}
	for _, extension := range r.selectedExtensionsLocked(agent) {
		hooks = append(hooks, selectedGenerationHook{extension.hooks, extension.name})
	}
	return hooks
}

func (r *Registry) selectedCompactionHooks(agent agentState) []selectedCompactionHook {
	r.mu.RLock()
	defer r.mu.RUnlock()
	hooks := []selectedCompactionHook{}
	for _, extension := range r.selectedExtensionsLocked(agent) {
		hooks = append(hooks, selectedCompactionHook{extension.compactionHooks, extension.name})
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
