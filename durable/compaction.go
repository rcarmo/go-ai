package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
)

type CompactionPolicy struct {
	Enabled          bool `json:"enabled,omitempty"`
	TriggerTokens    int  `json:"triggerTokens,omitempty"`
	ReserveTokens    int  `json:"reserveTokens,omitempty"`
	BackgroundTokens int  `json:"backgroundTokens,omitempty"`
	KeepRecentTokens int  `json:"keepRecentTokens,omitempty"`
	MaxTokens        int  `json:"maxTokens,omitempty"`
}

type CompactionOptions struct {
	KeepRecentTokens int
	Instructions     string
	MaxTokens        int
	Retry            RetryPolicy
}

const compactionSummaryPrefix = "The conversation history before this point was compacted into the following summary:\n\n<summary>\n"

// Compact creates a durable manual summarisation task. Selection and request
// intent survive reopen; summary placement is a passive write, queued while a
// generation runs. Generation-owned blocking compactions append directly.
func (c *ConversationHandle) Compact(ctx context.Context, options CompactionOptions) (ID, error) {
	if options.KeepRecentTokens < 0 || options.MaxTokens < 0 {
		return 0, reject("invalid compaction options")
	}
	if err := validateRetryPolicy(options.Retry); err != nil {
		return 0, err
	}
	h := c.h
	if h.closing.Load() {
		return 0, ErrClosed
	}
	snapshot, err := h.options.Registry.taskSnapshot(h.session.limits)
	if err != nil {
		return 0, err
	}
	definition := snapshot.Task("task.pi.compaction")
	if definition == nil {
		return 0, reject("compaction adapter unavailable")
	}
	var id ID
	_, err = h.CommitTasks(ctx, c.id, func(tx *Tx) error {
		var err error
		id, err = tx.CreateTask(definition, JSON{"reason": "manual", "keepRecentTokens": options.KeepRecentTokens, "instructions": options.Instructions, "maxTokens": options.MaxTokens, "retry": JSON{"enabled": options.Retry.Enabled, "maxRetries": options.Retry.MaxRetries, "baseDelayMs": options.Retry.BaseDelayMs, "maxDelayMs": options.Retry.MaxDelayMs}}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		return err
	})
	if err == nil {
		h.scheduler.enable()
	}
	return id, err
}
func (h *Harness) compactionDefinition() (*TaskDefinition, error) {
	return DefineTask(TaskDefinitionOptions{Kind: "task.pi.compaction", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "select"}, nil }, Phases: map[string]TaskPhase{
		"select": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			h := r.harness
			view, err := h.session.ContextView(ctx, r.ConversationID(), 0)
			if err != nil {
				return err
			}
			input := task.Input.Value.(map[string]any)
			keep := 0
			if value, ok := input["keepRecentTokens"]; ok {
				n, _ := exactNumber(value)
				if n != nil {
					keep = int(n.Num().Int64())
				}
			}
			cut := compactionCut(view, keep)
			if cut <= 0 {
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: JSON{"compacted": false}}}}, nil
				})
			}
			snapshot, err := h.session.Snapshot(ctx)
			if err != nil {
				return err
			}
			doc, ok := agentDocument(snapshot, r.ConversationID())
			if !ok {
				return reject("compaction agent unavailable")
			}
			var agent agentState
			if err := fromObject(doc.Value, &agent, h.session.limits); err != nil {
				return err
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
			text, err := SerializeConversation(source, h.session.limits)
			if err != nil {
				return err
			}
			instructions, _ := input["instructions"].(string)
			reason, _ := input["reason"].(string)
			if reason == "" {
				reason = "threshold"
			}
			for _, hook := range h.options.Registry.selectedCompactionHooks(agent) {
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
					h.scheduler.report(err)
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
						h.scheduler.report(reject("invalid compaction hook summary"))
						continue
					}
					return r.Commit(ctx, func(tx *Tx, current TaskRecord) (*TaskState, error) {
						return placeCompactionSummary(tx, r, current, view.Entries[cut].ID, summary)
					})
				}
			}
			maxTokens := 2048
			if value, ok := input["maxTokens"]; ok {
				n, _ := exactNumber(value)
				if n != nil && n.Num().Int64() > 0 {
					maxTokens = int(n.Num().Int64())
				}
			}
			local, err := callModelResolver(h.options.Models, agent.Model)
			if err != nil {
				return err
			}
			model, err := cloneModel(local, h.session.limits)
			if err != nil {
				return err
			}
			if model.ID != agent.Model.ID || model.Provider != agent.Model.Provider {
				return reject("compaction model identity mismatch")
			}
			pinned, err := dtoObject(model, h.session.limits)
			if err != nil {
				return err
			}
			settings, err := dtoObject(agent.Settings, h.session.limits)
			if err != nil {
				return err
			}
			checkpoint := JSON{"pinnedModel": pinned, "settings": settings, "reason": reason, "phase": "summarize", "tail": tail, "firstKept": view.Entries[cut].ID, "source": text, "instructions": instructions, "model": JSON{"provider": string(agent.Model.Provider), "id": agent.Model.ID}, "maxTokens": maxTokens, "attempt": 1}
			if retry, ok := input["retry"]; ok {
				checkpoint["retry"] = retry
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "running", Checkpoint: checkpoint}, nil
			})
		},
		"summarize": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			h := r.harness
			cp := task.State.Checkpoint
			modelObject := JSON(cp["model"].(map[string]any))
			var ref ModelRef
			if err := fromObject(modelObject, &ref, h.session.limits); err != nil {
				return err
			}
			local, err := callModelResolver(h.options.Models, ref)
			if err != nil || local == nil || local.ID != ref.ID || local.Provider != ref.Provider {
				return reject("compaction model unavailable")
			}
			model, err := cloneModel(local, h.session.limits)
			if err != nil {
				return err
			}
			if raw, ok := cp["pinnedModel"].(map[string]any); ok {
				var pinned goai.Model
				if err := fromObject(JSON(raw), &pinned, h.session.limits); err != nil {
					return err
				}
				if pinned.ID != ref.ID || pinned.Provider != ref.Provider {
					return reject("compaction pinned model identity")
				}
				model = &pinned
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
			options.MaxTokens = &maxTokens
			if raw, ok := cp["settings"].(map[string]any); ok {
				var settings RequestSettings
				if err := fromObject(JSON(raw), &settings, h.session.limits); err != nil {
					return err
				}
				options.Temperature = settings.Temperature
			}
			options.Deferred = nil
			source, _ := cp["source"].(string)
			instructions, _ := cp["instructions"].(string)
			conv := &goai.Context{SystemPrompt: compactionSystemPrompt, Messages: []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: compactionRequestText(source, instructions)}}}}}
			message, success, _ := h.drain(goai.Stream(ctx, model, conv, options), model)
			if err := ctx.Err(); err != nil {
				return err
			}
			parts := []string{}
			for _, block := range message.Content {
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
				if !success || message.StopReason != goai.StopReasonStop || strings.TrimSpace(summary) == "" {
					var policy RetryPolicy
					if raw, ok := cp["retry"].(map[string]any); ok {
						if err := fromObject(JSON(raw), &policy, tx.limits); err != nil {
							return nil, err
						}
					}
					attempt := 1
					if n, ok := cp["attempt"]; ok {
						value, _ := exactNumber(n)
						if value != nil {
							attempt = int(value.Num().Int64())
						}
					}
					if policy.Enabled && attempt <= policy.MaxRetries && retryableReceipt(message) {
						now, err := r.Now()
						if err != nil {
							return nil, err
						}
						next, err := copyObject(cp, tx.limits)
						if err != nil {
							return nil, err
						}
						next["phase"], next["until"], next["attempt"] = "retry", retryDeadline(now, retryDelay(policy, attempt)), attempt+1
						return &TaskState{Status: "running", Checkpoint: next}, nil
					}
					return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "compaction model failed"}}}, nil
				}
				cut, _ := exactNumber(cp["firstKept"])
				firstKept := ID(cut.Num().Uint64())
				return placeCompactionSummary(tx, r, task, firstKept, summary)
			})
		},
		"retry": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			until, err := exactNumber(task.State.Checkpoint["until"])
			if !err {
				return reject("invalid compaction retry deadline")
			}
			if err := r.SleepUntil(ctx, until.Num().Int64()); err != nil {
				return err
			}
			checkpoint, copyErr := copyObject(task.State.Checkpoint, r.harness.session.limits)
			if copyErr != nil {
				return copyErr
			}
			checkpoint["phase"] = "summarize"
			delete(checkpoint, "until")
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "running", Checkpoint: checkpoint}, nil
			})
		},
	}, Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
			return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
		})
	}})
}
func compactionCut(view ContextView, keep int) int {
	cut := len(view.Entries)
	tokens := 0
	for i := len(view.Entries) - 1; i >= 0; i-- {
		for _, message := range view.Contributions[i] {
			tokens += goai.EstimateMessageTokens(receiptMessage(message))
		}
		if tokens > keep {
			break
		}
		cut = i
	}
	for cut < len(view.Entries) && !compactionCuttable(view, cut) {
		cut++
	}
	if cut == 0 || cut == len(view.Entries) {
		return 0
	}
	for _, messages := range view.Contributions[:cut] {
		if len(messages) > 0 {
			return cut
		}
	}
	return 0
}
func compactionCuttable(view ContextView, index int) bool {
	messages := view.Contributions[index]
	if len(messages) == 0 || messages[0].Role == goai.RoleToolResult {
		return false
	}
	if messages[0].Role != goai.RoleUser {
		return true
	}
	for previous := index - 1; previous >= 0; previous-- {
		assistant := false
		calls := map[string]bool{}
		for _, message := range view.Contributions[previous] {
			if message.Role != goai.RoleAssistant {
				continue
			}
			assistant = true
			for _, block := range message.Content {
				if block.Type == "toolCall" {
					calls[block.ID] = true
				}
			}
		}
		if !assistant {
			continue
		}
		if len(calls) == 0 {
			return true
		}
		for _, later := range view.Contributions[index:] {
			for _, message := range later {
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
