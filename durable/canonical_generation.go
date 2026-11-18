package durable

import (
	"context"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"sync"
)

var canonicalGenerationOnce sync.Once
var canonicalGenerationDefinition *TaskDefinition

// GenerationTask is the source pi.generation v2 token. Its checkpoint is the
// public prepare/request/retry/poll/tools/finish/drain union.
func GenerationTask() *TaskDefinition {
	canonicalGenerationOnce.Do(func() {
		var err error
		canonicalGenerationDefinition, err = DefineTask(TaskDefinitionOptions{Kind: "pi.generation", Version: 2, Initial: func(any) (JSON, error) { return JSON{"phase": "prepare", "attempt": 1}, nil }, Migrate: func(input, checkpoint any, _ uint64) (any, JSON, error) {
			return input, taskCheckpointMap(checkpoint), nil
		}, Phases: map[string]TaskPhase{"prepare": canonicalGenerationPrepare, "request": canonicalGenerationRequest, "retry": canonicalGenerationRetry, "poll": canonicalGenerationPoll, "tools": canonicalGenerationTools, "finish": canonicalGenerationFinish, "drain": canonicalGenerationDrain}, Abort: canonicalGenerationAbort})
		if err != nil {
			panic(err)
		}
	})
	return canonicalGenerationDefinition
}
func canonicalGenerationAgent(ctx context.Context, r *TaskRuntime) (TaskAgent, agentState, error) {
	agent, err := r.Agent(ctx)
	if err != nil {
		return agent, agentState{}, err
	}
	state, err := r.harness.agent(agent.Configuration)
	return agent, state, err
}
func canonicalGenerationLive(tx *Tx, r *TaskRuntime) (*DocumentHandle, JSON, error) {
	doc, err := builtin(tx, r.conversation, "pi.live")
	if err != nil {
		return nil, nil, err
	}
	value, err := doc.Get()
	return doc, value, err
}
func canonicalAppendAssistant(tx *Tx, conversation ID, message MessageReceipt) (ID, error) {
	usage, err := builtin(tx, conversation, "pi.usage")
	if err != nil {
		return 0, err
	}
	if err := usage.Update(func(value JSON) error { return addModelUsage(value, message, tx.limits) }); err != nil {
		return 0, err
	}
	wire, err := dtoObject(message, tx.limits)
	if err != nil {
		return 0, err
	}
	entry, err := tx.AppendEntry(conversation, JSON{"kind": assistantEntryKind, "model": []any{wire}})
	return entry.ID, err
}
func canonicalConvertPartial(tx *Tx, conversation ID, live JSON) error {
	partial := generationObject(generationObject(live["generation"])["message"])
	if partial == nil {
		return nil
	}
	var message MessageReceipt
	if err := fromObject(partial, &message, tx.limits); err != nil {
		return err
	}
	message.StopReason = goai.StopReasonAborted
	_, err := canonicalAppendAssistant(tx, conversation, message)
	return err
}
func canonicalFailedRun(tx *Tx, r *TaskRuntime, live JSON, boundary *canonicalBoundary, reason, message string) error {
	var detail *string
	// failNothingToAnswer keeps the explanatory text only in the task error,
	// not in either the submission settlement or LiveDoc runFailure.
	if message != "" && reason != "nothing_to_answer" {
		detail = &message
	}
	if err := canonicalEndRun(tx, live, r.taskID, 0, reason, detail); err != nil {
		return err
	}
	switch r.harness.AfterFailedRun() {
	case "withdraw":
		return canonicalWithdrawInputs(tx, r.conversation, "run_failed")
	case "continue":
		if boundary != nil {
			users, first, _, err := boundary.apply(tx, true, r.harness.session.now())
			if err != nil {
				return err
			}
			if len(users) > 0 {
				_, err = canonicalStartRun(tx, r.conversation, live, users, first, true)
			}
			return err
		}
	}
	return nil
}
func canonicalGenerationFail(ctx context.Context, r *TaskRuntime, reason, message string, salvage ...bool) error {
	return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		settings := r.harness.resolvedSettings(RequestSettings{})
		var boundary *canonicalBoundary
		var err error
		if r.harness.AfterFailedRun() == "continue" {
			boundary, err = prepareCanonicalBoundary(tx, r.conversation, settings)
			if err != nil {
				return nil, err
			}
		}
		doc, live, err := canonicalGenerationLive(tx, r)
		if err != nil {
			return nil, err
		}
		if len(salvage) > 0 && salvage[0] {
			if err := canonicalConvertPartial(tx, r.conversation, live); err != nil {
				return nil, err
			}
		}
		if err := canonicalFailedRun(tx, r, live, boundary, reason, message); err != nil {
			return nil, err
		}
		if err := doc.Set(live); err != nil {
			return nil, err
		}
		return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: message, Detail: &TaskValue{Present: true, Value: JSON{"reason": reason}}}}}, nil
	})
}
func canonicalGenerationNoModel(ctx context.Context, r *TaskRuntime, ref ModelRef) error {
	message := "No model is configured"
	if ref.ID != "" {
		message = fmt.Sprintf("Model %s/%s is not available", ref.Provider, ref.ID)
	}
	return canonicalGenerationFail(ctx, r, "no_model", message)
}
func canonicalGenerationCutOff(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
	cp := taskCheckpointMap(task.State.Checkpoint)
	phase, _ := cp["phase"].(string)
	if phase == "finish" || phase == "drain" {
		answer := submissionEntryID(cp["answer"])
		if phase == "finish" {
			answer = submissionEntryID(generationObject(cp["run"])["answer"])
		}
		return canonicalGenerationEndAnswered(ctx, r, answer, true, fmt.Sprintf("The generation was interrupted %s in its %s phase", canonicalCutOffCount(task.Restarts), phase))
	}
	salvage := task.Restarts <= r.harness.RestartLimit()
	if salvage {
		canonicalGenerationCancelDeferred(ctx, r, cp)
	}
	return canonicalGenerationFail(ctx, r, "restart_limit", fmt.Sprintf("The generation was interrupted %s in its %s phase", canonicalCutOffCount(task.Restarts), phase), salvage)
}
func canonicalGenerationPrepare(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
	if task.Restarts >= r.harness.RestartLimit() {
		return canonicalGenerationCutOff(ctx, task, r)
	}
	agent, state, err := canonicalGenerationAgent(ctx, r)
	if err != nil {
		return err
	}
	if state.Model.ID == "" {
		return canonicalGenerationNoModel(ctx, r, state.Model)
	}
	model, err := callModelResolver(r.harness.options.Models, state.Model)
	if err != nil {
		return err
	}
	if model == nil {
		return canonicalGenerationNoModel(ctx, r, state.Model)
	}
	cp := taskCheckpointMap(task.State.Checkpoint)
	attempt := submissionEntryID(cp["attempt"])
	compacted := submissionEntryID(cp["compacted"])
	if compacted != 0 && (cp["overflow"] != nil || cp["threshold"] == true) {
		outcomes, err := r.Outcomes(ctx, []ID{compacted})
		if err != nil {
			return err
		}
		outcome := outcomes[0]
		if text, overflow := cp["overflow"].(string); overflow {
			if outcome.Status != "completed" || outcome.Result == nil || submissionEntryID(generationObject(outcome.Result.Value)["entryId"]) == 0 {
				return canonicalGenerationFail(ctx, r, "model_error", text)
			}
		} else if outcome.Status != "completed" {
			text := "Compaction was aborted"
			if outcome.Error != nil {
				text = "Compaction failed: " + outcome.Error.Message
			} else if outcome.Status == "orphaned" {
				text = "Compaction failed: " + outcome.Reason
			}
			return canonicalGenerationFail(ctx, r, "model_error", text)
		}
	}
	view, err := r.ContextView(ctx, r.conversation, 0)
	if err != nil {
		return err
	}
	last := goai.Role("")
	for index := len(view.Messages) - 1; index >= 0; index-- {
		if view.Messages[index].Role != goai.RoleSystem {
			last = view.Messages[index].Role
			break
		}
	}
	if last != goai.RoleUser && last != goai.RoleToolResult {
		return canonicalGenerationFail(ctx, r, "nothing_to_answer", "The conversation has nothing to answer")
	}
	shown := ReplayPromptSections(view.Messages)
	env, envErr := resolveInvocationEnvironment(ctx, r.harness, r, r.conversation, false)
	if envErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r.reportHostError(envErr, "env")
	}
	sections := append(append([]PromptSection{}, agent.Sections...), r.harness.options.Sections...)
	desired, err := RenderPromptSections(ctx, sections, PromptInput{Conversation: r.conversation, Agent: agent.Configuration, Env: env, Shown: shown, Tools: agent.Tools, Messages: view.Messages, Read: &InvocationReader{r}}, shown, r.reportError)
	if err != nil {
		return err
	}
	entries, err := PlanSystemEntries(view, desired, agent.Tools, r.harness.session.now(), r.harness.session.limits)
	if err != nil {
		return err
	}
	// Rendering can await user code. Source settings are live getters at this
	// decision, so the earlier agent snapshot must not freeze threshold policy.
	policy := compactionPolicy(r.harness.resolvedSettings(RequestSettings{}), state.Model)
	blocking, background := false, false
	if compacted == 0 && policy.Enabled && model.ContextWindow > 0 && compactionCut(view, policy.KeepRecentTokens) > 0 {
		tokens := estimateContextTokens(view)
		for _, entry := range entries {
			for _, message := range entry.Model {
				tokens += goai.EstimateMessageTokens(receiptMessage(message))
			}
		}
		blocking = tokens > model.ContextWindow-policy.ReserveTokens
		background = !blocking && policy.BackgroundTokens > 0 && tokens > model.ContextWindow-policy.ReserveTokens-policy.BackgroundTokens
	}
	return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		if blocking {
			id, err := canonicalCreateCompaction(tx, r, "threshold", true)
			if err != nil {
				return nil, err
			}
			checkpoint := JSON{"phase": "prepare", "attempt": attempt, "compacted": id, "threshold": true}
			return &TaskState{Status: "waiting", Checkpoint: checkpoint, On: []ID{id}, Policy: "allSettled"}, nil
		}
		cutoff, _, err := tx.LatestEntryID(r.conversation)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			draft, err := inboxEntryDraft(entry, tx.limits)
			if err != nil {
				return nil, err
			}
			appended, err := tx.AppendEntry(r.conversation, draft)
			if err != nil {
				return nil, err
			}
			cutoff = appended.ID
		}
		if background {
			_, err := canonicalCreateCompaction(tx, r, "threshold", false)
			if err != nil {
				return nil, err
			}
		}
		checkpoint := JSON{"phase": "request", "attempt": attempt, "model": JSON{"provider": string(state.Model.Provider), "modelId": state.Model.ID}, "thinkingLevel": string(state.ThinkingLevel), "streamOptions": generationStreamOptions(r.harness.resolvedSettings(RequestSettings{})), "cutoff": cutoff}
		if compacted != 0 {
			checkpoint["compacted"] = compacted
		}
		return &TaskState{Status: "running", Checkpoint: checkpoint}, nil
	})
}
func canonicalCreateCompaction(tx *Tx, r *TaskRuntime, reason string, owned bool) (ID, error) {
	live, err := builtin(tx, r.conversation, "pi.live")
	if err != nil {
		return 0, err
	}
	value, err := live.Get()
	if err != nil {
		return 0, err
	}
	if !owned {
		if items, _ := value["compactions"].([]any); len(items) > 0 {
			return 0, nil
		}
	}
	ownership := TaskOwnership{Kind: "conversation"}
	if owned {
		ownership = TaskOwnership{Kind: "task", Task: r.taskID}
	}
	id, err := tx.CreateTask(CompactionTask(), JSON{"reason": reason}, TaskOptions{Conversation: r.conversation, Ownership: ownership, Background: !owned})
	if err != nil {
		return 0, err
	}
	items, _ := value["compactions"].([]any)
	value["compactions"] = append(items, JSON{"taskId": id, "trigger": reason})
	return id, live.Set(value)
}
func canonicalModelRef(cp JSON) (ModelRef, error) {
	var ref ModelRef
	err := fromObject(generationObject(cp["model"]), &ref, DefaultLimits())
	return ref, err
}
func canonicalGenerationRequest(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
	if task.Restarts >= r.harness.RestartLimit() {
		return canonicalGenerationCutOff(ctx, task, r)
	}
	cp := taskCheckpointMap(task.State.Checkpoint)
	if task.Restarts > 0 {
		return canonicalGenerationInterrupted(ctx, r, cp)
	}
	ref, err := canonicalModelRef(cp)
	if err != nil {
		return err
	}
	model, err := callModelResolver(r.harness.options.Models, ref)
	if err != nil {
		return err
	}
	if model == nil {
		return canonicalGenerationNoModel(ctx, r, ref)
	}
	if err := r.commitProgress(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		doc, live, err := canonicalGenerationLive(tx, r)
		if err != nil {
			return nil, err
		}
		if err := canonicalConvertPartial(tx, r.conversation, live); err != nil {
			return nil, err
		}
		attempt := JSON{"attempt": cp["attempt"]}
		return nil, doc.Update(func(value JSON) error {
			value["generation"] = attempt
			return nil
		})
	}); err != nil {
		return err
	}
	view, err := r.ContextView(ctx, r.conversation, submissionEntryID(cp["cutoff"]))
	if err != nil {
		return err
	}
	messages := append([]MessageReceipt(nil), view.Messages...)
	var imageState Snapshot
	for _, message := range messages {
		if len(message.ImageChunks) != 0 {
			imageState, err = r.harness.Snapshot(ctx)
			if err != nil {
				return err
			}
			break
		}
	}
	for i, message := range messages {
		messages[i], err = hydrateReceiptImages(message, imageState, r.conversation, r.harness.session.limits)
		if err != nil {
			return err
		}
	}
	_, agent, err := canonicalGenerationAgent(ctx, r)
	if err != nil {
		return err
	}
	hooks := r.phaseSelection().selectedHooks(r.harness.selectionAgent(agent))
	for _, hook := range hooks {
		if hook.BeforeRequest == nil {
			continue
		}
		copy, err := detachReceipts(messages, r.harness.session.limits)
		if err != nil {
			return err
		}
		next, err := callBeforeRequest(ctx, hook.BeforeRequest, copy, r)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			r.reportExtensionError(err, hook.extension)
			continue
		}
		if next != nil {
			messages, err = detachReceipts(next, r.harness.session.limits)
			if err != nil {
				return err
			}
		}
	}
	options, err := canonicalGenerationOptions(ctx, r, ref, cp)
	if err != nil {
		return err
	}
	declarations := receiptMessage(MessageReceipt{Role: goai.RoleSystem, ToolsAdded: ReplayToolDeclarations(messages)})
	conversation := &goai.Context{Tools: declarations.ToolsAdded, SystemPrompt: agent.SystemPrompt}
	for _, message := range messages {
		conversation.Messages = append(conversation.Messages, receiptMessage(message))
	}
	var progress *generationProgress
	interval := int64(100)
	if settings := r.harness.resolvedSettings(RequestSettings{}).Progress; settings.PartialIntervalMs != nil {
		interval = *settings.PartialIntervalMs
	}
	progress = newRuntimeGenerationProgress(func(message MessageReceipt) error {
		return r.commitProgress(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
			doc, live, err := canonicalGenerationLive(tx, r)
			if err != nil {
				return nil, err
			}
			wire, err := dtoObject(message, tx.limits)
			if err != nil {
				return nil, err
			}
			live["generation"] = JSON{"attempt": cp["attempt"], "message": wire}
			return nil, doc.Set(live)
		})
	}, func(err error) {
		if ctx.Err() == nil {
			r.reportError(err)
		}
	}, r, progressInterval(interval))
	// Source streamResponse finally covers acquisition as well as iteration.
	// Keep the explicit stop before classify, and join on every unwind too.
	defer progress.stop()
	events := goai.Stream(ctx, model, conversation, options)
	observed := make(chan goai.Event)
	go func() {
		defer close(observed)
		for event := range events {
			if partial := generationEventPartial(event); partial != nil && len(partial.Content) > 0 {
				receipt, err := contributionReceipt(*partial, r.harness.session.limits)
				if err == nil {
					progress.mark(receipt)
				}
			}
			observed <- event
		}
	}()
	message, _, received := r.harness.drainInvocation(r, observed, model)
	progress.stop()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !received {
		return fmt.Errorf("model stream ended without a terminal response")
	}
	return canonicalGenerationClassify(ctx, r, cp, message, view.Messages)
}
func canonicalGenerationOptions(ctx context.Context, r *TaskRuntime, ref ModelRef, cp JSON) (*goai.StreamOptions, error) {
	var options *goai.StreamOptions
	var err error
	if r.harness.options.RequestOptions != nil {
		options, err = callRequestOptions(r.harness.options.RequestOptions, ctx, ref)
		if err != nil {
			return nil, err
		}
	}
	options, err = cloneOptions(options, r.harness.session.limits)
	if err != nil {
		return nil, err
	}
	raw := generationObject(cp["streamOptions"])
	var settings RequestSettings
	if raw != nil {
		copy := JSON{}
		for key, value := range raw {
			copy[key] = value
		}
		if enabled, ok := copy["deferred"].(bool); ok {
			delete(copy, "deferred")
			if enabled {
				settings.Deferred = &goai.DeferredOptions{}
			}
		}
		saved := settings.Deferred
		if err := fromObject(copy, &settings, r.harness.session.limits); err != nil {
			return nil, err
		}
		if saved != nil {
			settings.Deferred = saved
		}
	}
	if err := applyRequestSettings(options, settings, r.harness.session.limits); err != nil {
		return nil, err
	}
	options.Telemetry = TelemetryFromContext(ctx)
	options.Identity = goai.MergeClientIdentity(r.harness.options.Identity, options.Identity)
	options.SessionID, err = r.providerSessionID(ctx)
	if err != nil {
		return nil, err
	}
	if thinking, ok := cp["thinkingLevel"].(string); ok && thinking != "off" {
		level := goai.ThinkingLevel(thinking)
		options.Reasoning = &level
	}
	return options, nil
}
func canonicalGenerationPoll(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
	if task.Restarts >= r.harness.RestartLimit() {
		return canonicalGenerationCutOff(ctx, task, r)
	}
	cp := taskCheckpointMap(task.State.Checkpoint)
	if task.Restarts > 0 {
		return canonicalGenerationInterrupted(ctx, r, cp)
	}
	until := int64(submissionEntryID(cp["pollAt"]))
	now, err := r.Now()
	if err != nil {
		return err
	}
	if until > now {
		return r.SleepUntil(ctx, until, cp)
	}
	ref, err := canonicalModelRef(cp)
	if err != nil {
		return err
	}
	model, err := callModelResolver(r.harness.options.Models, ref)
	if err != nil {
		return err
	}
	if model == nil {
		return canonicalGenerationNoModel(ctx, r, ref)
	}
	var handle goai.DeferredHandle
	if err := fromObject(generationObject(cp["handle"]), &handle, r.harness.session.limits); err != nil {
		return err
	}
	options := &goai.StreamOptions{Telemetry: TelemetryFromContext(ctx)}
	message, err := goai.FetchDeferred(ctx, model, handle, options)
	if err != nil {
		return err
	}
	events := make(chan goai.Event, 1)
	events <- &goai.DoneEvent{Reason: message.StopReason, Message: message}
	close(events)
	receipt, _, _ := r.harness.drainInvocation(r, events, model)
	return canonicalGenerationClassify(ctx, r, cp, receipt, nil)
}
func canonicalGenerationRetry(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
	if task.Restarts >= r.harness.RestartLimit() {
		return canonicalGenerationCutOff(ctx, task, r)
	}
	cp := taskCheckpointMap(task.State.Checkpoint)
	now, err := r.Now()
	if err != nil {
		return err
	}
	until := int64(submissionEntryID(cp["until"]))
	if until > now {
		return r.SleepUntil(ctx, until, cp)
	}
	next := JSON{"phase": "prepare", "attempt": submissionEntryID(cp["attempt"]) + 1}
	if id, present := cp["compacted"]; present {
		next["compacted"] = id
	}
	if poll := generationObject(cp["poll"]); poll != nil {
		for key, value := range poll {
			next[key] = value
		}
		next["phase"] = "poll"
		next["pollAt"] = now
	}
	return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		doc, live, err := canonicalGenerationLive(tx, r)
		if err != nil {
			return nil, err
		}
		generation := JSON{"attempt": next["attempt"]}
		if next["phase"] == "poll" {
			generation["deferred"] = JSON{"pollAt": now}
		}
		live["generation"] = generation
		if err := doc.Set(live); err != nil {
			return nil, err
		}
		return &TaskState{Status: "running", Checkpoint: next}, nil
	})
}
func canonicalGenerationInterrupted(ctx context.Context, r *TaskRuntime, cp JSON) error {
	settings := r.harness.resolvedSettings(RequestSettings{})
	attempt := int(submissionEntryID(cp["attempt"]))
	retry := settings.Retry.Enabled && attempt <= settings.Retry.MaxRetries
	message := fmt.Sprintf("The host stopped while the model was answering, on attempt %d", attempt)
	if cp["phase"] == "poll" {
		message = fmt.Sprintf("The host stopped while the deferred answer was fetched, on attempt %d", attempt)
	}
	if !retry {
		if cp["phase"] == "poll" {
			// Source interrupted() cancels the provider job when this run
			// has no retry left; an interrupted poll must not leave it running.
			canonicalGenerationCancelDeferred(ctx, r, cp)
		}
		return canonicalGenerationFail(ctx, r, "interrupted", message)
	}
	now, err := r.Now()
	if err != nil {
		return err
	}
	until := retryDeadline(now, retryDelay(settings.Retry, attempt))
	return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		doc, live, err := canonicalGenerationLive(tx, r)
		if err != nil {
			return nil, err
		}
		if err := canonicalConvertPartial(tx, r.conversation, live); err != nil {
			return nil, err
		}
		live["generation"] = JSON{"attempt": attempt, "retry": JSON{"at": until, "error": message}}
		if err := doc.Set(live); err != nil {
			return nil, err
		}
		next := JSON{"phase": "retry", "attempt": attempt, "until": until}
		if id, present := cp["compacted"]; present {
			next["compacted"] = id
		}
		if cp["phase"] == "poll" {
			poll := JSON{}
			for _, key := range []string{"model", "thinkingLevel", "cutoff", "handle"} {
				if value, present := cp[key]; present {
					poll[key] = value
				}
			}
			next["poll"] = poll
		}
		return &TaskState{Status: "sleeping", Checkpoint: next, Until: &until}, nil
	})
}
func canonicalGenerationClassify(ctx context.Context, r *TaskRuntime, cp JSON, message MessageReceipt, messages []MessageReceipt) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if thinking, ok := cp["thinkingLevel"].(string); ok {
		message.ThinkingLevel = goai.ModelThinkingLevel(thinking)
	}
	if message.StopReason == goai.StopReasonDeferred && message.Deferred != nil {
		now, err := r.Now()
		if err != nil {
			return err
		}
		delay := message.Deferred.PollAfterMs
		if delay == 0 {
			delay = 5000
		}
		at := retryDeadline(now, int64(delay))
		previous := int64(submissionEntryID(cp["pollAt"]))
		if at <= previous {
			at = previous + 1
		}
		handle, err := dtoObject(*message.Deferred, r.harness.session.limits)
		if err != nil {
			return err
		}
		return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
			doc, live, err := canonicalGenerationLive(tx, r)
			if err != nil {
				return nil, err
			}
			live["generation"] = JSON{"attempt": cp["attempt"], "deferred": JSON{"pollAt": at}}
			if err := doc.Set(live); err != nil {
				return nil, err
			}
			next := JSON{"phase": "poll", "attempt": cp["attempt"], "model": cp["model"], "cutoff": cp["cutoff"], "handle": handle, "pollAt": at}
			for _, key := range []string{"compacted", "thinkingLevel"} {
				if value, present := cp[key]; present {
					next[key] = value
				}
			}
			return &TaskState{Status: "sleeping", Checkpoint: next, Until: &at}, nil
		})
	}
	_, agent, err := canonicalGenerationAgent(ctx, r)
	if err != nil {
		return err
	}
	for _, hook := range r.phaseSelection().selectedHooks(r.harness.selectionAgent(agent)) {
		if hook.AfterResponse != nil {
			copy, err := detachReceipts([]MessageReceipt{message}, r.harness.session.limits)
			if err != nil {
				return err
			}
			if err := callAfterResponse(ctx, hook.AfterResponse, copy[0], r); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				r.reportExtensionError(err, hook.extension)
			}
		}
	}
	calls := []goai.ContentBlock{}
	for _, block := range message.Content {
		if block.Type == "toolCall" {
			calls = append(calls, block)
		}
	}
	if message.StopReason == goai.StopReasonToolUse && len(calls) > 0 {
		return canonicalGenerationStartTools(ctx, r, cp, message, calls, messages, agent)
	}
	if message.StopReason == goai.StopReasonStop || message.StopReason == goai.StopReasonLength || message.StopReason == goai.StopReasonToolUse {
		return canonicalGenerationAnswer(ctx, r, message, agent)
	}
	// Source failResponse reads runtime.settings at classification. The
	// phase-cached agent must not freeze policy while the response streams.
	settings := r.harness.resolvedSettings(RequestSettings{})
	attempt := int(submissionEntryID(cp["attempt"]))
	retry := message.StopReason == goai.StopReasonError && !message.ContextOverflow && message.Retryable && settings.Retry.Enabled && attempt <= settings.Retry.MaxRetries
	if message.ContextOverflow && cp["compacted"] == nil && compactionPolicy(settings, agent.Model).Enabled {
		view, err := r.ContextView(ctx, r.conversation, submissionEntryID(cp["cutoff"]))
		if err != nil {
			return err
		}
		if compactionCut(view, compactionPolicy(settings, agent.Model).KeepRecentTokens) > 0 {
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				if _, err := canonicalAppendAssistant(tx, r.conversation, message); err != nil {
					return nil, err
				}
				id, err := canonicalCreateCompaction(tx, r, "overflow", true)
				if err != nil {
					return nil, err
				}
				if err := clearGenerationPresentation(tx, r.conversation); err != nil {
					return nil, err
				}
				text := message.ErrorMessage
				if text == "" {
					text = "Context overflow"
				}
				next := JSON{"phase": "prepare", "attempt": attempt, "compacted": id, "overflow": text}
				return &TaskState{Status: "waiting", Checkpoint: next, On: []ID{id}, Policy: "allSettled"}, nil
			})
		}
	}
	now, err := r.Now()
	if err != nil {
		return err
	}
	until := retryDeadline(now, retryDelay(settings.Retry, attempt))
	text := message.ErrorMessage
	if text == "" {
		text = fmt.Sprintf("Model response ended with stop reason %s", message.StopReason)
	}
	return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		var boundary *canonicalBoundary
		var err error
		if !retry && r.harness.AfterFailedRun() == "continue" {
			boundary, err = prepareCanonicalBoundary(tx, r.conversation, settings)
			if err != nil {
				return nil, err
			}
		}
		doc, live, err := canonicalGenerationLive(tx, r)
		if err != nil {
			return nil, err
		}
		if _, err := canonicalAppendAssistant(tx, r.conversation, message); err != nil {
			return nil, err
		}
		if retry {
			live["generation"] = JSON{"attempt": attempt, "retry": JSON{"at": until, "error": message.ErrorMessage}}
			if err := doc.Set(live); err != nil {
				return nil, err
			}
			next := JSON{"phase": "retry", "attempt": attempt, "until": until}
			if id, present := cp["compacted"]; present {
				next["compacted"] = id
			}
			return &TaskState{Status: "sleeping", Checkpoint: next, Until: &until}, nil
		}
		if err := canonicalFailedRun(tx, r, live, boundary, "model_error", text); err != nil {
			return nil, err
		}
		if err := doc.Set(live); err != nil {
			return nil, err
		}
		return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: text, Detail: &TaskValue{Present: true, Value: JSON{"reason": "model_error"}}}}}, nil
	})
}
func canonicalGenerationStartTools(ctx context.Context, r *TaskRuntime, cp JSON, message MessageReceipt, calls []goai.ContentBlock, messages []MessageReceipt, agent agentState) error {
	if messages == nil {
		view, err := r.ContextView(ctx, r.conversation, submissionEntryID(cp["cutoff"]))
		if err != nil {
			return err
		}
		messages = view.Messages
	}
	offered := map[string]bool{}
	for _, tool := range ReplayToolDeclarations(messages) {
		offered[tool.Name] = true
	}
	// Source reads the live host mode when the response starts its round;
	// pending calls then retain that decision in the tools checkpoint.
	sequential := r.harness.resolvedSettings(RequestSettings{}).ToolExecution == "sequential"
	_, pins, _, _, err := r.phaseSelection().selectedGeneration(r.harness.selectionAgent(agent), r.harness.session.limits, r.reportError)
	if err != nil {
		return err
	}
	for _, call := range calls {
		if offered[call.Name] && pins[call.Name].offer.ExecutionMode == "sequential" {
			sequential = true
		}
	}
	return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		doc, live, err := canonicalGenerationLive(tx, r)
		if err != nil {
			return nil, err
		}
		assistant, err := canonicalAppendAssistant(tx, r.conversation, message)
		if err != nil {
			return nil, err
		}
		tools := []ID{}
		pending := []any{}
		slots := []any{}
		for _, call := range calls {
			slot := JSON{"callId": call.ID, "name": call.Name, "status": "pending"}
			input := JSON{"kind": "model", "assistant": assistant, "callId": call.ID}
			if !offered[call.Name] {
				slot["unoffered"] = true
				input["unoffered"] = true
			}
			if sequential && len(tools) > 0 {
				pending = append(pending, call.ID)
			} else {
				id, err := tx.CreateTask(ToolTask(), input, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.taskID}})
				if err != nil {
					return nil, err
				}
				tools = append(tools, id)
				slot["taskId"] = id
			}
			slots = append(slots, slot)
		}
		delete(live, "generation")
		live["tools"] = slots
		if run := generationObject(live["run"]); submissionEntryID(run["taskId"]) == r.taskID {
			run["turns"] = submissionEntryID(run["turns"]) + 1
			if run["first"] == nil {
				run["first"] = assistant
			}
		}
		if err := doc.Set(live); err != nil {
			return nil, err
		}
		ids := make([]any, len(tools))
		for i, id := range tools {
			ids[i] = id
		}
		return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "tools", "assistant": assistant, "tools": ids, "pending": pending}, On: tools, Policy: "allSettled"}, nil
	})
}
func canonicalGenerationTools(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
	if task.Restarts >= r.harness.RestartLimit() {
		return canonicalGenerationCutOff(ctx, task, r)
	}
	cp := taskCheckpointMap(task.State.Checkpoint)
	pending, _ := cp["pending"].([]any)
	if len(pending) > 0 {
		return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
			doc, live, err := canonicalGenerationLive(tx, r)
			if err != nil {
				return nil, err
			}
			next, _ := pending[0].(string)
			input := JSON{"kind": "model", "assistant": cp["assistant"], "callId": next}
			slots, _ := live["tools"].([]any)
			var selected JSON
			for _, raw := range slots {
				slot := generationObject(raw)
				if slot["callId"] == next && slot["taskId"] == nil {
					selected = slot
					if slot["unoffered"] == true {
						input["unoffered"] = true
					}
					break
				}
			}
			id, err := tx.CreateTask(ToolTask(), input, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.taskID}})
			if err != nil {
				return nil, err
			}
			if selected != nil {
				selected["taskId"] = id
			}
			if err := doc.Set(live); err != nil {
				return nil, err
			}
			tools, _ := cp["tools"].([]any)
			return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "tools", "assistant": cp["assistant"], "tools": append(tools, id), "pending": pending[1:]}, On: []ID{id}, Policy: "allSettled"}, nil
		})
	}
	return canonicalGenerationFinishTools(ctx, r, cp)
}
func canonicalGenerationAnswer(ctx context.Context, r *TaskRuntime, message MessageReceipt, agent agentState) error {
	hooks := r.phaseSelection().selectedHooks(r.harness.selectionAgent(agent))
	var continuation *Input
	after := false
	for _, hook := range hooks {
		after = after || hook.AfterRun != nil
		if continuation == nil && hook.OnYield != nil {
			value, err := callOnYield(ctx, hook.OnYield, message, r)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				r.reportExtensionError(err, hook.extension)
			} else {
				continuation = value
			}
		}
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
		answer, err := canonicalAppendAssistant(tx, r.conversation, message)
		if err != nil {
			return nil, err
		}
		outcome := &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: JSON{"entryId": answer}}}}
		takes := continuation != nil && !boundary.hasUsersOrReset()
		run := generationObject(live["run"])
		if !takes && after && submissionEntryID(run["taskId"]) == r.taskID {
			inputs, _ := run["inputs"].([]any)
			for _, id := range inputs {
				if err := tx.SettleSubmission(submissionEntryID(id), SubmissionSettlement{Status: "done", Answer: answer}); err != nil {
					return nil, err
				}
			}
			run["inputs"] = []any{}
			run["afterRun"] = JSON{}
			for _, key := range []string{"generation", "tools", "nestedTools"} {
				delete(live, key)
			}
			first := submissionEntryID(run["first"])
			if first == 0 {
				first = answer
			}
			if err := doc.Set(live); err != nil {
				return nil, err
			}
			return &TaskState{Status: "running", Checkpoint: JSON{"phase": "finish", "run": JSON{"reason": "answer", "first": first, "answer": answer, "last": answer}}}, nil
		}
		users, first, _, err := boundary.apply(tx, true, r.harness.session.now())
		if err != nil {
			return nil, err
		}
		if takes {
			receipt, err := inputReceipt(continuation.Content, continuation.Blocks, tx.limits)
			if err != nil {
				return nil, err
			}
			wire, err := dtoObject(receipt, tx.limits)
			if err != nil {
				return nil, err
			}
			wire["timestamp"] = r.harness.session.now()
			if _, err := tx.AppendEntry(r.conversation, JSON{"kind": userEntryKind, "model": []any{wire}}); err != nil {
				return nil, err
			}
			id, err := tx.CreateTask(GenerationTask(), JSON{}, TaskOptions{Conversation: r.conversation, Ownership: TaskOwnership{Kind: "conversation"}})
			if err != nil {
				return nil, err
			}
			if submissionEntryID(run["taskId"]) == r.taskID {
				run["taskId"] = id
				run["turns"] = 0
			}
			delete(live, "generation")
		} else {
			if err := canonicalEndRun(tx, live, r.taskID, answer, "", nil); err != nil {
				return nil, err
			}
			if len(users) > 0 {
				if _, err := canonicalStartRun(tx, r.conversation, live, users, first, false); err != nil {
					return nil, err
				}
			}
		}
		return outcome, doc.Set(live)
	})
}
func canonicalGenerationFinishTools(ctx context.Context, r *TaskRuntime, cp JSON) error {
	_, agent, err := canonicalGenerationAgent(ctx, r)
	if err != nil {
		return err
	}
	rawIDs, _ := cp["tools"].([]any)
	ids := make([]ID, len(rawIDs))
	for i, value := range rawIDs {
		ids[i] = submissionEntryID(value)
	}
	outcomes, err := r.Outcomes(ctx, ids)
	if err != nil {
		return err
	}
	controls := map[ID]*ToolControl{}
	for i, outcome := range outcomes {
		if outcome.Status == "completed" && outcome.Result != nil {
			value := generationObject(outcome.Result.Value)
			if control := generationObject(value["control"]); control != nil {
				var parsed ToolControl
				if err := fromObject(control, &parsed, r.harness.session.limits); err != nil {
					return err
				}
				controls[ids[i]] = &parsed
			}
		}
	}
	view, found, err := r.SnapshotDefinition(ctx, mountedDocumentDefinitions[1], r.conversation, nil)
	if err != nil {
		return err
	}
	var slots []any
	if found {
		slots, _ = generationObject(view.Copy())["tools"].([]any)
	}
	results := []ID{}
	receipts := []MessageReceipt{}
	terminate := len(slots) > 0
	added := []string{}
	var handoff *string
	for _, raw := range slots {
		slot := generationObject(raw)
		if id := submissionEntryID(slot["entry"]); id != 0 {
			results = append(results, id)
			entry, found, err := r.Entry(ctx, id)
			if err != nil {
				return err
			}
			if found {
				receipts = append(receipts, entry.Model...)
			}
		}
		control := controls[submissionEntryID(slot["taskId"])]
		if control == nil || !control.Terminate {
			terminate = false
		}
		if control != nil {
			added = append(added, control.AddTools...)
			if control.Handoff != nil {
				handoff = control.Handoff
			}
		}
	}
	for _, hook := range r.phaseSelection().selectedHooks(r.harness.selectionAgent(agent)) {
		if hook.AfterTools != nil {
			if err := callAfterTools(ctx, hook.AfterTools, receipts, r); err != nil {
				r.reportExtensionError(err, hook.extension)
			}
		}
		if hook.AfterToolEntries != nil {
			if err := callAfterToolEntries(ctx, hook.AfterToolEntries, submissionEntryID(cp["assistant"]), results, r); err != nil {
				r.reportExtensionError(err, hook.extension)
			}
		}
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
		if len(added) > 0 {
			if err := canonicalAddTools(tx, r.conversation, added); err != nil {
				return nil, err
			}
		}
		last := submissionEntryID(cp["assistant"])
		for _, id := range results {
			if id > last {
				last = id
			}
		}
		if handoff != nil {
			entry, err := tx.AppendEntry(r.conversation, JSON{"kind": "pi.reset", "head": "self", "model": []any{JSON{"role": "user", "content": *handoff, "timestamp": r.harness.session.now()}}})
			if err != nil {
				return nil, err
			}
			boundary.head = entry.ID
			last = entry.ID
		}
		if terminate {
			for _, hook := range r.phaseSelection().selectedHooks(r.harness.selectionAgent(agent)) {
				if hook.AfterRun == nil {
					continue
				}
				run := generationObject(live["run"])
				if submissionEntryID(run["taskId"]) != r.taskID {
					break
				}
				answer := submissionEntryID(cp["assistant"])
				inputs, _ := run["inputs"].([]any)
				for _, id := range inputs {
					if err := tx.SettleSubmission(submissionEntryID(id), SubmissionSettlement{Status: "done", Answer: answer}); err != nil {
						return nil, err
					}
				}
				run["inputs"], run["afterRun"] = []any{}, JSON{}
				for _, key := range []string{"generation", "tools", "nestedTools"} {
					delete(live, key)
				}
				first := submissionEntryID(run["first"])
				if first == 0 {
					first = answer
				}
				if err := doc.Set(live); err != nil {
					return nil, err
				}
				return &TaskState{Status: "running", Checkpoint: JSON{"phase": "finish", "run": JSON{"reason": "terminate", "first": first, "answer": answer, "last": last}}}, nil
			}
		}
		users, first, reset, err := boundary.apply(tx, terminate, r.harness.session.now())
		if err != nil {
			return nil, err
		}
		assistant := submissionEntryID(cp["assistant"])
		if terminate || reset {
			reason := ""
			answer := assistant
			if reset && !terminate {
				reason = "reset"
				answer = 0
			}
			if err := canonicalEndRun(tx, live, r.taskID, answer, reason, nil); err != nil {
				return nil, err
			}
			if len(users) > 0 {
				if _, err := canonicalStartRun(tx, r.conversation, live, users, first, reset); err != nil {
					return nil, err
				}
			}
		} else {
			delete(live, "tools")
			delete(live, "nestedTools")
			run := generationObject(live["run"])
			id, err := tx.CreateTask(GenerationTask(), JSON{}, TaskOptions{Conversation: r.conversation, Ownership: TaskOwnership{Kind: "conversation"}})
			if err != nil {
				return nil, err
			}
			if submissionEntryID(run["taskId"]) == r.taskID {
				run["taskId"] = id
				inputs, _ := run["inputs"].([]any)
				for _, id := range users {
					inputs = append(inputs, id)
				}
				run["inputs"] = inputs
			}
		}
		if err := doc.Set(live); err != nil {
			return nil, err
		}
		return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: JSON{"entryId": assistant}}}}, nil
	})
}
func canonicalAddTools(tx *Tx, conversation ID, names []string) error {
	doc, err := builtin(tx, conversation, "pi.agent")
	if err != nil {
		return err
	}
	value, err := doc.Get()
	if err != nil {
		return err
	}
	var state agentState
	if err := fromObject(value, &state, tx.limits); err != nil {
		return err
	}
	if state.ModelTools == nil {
		state.ModelTools = &ExtensionFilter{}
	}
	for _, name := range names {
		present := false
		for _, existing := range state.ModelTools.Add {
			present = present || existing == name
		}
		if !present {
			state.ModelTools.Add = append(state.ModelTools.Add, name)
		}
	}
	kept := []string{}
	for _, name := range state.ModelTools.Remove {
		keep := true
		for _, added := range names {
			if name == added {
				keep = false
			}
		}
		if keep {
			kept = append(kept, name)
		}
	}
	state.ModelTools.Remove = kept
	if state.Tools != nil {
		selected := append([]string{}, (*state.Tools)...)
		for _, name := range names {
			present := false
			for _, existing := range selected {
				present = present || name == existing
			}
			if !present {
				selected = append(selected, name)
			}
		}
		state.Tools = &selected
	} else if state.ToolsRemoved != nil {
		kept := []string{}
		for _, name := range *state.ToolsRemoved {
			keep := true
			for _, added := range names {
				if name == added {
					keep = false
				}
			}
			if keep {
				kept = append(kept, name)
			}
		}
		state.ToolsRemoved = &kept
	}
	encoded, err := dtoObject(state, tx.limits)
	if err != nil {
		return err
	}
	return doc.Set(encoded)
}
func canonicalGenerationFinish(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
	cp := taskCheckpointMap(task.State.Checkpoint)
	var summary RunSummary
	if err := fromObject(generationObject(cp["run"]), &summary, r.harness.session.limits); err != nil {
		return err
	}
	if task.Restarts >= r.harness.RestartLimit() {
		return canonicalGenerationCutOff(ctx, task, r)
	}
	hooks, err := afterRunHooks(r)
	if err != nil {
		return err
	}
	prepared := []preparedRunEndTask{}
	for _, hook := range hooks {
		if hook.AfterRun == nil {
			continue
		}
		asked, err := callAfterRun(ctx, hook.AfterRun, summary, r)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			r.reportExtensionError(err, hook.extension)
			continue
		}
		if asked != nil {
			if err := prepareRunEndTasks(r, asked.Tasks, &prepared); err != nil {
				r.reportExtensionError(err, hook.extension)
			}
		}
	}
	if len(prepared) == 0 {
		return canonicalGenerationEndAnswered(ctx, r, summary.Answer, false)
	}
	return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		ids := []ID{}
		values := []any{}
		for _, prepared := range prepared {
			id, err := tx.createTask(prepared.definition, prepared.execution.Input.Value, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.taskID}}, prepared.execution)
			if err != nil {
				return nil, err
			}
			ids = append(ids, id)
			values = append(values, id)
		}
		doc, live, err := canonicalGenerationLive(tx, r)
		if err != nil {
			return nil, err
		}
		run := generationObject(live["run"])
		if submissionEntryID(run["taskId"]) == r.taskID {
			if after := generationObject(run["afterRun"]); after != nil {
				after["tasks"] = values
			}
		}
		if err := doc.Set(live); err != nil {
			return nil, err
		}
		return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "drain", "answer": summary.Answer, "tasks": values}, On: ids, Policy: "allSettled"}, nil
	})
}
func canonicalGenerationDrain(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
	if task.Restarts >= r.harness.RestartLimit() {
		return canonicalGenerationCutOff(ctx, task, r)
	}
	return canonicalGenerationEndAnswered(ctx, r, submissionEntryID(taskCheckpointMap(task.State.Checkpoint)["answer"]), false)
}
func canonicalGenerationEndAnswered(ctx context.Context, r *TaskRuntime, answer ID, cutoff bool, crashMessage ...string) error {
	return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		var boundary *canonicalBoundary
		var err error
		if !cutoff {
			boundary, err = prepareCanonicalBoundary(tx, r.conversation, r.harness.resolvedSettings(RequestSettings{}))
			if err != nil {
				return nil, err
			}
		}
		doc, live, err := canonicalGenerationLive(tx, r)
		if err != nil {
			return nil, err
		}
		var users []ID
		var first ID
		if boundary != nil {
			users, first, _, err = boundary.apply(tx, true, r.harness.session.now())
			if err != nil {
				return nil, err
			}
		}
		if err := canonicalEndRun(tx, live, r.taskID, answer, "", nil); err != nil {
			return nil, err
		}
		if len(users) > 0 {
			if _, err := canonicalStartRun(tx, r.conversation, live, users, first, false); err != nil {
				return nil, err
			}
		}
		if err := doc.Set(live); err != nil {
			return nil, err
		}
		completed := &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: JSON{"entryId": answer}}}}
		if len(crashMessage) > 0 {
			completed.endedByCrashes = crashMessage[0]
		}
		return completed, nil
	})
}

// Source deferredOf/cancelDeferred: shared by abort and at-limit salvage.
func canonicalGenerationCancelDeferred(ctx context.Context, r *TaskRuntime, cp JSON) {
	poll := cp
	if cp["phase"] == "retry" {
		poll = generationObject(cp["poll"])
	}
	if handle := generationObject(poll["handle"]); handle != nil {
		ref, err := canonicalModelRef(poll)
		if err == nil {
			model, resolveErr := callModelResolver(r.harness.options.Models, ref)
			if resolveErr == nil && model != nil {
				var deferred goai.DeferredHandle
				if fromObject(handle, &deferred, r.harness.session.limits) == nil {
					// The Models boundary applies host identity to cancellation too.
					// Like source cancelDeferred, report a throwing provider without
					// replacing the durable aborted outcome with a scheduler fault.
					options := &goai.StreamOptions{Telemetry: TelemetryFromContext(ctx), Identity: goai.MergeClientIdentity(r.harness.options.Identity, nil)}
					if err := callDeferredCancel(ctx, model, deferred, options); err != nil {
						r.reportHostError(err, "models")
					}
				}
			}
		}
	}
}
func canonicalGenerationAbort(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
	cp := taskCheckpointMap(task.State.Checkpoint)
	canonicalGenerationCancelDeferred(ctx, r, cp)
	var calls []goai.ContentBlock
	if cp["phase"] == "tools" {
		entry, found, err := r.Entry(ctx, submissionEntryID(cp["assistant"]))
		if err != nil {
			return err
		}
		if found && len(entry.Model) > 0 {
			pending, _ := cp["pending"].([]any)
			for _, id := range pending {
				for _, block := range entry.Model[0].Content {
					if block.Type == "toolCall" && block.ID == id {
						calls = append(calls, block)
						break
					}
				}
			}
		}
	}
	return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		doc, live, err := canonicalGenerationLive(tx, r)
		if err != nil {
			return nil, err
		}
		if err := canonicalConvertPartial(tx, r.conversation, live); err != nil {
			return nil, err
		}
		for _, call := range calls {
			result := canonicalToolError("aborted", fmt.Sprintf("Tool %s was aborted", call.Name))
			slots, _ := live["tools"].([]any)
			for _, raw := range slots {
				slot := generationObject(raw)
				if slot["callId"] == call.ID && slot["unoffered"] == true {
					result = canonicalToolError("tool_unavailable", fmt.Sprintf("Tool %s is not available", call.Name))
				}
			}
			receipt := MessageReceipt{Role: goai.RoleToolResult, ToolCallID: call.ID, ToolName: call.Name, Content: []goai.ContentBlock{{Type: "text", Text: renderToolDiagnostics(result.Diagnostics)}}, IsError: true, Diagnostics: result.Diagnostics, Timestamp: r.harness.session.now()}
			wire, err := dtoObject(receipt, tx.limits)
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
		}
		if err := canonicalEndRun(tx, live, r.taskID, 0, "aborted", nil); err != nil {
			return nil, err
		}
		if err := doc.Set(live); err != nil {
			return nil, err
		}
		return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
	})
}

func canonicalCutOffCount(restarts uint64) string {
	if restarts == 1 {
		return "once"
	}
	return fmt.Sprintf("%d times in a row", restarts)
}
