package durable

import (
	"context"
	"errors"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"math"
	"sort"
	"strconv"
	"time"
)

func (h *Harness) nextTask(id ID) (Task, generationCheckpoint, bool, error) {
	s, e := h.session.Snapshot(context.Background())
	if e != nil {
		return Task{}, generationCheckpoint{}, false, e
	}
	var tasks []Task
	for _, t := range s.Tasks {
		if t.Conversation == id && t.Kind == "pi.generation" && !terminalStatus(t.Status) {
			tasks = append(tasks, t)
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	if len(tasks) == 0 {
		return Task{}, generationCheckpoint{}, false, nil
	}
	var cp generationCheckpoint
	if e = fromObject(tasks[0].Checkpoint, &cp, h.session.limits); e != nil {
		return Task{}, cp, false, e
	}
	return tasks[0], cp, true, nil
}

// runGenerationInvocation performs one reserved built-in unit. It never starts
// children or another generation; tools-phase continuation returns to scheduler.
func (h *Harness) runGenerationInvocation(runtime *TaskRuntime, task Task, cp generationCheckpoint) (e error) {
	id := task.Conversation
	if cp.Abort {
		h.cancelDeferred(runtime.context, cp)
		return h.drainAborted(task, cp)
	}
	if cp.Phase == "tools" {
		return h.runOwnedTools(runtime, &task, &cp)
	}
	if cp.Phase == "compaction" {
		if cp.OverflowMessage != "" {
			state, err := h.session.Snapshot(runtime.context)
			if err != nil {
				return err
			}
			child, ok := state.Tasks[cp.Compaction]
			if !ok {
				return reject("compaction child missing")
			}
			record, err := CanonicalTask(child, h.session.limits)
			if err != nil {
				return err
			}
			valid := record.State.Outcome != nil && record.State.Outcome.Status == "completed" && record.State.Outcome.Result != nil
			if valid {
				result, ok := record.State.Outcome.Result.Value.(map[string]any)
				valid = ok && result["entryId"] != nil
			}
			if !valid {
				return h.finishRecordedModelFailure(task, cp, cp.OverflowMessage)
			}
		}
		cp.Phase = "queued"
		if cp.ResumeAfterCompaction {
			cp.Phase = "prepare-next"
		}
		return h.sessionPrepareAfterCompaction(runtime, task, cp)
	}
	if cp.Phase == "retry" {
		if err := runtime.SleepUntil(runtime.context, cp.RetryUntil); err != nil {
			return err
		}
		// A durable retry starts a new logical attempt from current committed
		// agent/context/registry, unlike transport-level resends of old intent.
		cp.Phase, cp.RetryUntil = "prepare-next", 0
		value, err := dtoObject(cp, h.session.limits)
		if err != nil {
			return err
		}
		task.Checkpoint = value
		if _, err = h.session.invocationCommit(runtime.context, task.ID, func(tx *Tx) error {
			if h.closing.Load() || taskAborted(tx.state.Tasks[task.ID]) {
				return ErrSealed
			}
			return tx.stage(Write{Op: "put-task", Task: &task})
		}); err != nil {
			return err
		}
	}
	if cp.Phase == "queued" || cp.Phase == "prepare-next" {
		started, err := h.startAutomaticCompaction(runtime, task, &cp)
		if err != nil {
			return err
		}
		if started {
			return nil
		}
		e = func() error {
			// Witness this logical attempt ONline before offline validation.
			// Preparation can reject without reaching invocationCommit at all.
			// Keep the same baseline across offline work, intent admission and
			// the one bounded error-receipt fallback; no stale host-start epoch.
			if err := h.session.readTasks(context.Background(), func(state Snapshot) error {
				if err := runtime.check(); err != nil {
					return err
				}
				current := state.Tasks[task.ID]
				if h.closing.Load() || taskAborted(current) || taskBelowCancelled(state, current) || taskSelectedFailFastCancellation(state, current.ID) {
					h.scheduler.endOnLine(runtime)
					return ErrSealed
				}
				epoch := h.scheduler.epoch.Load()
				runtime.admissionEpoch = epoch
				runtime.fallbackEpoch = &epoch
				return nil
			}); err != nil {
				return err
			}
			defer h.session.taskBookkeeping(func() { runtime.fallbackEpoch = nil })
			if preparationErr := h.prepareRequest(runtime, &task, &cp); preparationErr != nil {
				var missing *generationNoModel
				if errors.As(preparationErr, &missing) {
					return h.finishGenerationNoModel(task, cp, missing)
				}
				var rejected *StorageRejected
				if h.life.Err() == nil && errors.As(preparationErr, &rejected) {
					fallbackErr := h.finish(task, cp, errorReceipt("invalid_preparation"), false)
					if fallbackErr == nil {
						h.notify()
					}
					return fallbackErr
				}
				return preparationErr
			}
			return nil
		}()
		if e != nil {
			return e
		}
		// A successful preparation-error fallback already decided this task.
		if runtime.ended.Load() {
			return nil
		}
	}
	if cp.Partial != nil {
		if e = h.sessionCommitInterruptedPartial(task, cp); e != nil {
			return e
		}
		cp.Partial = nil
	}
	// On recovery reuse persisted model/settings/cutoff. A new logical attempt is
	// committed before remote dispatch. Generic providers have no exactly-once
	// billing guarantee; an interrupted request can be billed again.
	if cp.Phase != "poll" && cp.Attempt >= MaxID {
		return h.finish(task, cp, messageReceipt{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, ErrorCode: "attempt_exhausted"}, false)
	}
	if cp.Phase != "poll" {
		cp.Attempt++
	}
	task.Status = "running"
	value, e := dtoObject(cp, h.session.limits)
	if e != nil {
		return e
	}
	task.Checkpoint = value
	if _, e = h.session.invocationCommit(context.Background(), task.ID, func(tx *Tx) error {
		var current generationCheckpoint
		if e := fromObject(tx.state.Tasks[task.ID].Checkpoint, &current, h.session.limits); e != nil {
			return e
		}
		if current.Abort {
			return reject("generation aborted")
		}
		return tx.stage(Write{Op: "put-task", Task: &task})
	}); e != nil {
		if _, latest, ok, err := h.nextTask(id); err == nil && ok && latest.Abort {
			return e
		}
		return e
	}
	if h.life.Err() != nil {
		return e
	}
	if cp.Model == nil {
		return reject("missing pinned model")
	}
	local, e := callModelResolver(h.options.Models, cp.Agent.Model)
	if e == nil && local == nil {
		return h.finishGenerationNoModel(task, cp, &generationNoModel{ref: cp.Agent.Model})
	}
	var model *goai.Model
	if e == nil {
		model, e = cloneModel(cp.Model, h.session.limits)
	}
	if e == nil {
		if local == nil || local.ID != cp.Agent.Model.ID || local.Provider != cp.Agent.Model.Provider {
			e = reject("model endpoint unavailable")
		} else {
			model.BaseURL = local.BaseURL
			model.Headers = make(map[string]string, len(local.Headers))
			for k, v := range local.Headers {
				model.Headers[k] = v
			}
			model.APIKey = local.APIKey
		}
	}
	if e != nil {
		return h.finish(task, cp, errorReceipt("model_unavailable"), false)
	}
	var options *goai.StreamOptions
	if h.options.RequestOptions != nil {
		options, e = callRequestOptions(h.options.RequestOptions, h.life, cp.Agent.Model)
		if e != nil {
			if h.life.Err() != nil {
				return e
			}
			return h.finish(task, cp, errorReceipt("request_options_unavailable"), false)
		}
	}
	options, e = cloneOptions(options, h.session.limits)
	if e != nil {
		return h.finish(task, cp, errorReceipt("invalid_options"), false)
	}
	if err := applyRequestSettings(options, cp.Agent.Settings, h.session.limits); err != nil {
		return h.finish(task, cp, errorReceipt("invalid_options"), false)
	}
	options.Reasoning = nil
	if cp.Agent.ThinkingLevel != "" && cp.Agent.ThinkingLevel != "off" {
		level := goai.ThinkingLevel(cp.Agent.ThinkingLevel)
		options.Reasoning = &level
	}
	conv := &goai.Context{SystemPrompt: cp.Agent.SystemPrompt}
	conv.Tools, e = protocolTools(cp.Offered, h.session.limits)
	if e != nil {
		return e
	}
	requestMessages := cp.Messages
	hooks := runtime.phaseSelection().selectedHooks(h.selectionAgent(cp.Agent))
	for _, hook := range hooks {
		if hook.BeforeRequest == nil || cp.Phase == "poll" {
			continue
		}
		copy, err := detachReceipts(requestMessages, h.session.limits)
		if err != nil {
			return err
		}
		next, err := callBeforeRequest(runtime.context, hook.BeforeRequest, copy, runtime)
		if err != nil {
			if runtime.context.Err() != nil {
				return runtime.context.Err()
			}
			h.scheduler.report(err)
			continue
		}
		if next != nil {
			owned, err := detachReceipts(next, h.session.limits)
			if err != nil {
				h.scheduler.report(err)
				continue
			}
			requestMessages = owned
		}
	}
	for _, m := range requestMessages {
		conv.Messages = append(conv.Messages, receiptMessage(m))
	}
	if e = h.session.readTasks(context.Background(), func(state Snapshot) error {
		current := state.Tasks[task.ID]
		if h.closing.Load() || taskAborted(current) || taskBelowCancelled(state, current) || taskSelectedFailFastCancellation(state, current.ID) {
			h.scheduler.endOnLine(runtime)
			return ErrSealed
		}
		return nil
	}); e != nil {
		return e
	}
	var receipt MessageReceipt
	var success, received bool
	if cp.Phase == "poll" {
		if cp.Deferred == nil {
			return h.finish(task, cp, errorReceipt("invalid_deferred_handle"), false)
		}
		if err := runtime.SleepUntil(runtime.context, cp.PollAt); err != nil {
			return err
		}
		message, err := goai.FetchDeferred(runtime.context, model, *cp.Deferred, options)
		if err != nil {
			if runtime.context.Err() != nil {
				return err
			}
			return &generationProviderFault{cause: err}
		}
		events := make(chan goai.Event, 1)
		events <- &goai.DoneEvent{Reason: message.StopReason, Message: message}
		close(events)
		receipt, success, received = h.drain(events, cp.Model)
	} else {
		receipt, success, received = h.drain(goai.Stream(runtime.context, model, conv, options), cp.Model, task.ID)
	}
	// Close joins the full provider channel, including a noncooperative provider.
	// Its cancellation never invents an aborted durable terminal outcome.
	if h.life.Err() != nil && !received {
		return e
	}
	if current, checkpoint, ok, err := h.nextTask(id); err != nil {
		return err
	} else if ok && current.ID == task.ID && checkpoint.Abort {
		if e = h.drainAborted(current, checkpoint, receipt); e != nil {
			return e
		}
		return e
	}
	for _, hook := range hooks {
		if hook.AfterResponse == nil || receipt.StopReason == goai.StopReasonDeferred {
			continue
		}
		copy, err := detachReceipts([]MessageReceipt{receipt}, h.session.limits)
		if err != nil {
			return err
		}
		if err := callAfterResponse(runtime.context, hook.AfterResponse, copy[0], runtime); err != nil {
			if runtime.context.Err() != nil {
				return runtime.context.Err()
			}
			h.scheduler.report(err)
		}
	}
	if success && receipt.StopReason == goai.StopReasonDeferred && receipt.Deferred != nil {
		provider := goai.GetApiProvider(cp.Model.Api)
		if provider == nil || provider.FetchDeferred == nil {
			return h.finish(task, cp, errorReceipt("deferred_unsupported"), false)
		}
		now, err := runtime.Now()
		if err != nil {
			return err
		}
		delay := int64(receipt.Deferred.PollAfterMs)
		if delay < 1 {
			delay = 5000
		}
		poll := retryDeadline(now, delay)
		if poll <= cp.PollAt {
			poll = retryDeadline(cp.PollAt, 1)
		}
		cp.Phase, cp.Deferred, cp.PollAt = "poll", receipt.Deferred, poll
		checkpoint, err := dtoObject(cp, h.session.limits)
		if err != nil {
			return err
		}
		_, err = h.session.invocationCommit(runtime.context, task.ID, func(tx *Tx) error {
			if h.closing.Load() || taskAborted(tx.state.Tasks[task.ID]) {
				return ErrSealed
			}
			owned, err := copyTask(tx.state.Tasks[task.ID], tx.limits)
			if err != nil {
				return err
			}
			owned.Status, owned.Checkpoint = "pending", checkpoint
			return tx.stage(Write{Op: "put-task", Task: &owned})
		})
		return err
	}
	toolCalls := false
	for _, block := range receipt.Content {
		if block.Type == "toolCall" {
			toolCalls = true
			break
		}
	}
	if success && receipt.StopReason == goai.StopReasonToolUse && toolCalls {
		if e = h.acceptTools(task, cp, receipt); e != nil {
			if _, latest, ok, err := h.nextTask(id); err == nil && ok && latest.Abort {
				return e
			}
			return e
		}
		h.notify()
		return e
	}
	if !success && h.life.Err() == nil && receipt.ContextOverflow {
		started, err := h.startGenerationCompaction(runtime, task, &cp, true, &receipt)
		if err != nil {
			return err
		}
		if started {
			return nil
		}
		return h.finish(task, cp, receipt, false)
	}
	settings := cp.Agent.Settings
	if !success && h.life.Err() == nil {
		if err := h.session.readTasks(runtime.context, func(state Snapshot) error {
			if doc, ok := agentDocument(state, task.Conversation); ok {
				var current agentState
				if err := fromObject(doc.Value, &current, h.session.limits); err != nil {
					return err
				}
				settings = h.resolvedSettings(current.Settings)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	if !success && h.life.Err() == nil && settings.Retry.Enabled && cp.RetryCount < settings.Retry.MaxRetries && retryableReceipt(receipt) {
		cp.RetryCount++
		now, err := runtime.Now()
		if err != nil {
			return err
		}
		cp.Phase = "retry"
		cp.RetryMessage = receipt.ErrorMessage
		cp.Deferred, cp.PollAt = nil, 0
		cp.RetryUntil = retryDeadline(now, retryDelay(settings.Retry, cp.RetryCount))
		checkpoint, err := dtoObject(cp, h.session.limits)
		if err != nil {
			return err
		}
		task.Checkpoint = checkpoint
		_, err = h.session.invocationCommit(runtime.context, task.ID, func(tx *Tx) error {
			if h.closing.Load() || taskAborted(tx.state.Tasks[task.ID]) {
				return ErrSealed
			}
			usage, err := builtin(tx, task.Conversation, "pi.usage")
			if err != nil {
				return err
			}
			if err = usage.Update(func(value JSON) error { return addModelUsage(value, receipt, tx.limits) }); err != nil {
				return err
			}
			value, err := dtoObject(receipt, tx.limits)
			if err != nil {
				return err
			}
			entry, err := tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.AppendEntry(Entry{ID: entry, Conversation: task.Conversation, Kind: "message", Value: value, ByTask: task.ID}); err != nil {
				return err
			}
			owned, err := copyTask(tx.state.Tasks[task.ID], tx.limits)
			if err != nil {
				return err
			}
			owned.Status, owned.Checkpoint = "pending", checkpoint
			return tx.stage(Write{Op: "put-task", Task: &owned})
		})
		return err
	}
	if success && (receipt.StopReason == goai.StopReasonStop || receipt.StopReason == goai.StopReasonLength || receipt.StopReason == goai.StopReasonToolUse) {
		for _, hook := range hooks {
			if hook.OnYield == nil {
				continue
			}
			copy, err := detachReceipts([]MessageReceipt{receipt}, h.session.limits)
			if err != nil {
				return err
			}
			input, err := callOnYield(runtime.context, hook.OnYield, copy[0], runtime)
			if err != nil {
				if runtime.context.Err() != nil {
					return runtime.context.Err()
				}
				h.scheduler.report(err)
				continue
			}
			if input == nil {
				continue
			}
			continued, err := h.continueYield(runtime, task, cp, receipt, *input)
			if err != nil {
				return err
			}
			if continued {
				return nil
			}
			break
		}
	}
	if e = h.finish(task, cp, receipt, success); e != nil {
		if _, latest, ok, err := h.nextTask(id); err == nil && ok && latest.Abort {
			return e
		}
		return e
	}

	h.notify()
	return nil
}

// These fences cover application callbacks only. Clone/codec, Session/storage
// admissions and scheduler invariants remain outside their recovery scopes.
func callModelResolver(resolve func(goai.Provider, string) *goai.Model, ref ModelRef) (model *goai.Model, err error) {
	defer func() {
		if recover() != nil {
			model = nil
			err = reject("model resolver callback panic")
		}
	}()
	return resolve(ref.Provider, ref.ID), nil
}

func callRequestOptions(resolve func(context.Context, ModelRef) (*goai.StreamOptions, error), ctx context.Context, ref ModelRef) (options *goai.StreamOptions, err error) {
	defer func() {
		if recover() != nil {
			options = nil
			err = reject("request options callback panic")
		}
	}()
	return resolve(ctx, ref)
}

func (h *Harness) prepareRequest(runtime *TaskRuntime, task *Task, cp *generationCheckpoint) (err error) {
	original := *cp
	continuation := cp.Phase == "prepare-next"
	defer func() {
		if err != nil {
			*cp = original
		}
	}()
	s, e := h.session.Snapshot(context.Background())
	if e != nil {
		return e
	}
	cp.Agent = agentState{}
	if d, ok := agentDocument(s, task.Conversation); ok {
		if e = fromObject(d.Value, &cp.Agent, h.session.limits); e != nil {
			return e
		}
	}
	cp.Agent.Settings = h.resolvedSettings(cp.Agent.Settings)
	if cp.Agent.Model.ID == "" {
		return &generationNoModel{ref: cp.Agent.Model}
	}
	view, e := deriveContextView(s, task.Conversation, 0, h.session.limits)
	if e != nil {
		return e
	}
	messages := view.Messages
	// Validate the process-local options seam before committing a request intent;
	// only auth/read-only host observations are permitted, never payload rewrites.
	if h.options.RequestOptions != nil {
		resolved, err := callRequestOptions(h.options.RequestOptions, h.life, cp.Agent.Model)
		if err != nil {
			return reject("request options unavailable")
		}
		if _, err = cloneOptions(resolved, h.session.limits); err != nil {
			return err
		}
	}
	// Pin all non-secret model behavior before the intent write. Endpoint and
	// credentials are erased from persistence and rehydrated process-locally.
	local, e := callModelResolver(h.options.Models, cp.Agent.Model)
	if e != nil {
		return e
	}
	if local == nil {
		return &generationNoModel{ref: cp.Agent.Model}
	}
	model, e := cloneModel(local, h.session.limits)
	if e != nil {
		return e
	}
	if model.ID != cp.Agent.Model.ID || model.Provider != cp.Agent.Model.Provider {
		return reject("model identity mismatch")
	}
	model.APIKey = ""
	model.Headers = nil
	model.BaseURL = ""
	cp.Model = model
	offers, pins, selectedSections, _, e := runtime.phaseSelection().selectedGeneration(h.selectionAgent(cp.Agent), h.session.limits, h.scheduler.report)
	if e != nil {
		return e
	}
	cp.Offered = offers
	var promptEntries []Entry
	sections := append(selectedSections, h.options.Sections...)
	sections = withInstructionSection(sections, cp.Agent.Instructions)
	// Preserve unmanaged historical contributions at initial preparation; once
	// native pi.system entries exist, loadout changes are explicit transcript patches.
	managed := len(offers) > 0 || cp.Agent.Tools != nil || cp.Agent.Extensions != nil || cp.Agent.ToolsRemoved != nil || cp.Agent.ExtensionFilter != nil
	for _, entry := range view.Entries {
		if entry.Kind == "pi.system" {
			managed = true
		}
	}
	if len(sections) > 0 || managed || continuation && len(ReplayPromptSections(messages)) > 0 {
		tools, err := protocolTools(offers, h.session.limits)
		if err != nil {
			return err
		}
		shown := ReplayPromptSections(messages)
		environment, envErr := resolveInvocationEnvironment(runtime.context, h, runtime, task.Conversation, false)
		if envErr != nil {
			if runtime.context.Err() != nil {
				return runtime.context.Err()
			}
			h.scheduler.report(envErr)
		}
		desired, err := RenderPromptSections(runtime.context, sections, PromptInput{Env: environment, Read: &InvocationReader{runtime}, Shown: shown, Conversation: task.Conversation, Agent: AgentChange{Name: cp.Agent.Name, Cwd: cp.Agent.Cwd, Model: cp.Agent.Model, ThinkingLevel: cp.Agent.ThinkingLevel, SystemPrompt: cp.Agent.SystemPrompt, Instructions: cp.Agent.Instructions, Settings: cp.Agent.Settings, Extensions: cp.Agent.Extensions, Tools: cp.Agent.Tools, ExtensionFilter: cp.Agent.ExtensionFilter, ToolsRemoved: cp.Agent.ToolsRemoved}, Tools: tools, Messages: messages}, ReplayPromptSections(messages), h.options.OnReport)
		if err != nil {
			return err
		}
		if len(sections) == 0 && !continuation {
			// Legacy unmanaged contributions remain readable, but clearing the
			// reserved agent field must remove its persisted section.
			for _, section := range shown {
				if section.Key != "instructions" {
					desired = append(desired, section)
				}
			}
		}
		now := time.Now().UnixMilli()
		if h.options.Now != nil {
			now = h.options.Now()
		}
		promptEntries, err = PlanSystemEntries(view, desired, tools, now, h.session.limits)
		if err != nil {
			return err
		}
		for _, entry := range promptEntries {
			messages = append(messages, entry.Model...)
		}
		text := promptText(desired)
		if text != "" {
			cp.Agent.SystemPrompt += "\n\n" + text
		}
	}
	cp.Phase = "intent"
	cp.Messages = messages
	input, err := inputReceipt(cp.Input, cp.InputBlocks, h.session.limits)
	if err != nil {
		return err
	}
	if !continuation && cp.InputEntry == 0 {
		cp.Messages = append(cp.Messages, input)
	}
	value, e := dtoObject(cp, h.session.limits)
	if e != nil {
		return e
	}
	task.Status = "running"
	task.Checkpoint = value
	_, e = h.session.invocationCommit(context.Background(), task.ID, func(tx *Tx) error {
		var latest generationCheckpoint
		if e := fromObject(tx.state.Tasks[task.ID].Checkpoint, &latest, h.session.limits); e != nil {
			return e
		}
		if latest.Abort {
			return reject("generation aborted")
		}
		for _, draft := range promptEntries {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			draft.ID, draft.Conversation = id, task.Conversation
			if err = tx.AppendEntry(draft); err != nil {
				return err
			}
		}
		if !continuation && cp.InputEntry == 0 {
			id, e := tx.MintID()
			if e != nil {
				return e
			}
			user, e := dtoObject(input, h.session.limits)
			if e != nil {
				return e
			}
			if e = tx.AppendEntry(Entry{ID: id, Conversation: task.Conversation, Kind: "message", Value: user}); e != nil {
				return e
			}
			cp.InputEntry = id
			if err := markSubmissionEntry(tx, cp.Submission, id); err != nil {
				return err
			}
		}
		if !continuation {
			if err := h.applyQueuedInputs(tx, *task, cp, true); err != nil {
				return err
			}
		}
		if continuation || len(cp.Steered) > 0 || cp.InputEntry != 0 {
			state := tx.state
			var err error
			if len(tx.writes) > 0 {
				state, err = tx.current()
				if err != nil {
					return err
				}
			}
			cp.Messages, err = contextReceipts(state, task.Conversation, tx.limits)
			if err != nil {
				return err
			}
			task.Checkpoint, err = dtoObject(cp, tx.limits)
			if err != nil {
				return err
			}
		}
		current, err := copyTask(tx.state.Tasks[task.ID], tx.limits)
		if err != nil {
			return err
		}
		current.Status = task.Status
		current.Checkpoint = task.Checkpoint
		*task = current
		if e = tx.stage(Write{Op: "put-task", Task: task}); e != nil {
			return e
		}
		if continuation {
			return nil
		}
		inbox, e := builtin(tx, task.Conversation, "pi.inbox")
		if e != nil {
			return e
		}
		if e = inbox.Update(func(v JSON) error {
			items, ok := v["items"].([]any)
			if !ok {
				return reject("inbox shape")
			}
			keep := make([]any, 0, len(items))
			for _, item := range items {
				obj, ok := item.(map[string]any)
				if !ok {
					return reject("inbox item")
				}
				if fmt.Sprint(obj["id"]) != strconv.FormatUint(uint64(cp.Submission), 10) {
					keep = append(keep, item)
				}
			}
			v["items"] = keep
			return nil
		}); e != nil {
			return e
		}
		live, e := builtin(tx, task.Conversation, "pi.live")
		if e != nil {
			return e
		}
		inputs := []any{cp.Submission}
		for _, id := range cp.Steered {
			inputs = append(inputs, id)
		}
		return live.Update(func(value JSON) error { value["run"] = JSON{"task": task.ID, "inputs": inputs}; return nil })
	})
	if e == nil {
		h.mu.Lock()
		h.pins[task.ID] = pins
		h.mu.Unlock()
	}
	return e
}
func errorReceipt(code string) messageReceipt {
	return messageReceipt{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, Content: []goai.ContentBlock{}, ErrorCode: code}
}
func (h *Harness) drain(events <-chan goai.Event, pinned *goai.Model, taskID ...ID) (messageReceipt, bool, bool) {
	if pinned == nil {
		return errorReceipt("missing_pinned_model"), false, false
	}
	terminal := 0
	result := errorReceipt("missing_terminal")
	success := false
	if events == nil {
		return errorReceipt("nil_stream"), false, false
	}
	// Partial visibility is limited to explicit durable checkpoints, never raw
	// event forwarding. Consecutive duplicate prefixes are ignored.
	var lastPartial *MessageReceipt
	var progress *generationProgress
	if len(taskID) > 0 {
		progress = newGenerationProgress(func(message MessageReceipt) error { return h.commitPartial(taskID[0], message) }, func(err error) {
			if h.closing.Load() {
				return
			}
			state, readErr := h.session.Snapshot(context.Background())
			if readErr == nil && taskAborted(state.Tasks[taskID[0]]) {
				return
			}
			reportTaskError(h.options.OnReport, err)
		})
		defer progress.stop()
	}
	for event := range events {
		var message *goai.Message
		ok := false
		code := ""
		switch e := event.(type) {
		case *goai.DoneEvent:
			if e == nil {
				continue
			}
			terminal++
			message = e.Message
			ok = true
		case *goai.ErrorEvent:
			if e == nil {
				continue
			}
			terminal++
			message = e.Error
			code = "provider_error"
		default:
			if len(taskID) > 0 {
				partial := generationEventPartial(event)
				if partial != nil && len(partial.Content) > 0 {
					copy := *partial
					copy.Role, copy.ErrorMessage, copy.Deferred = goai.RoleAssistant, "", nil
					r, err := contributionReceipt(copy, h.session.limits)
					if err != nil {
						continue
					}
					r.Api, r.Provider, r.Model = pinned.Api, pinned.Provider, pinned.ID
					_, err = encodeBounded(r, h.session.limits, h.session.limits.MaxRecordBytes)
					if err != nil {
						continue
					}
					if lastPartial != nil && equalJSONValue(*lastPartial, r) {
						continue
					}
					progress.mark(r)
					lastPartial = &r
				}
			}
			continue
		}
		if message == nil {
			result = errorReceipt("missing_terminal_message")
			success = false
			continue
		}
		// Terminal provider claims are not attribution authority. Use the model
		// committed with the request, even when identity is absent or conflicting.
		r := messageReceipt{Role: goai.RoleAssistant, Content: []goai.ContentBlock{}, Api: pinned.Api, Provider: pinned.Provider, Model: pinned.ID, Usage: message.Usage, StopReason: message.StopReason, Timestamp: message.Timestamp, ErrorCode: code, Retryable: goai.IsRetryableAssistantError(message), ContextOverflow: goai.IsContextOverflow(message, pinned.ContextWindow), providerError: message.ErrorMessage, ErrorMessage: message.ErrorMessage, providerStopReason: message.StopReason, ResponseID: message.ResponseID, ResponseModel: message.ResponseModel, ProviderThinkingLevel: message.ProviderThinkingLevel, ThinkingLevel: message.ThinkingLevel, AssistantDiagnostics: message.Diagnostics, RawStopReason: message.RawStopReason, EndTurn: message.EndTurn, ContentPresence: captureContentPresence(message.Content)}
		for _, c := range message.Content {
			switch c.Type {
			case "text", "thinking":
				r.Content = append(r.Content, goai.ContentBlock{Type: c.Type, Text: c.Text, Thinking: c.Thinking, TextSignature: c.TextSignature, TextSignaturePresent: c.TextSignaturePresent, ThinkingSignature: c.ThinkingSignature, ThinkingSignaturePresent: c.ThinkingSignaturePresent, Redacted: c.Redacted, RedactedPresent: c.RedactedPresent})
			case "toolCall":
				if h.options.Registry == nil {
					ok = false
					r.ErrorCode = "tools_unsupported"
					continue
				}
				if c.ID == "" || c.Name == "" || c.Arguments == nil {
					ok = false
					r.ErrorCode = "invalid_tool_call"
					continue
				}
				arguments, e := copyObject(JSON(c.Arguments), h.session.limits)
				if e != nil {
					ok = false
					r.ErrorCode = "invalid_tool_arguments"
					continue
				}
				if len(arguments) == 0 {
					r.EmptyArguments = append(r.EmptyArguments, len(r.Content))
				}
				r.Content = append(r.Content, goai.ContentBlock{Type: "toolCall", ID: c.ID, Name: c.Name, Arguments: arguments, ThoughtSignature: c.ThoughtSignature, ThoughtSignaturePresent: c.ThoughtSignaturePresent, Namespace: c.Namespace, NamespacePresent: c.NamespacePresent})
			default:
				ok = false
				r.ErrorCode = "content_unsupported"
			}
		}
		switch message.StopReason {
		case goai.StopReasonStop, goai.StopReasonLength:
			for _, content := range r.Content {
				if content.Type == "toolCall" {
					ok = false
					r.ErrorCode = "unexpected_tool_call"
				}
			}
		case goai.StopReasonToolUse:
			// Empty toolUse is a final answer in the reference runtime.
		case goai.StopReasonDeferred:
			if message.Deferred == nil || message.Deferred.ID == "" {
				ok = false
				r.ErrorCode = "invalid_deferred_handle"
			} else {
				handle := *message.Deferred
				if handle.Provider != "" && handle.Provider != string(pinned.Provider) || handle.ModelID != "" && handle.ModelID != pinned.ID || handle.Api != "" && handle.Api != string(pinned.Api) {
					ok = false
					r.ErrorCode = "invalid_deferred_identity"
				} else {
					if handle.Data != nil {
						owned, e := ownJSONValue(handle.Data, h.session.limits)
						if e != nil {
							ok = false
							r.ErrorCode = "invalid_deferred_data"
						} else {
							handle.Data = owned
						}
					}
					r.Deferred = &handle
				}
			}
		case goai.StopReasonPending:
			ok = false
			r.ErrorCode = "invalid_deferred_handle"
		default:
			ok = false
			if r.ErrorCode == "" {
				r.ErrorCode = "provider_error"
			}
		}
		if !ok {
			r.StopReason = goai.StopReasonError
		}
		if r.Usage != nil && (r.Usage.Input < 0 || r.Usage.Output < 0 || r.Usage.CacheRead < 0 || r.Usage.CacheWrite < 0 || r.Usage.TotalTokens < 0 || r.Usage.CacheWrite1h < 0 || r.Usage.Reasoning < 0 || r.Usage.Cost.Input < 0 || r.Usage.Cost.Output < 0 || r.Usage.Cost.CacheRead < 0 || r.Usage.Cost.CacheWrite < 0 || r.Usage.Cost.Total < 0) {
			result = errorReceipt("invalid_usage")
			success = false
			continue
		}
		value, e := dtoObject(r, h.session.limits)
		if e != nil {
			result = errorReceipt("invalid_terminal")
			success = false
			continue
		}
		var owned messageReceipt
		if e = fromObject(value, &owned, h.session.limits); e != nil {
			result = errorReceipt("invalid_terminal")
			success = false
			continue
		}
		owned.providerError, owned.providerStopReason = r.providerError, r.providerStopReason
		result = owned
		success = ok
	}
	if terminal != 1 {
		invalid := errorReceipt("invalid_terminal_count")
		invalid.Usage = result.Usage
		return invalid, false, terminal > 0
	}
	return result, success, terminal == 1
}
func (h *Harness) finish(task Task, cp generationCheckpoint, r messageReceipt, success bool) error {
	if cp.Model != nil {
		r.Api = cp.Model.Api
		r.Provider = cp.Model.Provider
		r.Model = cp.Model.ID
	}
	status := "failed"
	if cp.Abort {
		status = "aborted"
		r.StopReason = goai.StopReasonAborted
		r.ErrorCode = "aborted"
	}
	if success && !cp.Abort {
		status = "done"
	}
	cp.Phase = "terminal"
	checkpoint, e := dtoObject(cp, h.session.limits)
	if e != nil {
		return e
	}
	task.Status = status
	task.Checkpoint = checkpoint
	value, e := dtoObject(r, h.session.limits)
	if e != nil {
		return e
	}
	_, e = h.session.invocationCommit(context.Background(), task.ID, func(tx *Tx) error {
		sub, ok := tx.state.Submissions[cp.Submission]
		if !ok {
			return reject("submission unavailable")
		}
		current := tx.state.Tasks[task.ID]
		if terminalStatus(sub.Status) || taskHasDecidedOutcome(current) {
			return nil
		}
		var latest generationCheckpoint
		if e := fromObject(tx.state.Tasks[task.ID].Checkpoint, &latest, h.session.limits); e != nil {
			return e
		}
		if latest.Abort && !cp.Abort {
			return reject("abort precedes generation outcome")
		}
		id, e := tx.MintID()
		if e != nil {
			return e
		}
		if e = tx.AppendEntry(Entry{ID: id, Conversation: task.Conversation, Kind: "message", Value: value, ByTask: task.ID}); e != nil {
			return e
		}
		outcome := TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: JSON{"entryId": id}}}
		if status == "failed" {
			outcome = TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: r.ErrorCode}, Result: &TaskValue{Present: true, Value: JSON{"entryId": id}}}
			if r.ErrorCode == "provider_error" {
				outcome.Error.Message = r.ErrorMessage
				if outcome.Error.Message == "" {
					outcome.Error.Message = fmt.Sprintf("Model response ended with stop reason %s", r.StopReason)
				}
				outcome.Error.Detail = &TaskValue{Present: true, Value: JSON{"reason": "model_error"}}
			}
			if outcome.Error.Message == "" {
				outcome.Error.Message = "provider_error"
			}
		}
		if status == "aborted" {
			outcome = TaskOutcome{Status: "aborted", Reason: "aborted", Result: &TaskValue{Present: true, Value: JSON{"entryId": id}}}
		}
		hold := &BuiltinTaskHold{Stage: "final", Action: "generation-receipt", Outcome: outcome, FinalStatus: status, Entry: id, Submission: cp.Submission, Conversation: task.Conversation, Owner: task.Owner}
		metadata := &BuiltinTaskExecution{}
		if current.Execution != nil {
			*metadata = *current.Execution.Builtin
		}
		metadata.AbortRequested = cp.Abort || taskAborted(current)
		metadata.Memos = nil
		metadata.Hold = hold
		task.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: metadata}
		candidate, err := tx.current()
		if err != nil {
			return err
		}
		if len(h.scheduler.ownedLive(candidate, task.ID)) > 0 {
			hold.Stage = "held"
			task.Status = "completing"
		}
		// endRun settles inputs at the deciding commit. Owned work holds the
		// task's terminal state, not its answer or the next run boundary.
		if err := h.cleanupGeneration(tx, task, hold, value); err != nil {
			return err
		}
		if e = tx.stage(Write{Op: "put-task", Task: &task}); e != nil {
			return e
		}
		// Aggregate non-secret model usage is adopted with the single outcome.
		var existing Document
		found := false
		for _, d := range tx.state.Documents {
			if !d.Retired && d.Scope == "conversation" && d.Owner == task.Conversation && d.Kind == "pi.usage" {
				existing = d
				found = true
				break
			}
		}
		if found {
			doc, e := tx.Document(existing.ID)
			if e != nil {
				return e
			}
			return doc.Update(func(v JSON) error { return addModelUsage(v, r, h.session.limits) })
		}
		docID, e := tx.MintID()
		if e != nil {
			return e
		}
		state := JSON{"models": JSON{}, "tools": JSON{}}
		if e = addModelUsage(state, r, h.session.limits); e != nil {
			return e
		}
		_, e = tx.CreateDocument(Document{ID: docID, Scope: "conversation", Owner: task.Conversation, Kind: "pi.usage", Version: 1, Value: state})
		return e
	})
	return e
}

func validUsage(usage *goai.Usage) bool {
	if usage == nil {
		return true
	}
	for _, n := range []int{usage.Input, usage.Output, usage.CacheRead, usage.CacheWrite, usage.CacheWrite1h, usage.Reasoning, usage.TotalTokens} {
		if n < 0 || uint64(n) > MaxID {
			return false
		}
	}
	for _, n := range []float64{usage.Cost.Input, usage.Cost.Output, usage.Cost.CacheRead, usage.Cost.CacheWrite, usage.Cost.Total} {
		if n < 0 || math.IsNaN(n) || math.IsInf(n, 0) {
			return false
		}
	}
	return true
}
func addModelUsage(state JSON, r messageReceipt, l Limits) error {
	if r.Usage == nil {
		return nil
	}
	var models map[string]any
	switch v := state["models"].(type) {
	case JSON:
		models = map[string]any(v)
	case map[string]any:
		models = v
	default:
		return reject("usage document shape")
	}
	key := string(r.Provider) + "/" + r.Model
	var prior goai.Usage
	if old, ok := models[key]; ok {
		var object JSON
		switch value := old.(type) {
		case JSON:
			object = value
		case map[string]any:
			object = JSON(value)
		default:
			return reject("usage counter shape")
		}
		if e := fromObject(object, &prior, l); e != nil {
			return e
		}
	}
	add := func(a, b int) (int, error) {
		if a < 0 || b < 0 || uint64(a) > MaxID || uint64(b) > MaxID || uint64(a) > MaxID-uint64(b) {
			return 0, reject("usage overflow")
		}
		return a + b, nil
	}
	current := *r.Usage
	for _, pair := range []struct {
		out  *int
		a, b int
	}{{&current.Input, prior.Input, current.Input}, {&current.Output, prior.Output, current.Output}, {&current.CacheRead, prior.CacheRead, current.CacheRead}, {&current.CacheWrite, prior.CacheWrite, current.CacheWrite}, {&current.CacheWrite1h, prior.CacheWrite1h, current.CacheWrite1h}, {&current.Reasoning, prior.Reasoning, current.Reasoning}, {&current.TotalTokens, prior.TotalTokens, current.TotalTokens}} {
		n, e := add(pair.a, pair.b)
		if e != nil {
			return e
		}
		*pair.out = n
	}
	current.Cost.Input += prior.Cost.Input
	current.Cost.Output += prior.Cost.Output
	current.Cost.CacheRead += prior.Cost.CacheRead
	current.Cost.CacheWrite += prior.Cost.CacheWrite
	current.Cost.Total += prior.Cost.Total
	value, e := dtoObject(current, l)
	if e != nil {
		return e
	}
	models[key] = value
	return nil
}

func (h *Harness) commitPartial(id ID, partial messageReceipt) error {
	if _, e := dtoObject(partial, h.session.limits); e != nil {
		return e
	}
	_, e := h.session.invocationCommit(context.Background(), id, func(tx *Tx) error {
		task, ok := tx.state.Tasks[id]
		if !ok || terminalStatus(task.Status) {
			return ErrSealed
		}
		var cp generationCheckpoint
		if e := fromObject(task.Checkpoint, &cp, h.session.limits); e != nil {
			return e
		}
		if cp.Abort {
			return reject("generation aborted")
		}
		cp.Partial = &partial
		value, e := dtoObject(cp, h.session.limits)
		if e != nil {
			return e
		}
		task.Checkpoint = value
		return tx.stage(Write{Op: "put-task", Task: &task})
	})
	return e
}

func (h *Harness) sessionCommitInterruptedPartial(task Task, cp generationCheckpoint) error {
	_, e := h.session.invocationCommit(context.Background(), task.ID, func(tx *Tx) error {
		current := tx.state.Tasks[task.ID]
		var latest generationCheckpoint
		if e := fromObject(current.Checkpoint, &latest, h.session.limits); e != nil {
			return e
		}
		if latest.Abort {
			return reject("generation aborted")
		}
		if latest.Partial == nil {
			return nil
		}
		receipt := *latest.Partial
		receipt.StopReason = goai.StopReasonAborted
		id, e := tx.MintID()
		if e != nil {
			return e
		}
		value, e := dtoObject(receipt, h.session.limits)
		if e != nil {
			return e
		}
		if e = tx.AppendEntry(Entry{ID: id, Conversation: task.Conversation, Kind: "message", Value: value, ByTask: task.ID}); e != nil {
			return e
		}
		usage, err := builtin(tx, task.Conversation, "pi.usage")
		if err != nil {
			return err
		}
		if err := usage.Update(func(value JSON) error { return addModelUsage(value, receipt, tx.limits) }); err != nil {
			return err
		}
		latest.Partial = nil
		value, e = dtoObject(latest, h.session.limits)
		if e != nil {
			return e
		}
		owned, err := copyTask(current, tx.limits)
		if err != nil {
			return err
		}
		owned.Checkpoint = value
		return tx.stage(Write{Op: "put-task", Task: &owned})
	})
	return e
}
