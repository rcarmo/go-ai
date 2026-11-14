package durable

import (
	"context"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"unicode/utf8"
)

type CompactionPolicy struct {
	Enabled          bool `json:"enabled"`
	ReserveTokens    int  `json:"reserveTokens"`
	BackgroundTokens int  `json:"backgroundTokens"`
	KeepRecentTokens int  `json:"keepRecentTokens"`
}

const compactionSummaryPrefix = "The conversation history before this point was compacted into the following summary:\n\n<summary>\n"

// Compact creates a durable manual summarisation task. Selection and request
// intent survive reopen; summary placement is a passive write, queued while a
// generation runs. Generation-owned blocking compactions append directly.
// Nil instructions omit the field; a non-nil empty string remains explicit.
// Selection, token budget and retry policy come from the agent settings.
func (c *ConversationHandle) Compact(ctx context.Context, instructions *string) (ID, error) {
	h := c.h
	if h.closing.Load() {
		return 0, ErrClosed
	}
	snapshot, err := h.options.Registry.taskSnapshot(h.session.limits)
	if err != nil {
		return 0, err
	}
	definition := snapshot.Task("pi.compaction")
	if definition == nil {
		return 0, reject("compaction adapter unavailable")
	}
	var id ID
	_, err = h.CommitTasks(ctx, c.id, func(tx *Tx) error {
		var err error
		input := JSON{"reason": "manual"}
		if instructions != nil {
			input["instructions"] = *instructions
		}
		id, err = tx.CreateTask(definition, input, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		return err
	})
	if err == nil {
		h.scheduler.enable()
	}
	return id, err
}
func (h *Harness) compactionDefinition() (*TaskDefinition, error) {
	definition, err := DefineTask(TaskDefinitionOptions{Kind: "pi.compaction", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "select"}, nil }, Phases: map[string]TaskPhase{
		"select": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			h := r.harness
			view, err := h.session.ContextView(ctx, r.ConversationID(), 0)
			if err != nil {
				return err
			}
			snapshot, err := h.session.Snapshot(ctx)
			if err != nil {
				return err
			}
			var agent agentState
			if doc, ok := agentDocument(snapshot, r.ConversationID()); ok {
				if err := fromObject(doc.Value, &agent, h.session.limits); err != nil {
					return err
				}
			}
			agent.Settings = h.resolvedSettings(agent.Settings)
			local, err := callModelResolver(h.options.Models, agent.Model)
			if err != nil {
				return err
			}
			if local == nil {
				return failCompactionNoModel(ctx, r, agent.Model)
			}
			model, err := cloneModel(local, h.session.limits)
			if err != nil {
				return err
			}
			if model.ID != agent.Model.ID || model.Provider != agent.Model.Provider {
				return reject("compaction model identity mismatch")
			}
			input := task.Input.Value.(map[string]any)
			policy := compactionPolicy(agent.Settings, agent.Model)
			keep := policy.KeepRecentTokens
			cut := compactionCut(view, keep)
			if cut <= 0 {
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: JSON{"compacted": false}}}}, nil
				})
			}
			tail := ID(0)
			for _, entry := range view.Entries {
				if entry.ID > tail {
					tail = entry.ID
				}
			}
			source := []MessageReceipt{}
			for i := 0; i < cut; i++ {
				source = append(source, view.Contributions[i]...)
			}
			source = orderContextToolResults(source)
			var instructions *string
			if text, present := input["instructions"].(string); present {
				instructions = &text
			}
			reason, _ := input["reason"].(string)
			if reason == "" {
				reason = "threshold"
			}
			for _, hook := range r.phaseSelection().selectedCompactionHooks(h.selectionAgent(agent)) {
				if hook.BeforeCompact == nil {
					continue
				}
				messages, err := detachReceipts(source, h.session.limits)
				if err != nil {
					return err
				}
				entries := make([]Entry, cut)
				for i := range entries {
					entries[i], err = copyEntry(view.Entries[i], h.session.limits)
					if err != nil {
						return err
					}
				}
				decision, err := callBeforeCompact(ctx, hook.BeforeCompact, CompactionInput{Reason: reason, Entries: entries, Messages: messages, FirstKept: view.Entries[cut].ID, Instructions: instructions}, r)
				if err != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					r.reportExtensionError(err, hook.extension)
					continue
				}
				if decision == nil {
					continue
				}
				if decision.Decline {
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: JSON{"compacted": false}}}}, nil
					})
				}
				if decision.Summary != nil {
					summary := *decision.Summary
					if strings.TrimSpace(summary) == "" || len(summary) > h.session.limits.MaxStringBytes {
						r.reportExtensionError(reject("invalid compaction hook summary"), hook.extension)
						continue
					}
					return r.Commit(ctx, func(tx *Tx, current TaskRecord) (*TaskState, error) {
						return placeCompactionSummary(tx, r, current, view.Entries[cut].ID, summary)
					})
				}
			}
			maxTokens := policy.ReserveTokens/5*4 + policy.ReserveTokens%5*4/5
			if model.MaxTokens > 0 && maxTokens > model.MaxTokens {
				maxTokens = model.MaxTokens
			}
			thinking := string(agent.ThinkingLevel)
			if thinking == "" {
				thinking = string(goai.ThinkingOff)
			}
			checkpoint := JSON{"phase": "summarize", "attempt": 1, "model": JSON{"provider": string(agent.Model.Provider), "modelId": agent.Model.ID}, "thinkingLevel": thinking, "streamOptions": generationStreamOptions(agent.Settings), "maxTokens": maxTokens, "tail": tail, "firstKept": view.Entries[cut].ID}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "running", Checkpoint: checkpoint}, nil
			})
		},
		"summarize": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			h := r.harness
			cp := taskCheckpointMap(task.State.Checkpoint)
			modelObject := JSON(cp["model"].(map[string]any))
			var sourceRef struct {
				Provider goai.Provider `json:"provider"`
				ModelID  string        `json:"modelId"`
			}
			if err := fromObject(modelObject, &sourceRef, h.session.limits); err != nil {
				return err
			}
			ref := ModelRef{Provider: sourceRef.Provider, ID: sourceRef.ModelID}
			local, err := callModelResolver(h.options.Models, ref)
			if err != nil {
				return err
			}
			if local == nil {
				return failCompactionNoModel(ctx, r, ref)
			}
			if local.ID != ref.ID || local.Provider != ref.Provider {
				return reject("compaction model identity mismatch")
			}
			model, err := cloneModel(local, h.session.limits)
			if err != nil {
				return err
			}
			model.BaseURL, model.APIKey = local.BaseURL, local.APIKey
			model.Headers = make(map[string]string, len(local.Headers))
			for key, value := range local.Headers {
				model.Headers[key] = value
			}
			var options *goai.StreamOptions
			if h.options.RequestOptions != nil {
				options, err = callRequestOptions(h.options.RequestOptions, ctx, ref)
				if err != nil {
					return err
				}
			}
			options, err = cloneOptions(options, h.session.limits)
			if err != nil {
				return err
			}
			maximum, _ := exactNumber(cp["maxTokens"])
			maxTokens := int(maximum.Num().Int64())
			stream, ok := cp["streamOptions"].(map[string]any)
			if !ok {
				return reject("missing compaction stream options")
			}
			forwarded := JSON{}
			for key, value := range stream {
				if key != "deferred" {
					forwarded[key] = value
				}
			}
			var settings RequestSettings
			if err := fromObject(forwarded, &settings, h.session.limits); err != nil {
				return err
			}
			if err := applyRequestSettings(options, settings, h.session.limits); err != nil {
				return err
			}
			options.Identity = goai.MergeClientIdentity(h.options.Identity, options.Identity)
			if options.Telemetry == nil {
				options.Telemetry = TelemetryFromContext(ctx)
			}
			options.SessionID, err = r.providerSessionID(ctx)
			if err != nil {
				return err
			}
			options.MaxTokens = &maxTokens
			options.Deferred = nil
			options.CacheRetention = goai.CacheRetentionNone
			options.Reasoning = nil
			if thinking, _ := cp["thinkingLevel"].(string); thinking != "" && thinking != "off" {
				level := goai.ThinkingLevel(thinking)
				options.Reasoning = &level
			}
			snapshot, err := h.session.Snapshot(ctx)
			if err != nil {
				return err
			}
			tail, tailOK := exactNumber(cp["tail"])
			firstKept, firstOK := exactNumber(cp["firstKept"])
			if !tailOK || !firstOK || !tail.IsInt() || !firstKept.IsInt() {
				return reject("invalid compaction range")
			}
			source, err := compactionTranscript(snapshot, r.ConversationID(), ID(tail.Num().Uint64()), ID(firstKept.Num().Uint64()), h.session.limits)
			if err != nil {
				return err
			}
			nativeSource := !utf8.ValidString(source)
			var instructions *string
			if text, present := task.Input.Value.(map[string]any)["instructions"].(string); present {
				instructions = &text
			}
			conv := &goai.Context{SystemPrompt: compactionSystemPrompt, Messages: []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{compactionRequestBlock(source, instructions, nativeSource)}}}}
			message, success, _ := h.drain(goai.Stream(ctx, model, conv, options), model)
			if err := ctx.Err(); err != nil {
				return err
			}
			parts := []string{}
			hasToolCalls := false
			for _, block := range message.Content {
				if block.Type == "toolCall" {
					hasToolCalls = true
				}
				if block.Type == "text" {
					parts = append(parts, block.Text)
				}
			}
			summary := strings.TrimSpace(strings.Join(parts, "\n"))
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				usage, err := builtin(tx, r.ConversationID(), "pi.usage")
				if err != nil {
					return nil, err
				}
				if err = usage.Update(func(value JSON) error { return addModelUsage(value, message, tx.limits) }); err != nil {
					return nil, err
				}
				if !success || message.StopReason != goai.StopReasonStop || hasToolCalls || strings.TrimSpace(summary) == "" {
					policy := h.resolvedSettings(RequestSettings{}).Retry
					if document, ok := agentDocument(tx.state, r.ConversationID()); ok {
						var agent agentState
						if err := fromObject(document.Value, &agent, tx.limits); err != nil {
							return nil, err
						}
						policy = h.resolvedSettings(agent.Settings).Retry
					}
					attempt := 1
					if n, ok := cp["attempt"]; ok {
						value, _ := exactNumber(n)
						if value != nil {
							attempt = int(value.Num().Int64())
						}
					}
					if message.StopReason == goai.StopReasonError && policy.Enabled && attempt <= policy.MaxRetries && retryableReceipt(message) {
						now, err := r.Now()
						if err != nil {
							return nil, err
						}
						next, err := copyObject(cp, tx.limits)
						if err != nil {
							return nil, err
						}
						deadline := retryDeadline(now, retryDelay(policy, attempt))
						next["phase"], next["until"] = "retry", deadline
						if err := setCompactionRetryStatus(tx, r.ConversationID(), r.TaskID(), deadline, message.ErrorMessage); err != nil {
							return nil, err
						}
						return pacedTaskState(next, deadline), nil
					}
					return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: summaryFailure(message), Detail: &TaskValue{Present: true, Value: JSON{"reason": "model_error"}}}}}, nil
				}
				cut, _ := exactNumber(cp["firstKept"])
				firstKept := ID(cut.Num().Uint64())
				return placeCompactionSummary(tx, r, task, firstKept, summary)
			})
		},
		"retry": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			until, err := exactNumber(taskCheckpointMap(task.State.Checkpoint)["until"])
			if !err || !until.IsInt() || !until.Num().IsInt64() {
				return reject("invalid compaction retry deadline")
			}
			deadline := until.Num().Int64()
			now, clockErr := r.Now()
			if clockErr != nil {
				return clockErr
			}
			if now < deadline {
				if deadline >= 0 && uint64(deadline) <= MaxID {
					// Recovered pre-sleep checkpoints, or a clock rollback after
					// reservation, must yield rather than occupy a host permit.
					return r.SleepUntil(ctx, deadline, taskCheckpointMap(task.State.Checkpoint))
				}
				if err := r.waitUntil(ctx, deadline); err != nil {
					return err
				}
			}
			checkpoint, copyErr := copyObject(taskCheckpointMap(task.State.Checkpoint), r.harness.session.limits)
			if copyErr != nil {
				return copyErr
			}
			checkpoint["phase"] = "summarize"
			delete(checkpoint, "until")
			attempt, ok := exactNumber(checkpoint["attempt"])
			if !ok || !attempt.IsInt() || !attempt.Num().IsUint64() {
				return reject("invalid compaction attempt")
			}
			checkpoint["attempt"] = attempt.Num().Uint64() + 1
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "running", Checkpoint: checkpoint}, nil
			})
		},
	}, Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
			return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
		})
	}})
	if err != nil {
		return nil, err
	}
	for phase, handler := range definition.options.Phases {
		original := handler
		definition.options.Phases[phase] = func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			if task.Restarts > 0 && task.Restarts >= r.harness.RestartLimit() {
				message := fmt.Sprintf("Compaction was interrupted %s", cutoffCount(task.Restarts))
				err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: message, Detail: &TaskValue{Present: true, Value: JSON{"reason": "restart_limit"}}}}}, nil
				})
				if err == nil {
					r.reportCutoff(task.Restarts, message)
				}
				return err
			}
			return original(ctx, task, r)
		}
	}
	return definition, nil
}
func compactionCut(view ContextView, keep int) int {
	start := 0
	if view.Head != nil {
		start = 1
	}
	candidates := []int{}
	for index := start; index < len(view.Contributions); index++ {
		if compactionCuttable(view, index) {
			candidates = append(candidates, index)
		}
	}
	kept, cut := 0, -1
	for index := len(view.Contributions) - 1; index >= start; index-- {
		for _, message := range view.Contributions[index] {
			kept += goai.EstimateMessageTokens(receiptMessage(message))
		}
		if kept < keep {
			continue
		}
		for _, candidate := range candidates {
			if candidate >= index {
				cut = candidate
				break
			}
		}
		if cut < 0 && len(candidates) > 0 {
			cut = candidates[len(candidates)-1]
		}
		break
	}
	if cut < 0 {
		return 0
	}
	for index := start; index < cut; index++ {
		if len(view.Contributions[index]) > 0 {
			return cut
		}
	}
	return 0
}
func compactionCuttable(view ContextView, index int) bool {
	messages := view.Contributions[index]
	if len(messages) == 0 {
		return false
	}
	if messages[0].Role == goai.RoleAssistant {
		return true
	}
	if messages[0].Role != goai.RoleUser {
		return false
	}
	for previous := index - 1; previous >= 0; previous-- {
		assistant := false
		calls := map[string]bool{}
		for index := len(view.Contributions[previous]) - 1; index >= 0; index-- {
			message := view.Contributions[previous][index]
			if message.Role != goai.RoleAssistant {
				continue
			}
			assistant = true
			for _, block := range message.Content {
				if block.Type == "toolCall" {
					calls[block.ID] = true
				}
			}
			break
		}
		if !assistant {
			continue
		}
		if len(calls) == 0 {
			return true
		}
		for offset, later := range view.Contributions[index:] {
			for position, message := range later {
				if message.Role == goai.RoleAssistant && (offset > 0 || position > 0) {
					return true
				}
				if message.Role == goai.RoleToolResult && calls[message.ToolCallID] {
					return false
				}
			}
		}
		return true
	}
	return true
}

func placeCompactionSummary(tx *Tx, r *TaskRuntime, task TaskRecord, firstKept ID, summary string) (*TaskState, error) {
	reason := "threshold"
	if input, ok := task.Input.Value.(map[string]any); ok {
		if value, ok := input["reason"].(string); ok {
			reason = value
		}
	}
	message := userReceipt(compactionSummaryPrefix + summary + "\n</summary>")
	now, err := r.Now()
	if err != nil {
		return nil, err
	}
	message.Timestamp = now
	draft := Entry{Kind: "pi.compaction", Head: firstKept, Value: JSON{"reason": reason}, Model: []MessageReceipt{message}}
	result := JSON{"compacted": true, "firstKept": firstKept}
	if task.Owner == 0 {
		id, err := admitCompactionWrite(tx, r.ConversationID(), r.TaskID(), draft)
		if err != nil {
			return nil, err
		}
		result["submissionId"] = id
	} else {
		id, err := tx.MintID()
		if err != nil {
			return nil, err
		}
		draft.ID, draft.Conversation = id, r.ConversationID()
		if err = tx.AppendEntry(draft); err != nil {
			return nil, err
		}
		result["entryId"] = id
	}
	return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: result}}}, nil
}
