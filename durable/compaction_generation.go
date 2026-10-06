package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
)

func (h *Harness) startAutomaticCompaction(runtime *TaskRuntime, task Task, cp *generationCheckpoint) (bool, error) {
	return h.startGenerationCompaction(runtime, task, cp, false, nil)
}
func (h *Harness) startGenerationCompaction(runtime *TaskRuntime, task Task, cp *generationCheckpoint, overflow bool, receipt *MessageReceipt) (bool, error) {
	if cp.Compaction != 0 {
		return false, nil
	}
	state, err := h.session.Snapshot(runtime.context)
	if err != nil {
		return false, err
	}
	doc, ok := agentDocument(state, task.Conversation)
	if !ok {
		return false, nil
	}
	var agent agentState
	if err := fromObject(doc.Value, &agent, h.session.limits); err != nil {
		return false, err
	}
	agent.Settings = h.resolvedSettings(agent.Settings)
	policy := agent.Settings.Compaction
	if !policy.Enabled {
		return false, nil
	}
	view, err := deriveContextView(state, task.Conversation, 0, h.session.limits)
	if err != nil {
		return false, err
	}
	// No removable range means no compaction work; do not resolve the model
	// twice or surface a preparation error before ordinary request preparation.
	if compactionCut(view, policy.KeepRecentTokens) <= 0 {
		return false, nil
	}
	tokens := estimateContextTokens(view)
	if cp.Phase == "queued" && cp.InputEntry == 0 {
		input, err := inputReceipt(cp.Input, cp.InputBlocks, h.session.limits)
		if err != nil {
			return false, err
		}
		tokens += goai.EstimateMessageTokens(receiptMessage(input))
	}
	trigger := policy.TriggerTokens
	if trigger == 0 {
		model, err := callModelResolver(h.options.Models, agent.Model)
		if err != nil {
			return false, err
		}
		if model == nil {
			return false, nil
		}
		if model.ContextWindow <= 0 && !overflow {
			return false, nil
		}
		trigger = model.ContextWindow - policy.ReserveTokens
	}
	background := !overflow && policy.BackgroundTokens > 0 && tokens <= trigger && tokens > trigger-policy.BackgroundTokens
	if !overflow && tokens <= trigger && !background || compactionCut(view, policy.KeepRecentTokens) <= 0 {
		return false, nil
	}
	if background {
		for _, work := range state.Tasks {
			if work.Conversation == task.Conversation && work.Kind == "task.pi.compaction" && !terminalStatus(work.Status) {
				return false, nil
			}
		}
	}
	snapshot, err := h.options.Registry.taskSnapshot(h.session.limits)
	if err != nil {
		return false, err
	}
	definition := snapshot.Task("task.pi.compaction")
	if definition == nil {
		return false, reject("compaction adapter unavailable")
	}
	_, err = h.session.invocationCommit(runtime.context, task.ID, func(tx *Tx) error {
		current := tx.state.Tasks[task.ID]
		if err := runtime.check(); err != nil {
			return err
		}
		if h.closing.Load() || taskAborted(current) {
			return ErrSealed
		}
		tx.taskConversation = task.Conversation
		reason := "threshold"
		if overflow {
			reason = "overflow"
		}
		ownership := TaskOwnership{Kind: "task", Task: task.ID}
		if background {
			ownership = TaskOwnership{Kind: "conversation"}
			for _, work := range tx.state.Tasks {
				if work.Conversation == task.Conversation && work.Kind == "task.pi.compaction" && !terminalStatus(work.Status) {
					return nil
				}
			}
		}
		// Automatic work follows current retry settings after each response;
		// never convert host defaults into an explicit task-local override.
		child, err := tx.CreateTask(definition, JSON{"reason": reason, "keepRecentTokens": policy.KeepRecentTokens, "maxTokens": policy.MaxTokens}, TaskOptions{Ownership: ownership, Background: background})
		if err != nil {
			return err
		}
		if background {
			return nil
		}
		if receipt != nil {
			value, err := dtoObject(*receipt, tx.limits)
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
			usage, err := builtin(tx, task.Conversation, "pi.usage")
			if err != nil {
				return err
			}
			if err = usage.Update(func(value JSON) error { return addModelUsage(value, *receipt, tx.limits) }); err != nil {
				return err
			}
		}
		if overflow {
			cp.OverflowMessage = "Context overflow"
			if receipt != nil && receipt.ErrorMessage != "" {
				cp.OverflowMessage = receipt.ErrorMessage
			}
		}
		cp.ResumeAfterCompaction = overflow || cp.Phase == "prepare-next"
		cp.Phase, cp.Compaction = "compaction", child
		current.Checkpoint, err = dtoObject(cp, tx.limits)
		if err != nil {
			return err
		}
		current.Status = "pending"
		return tx.stage(Write{Op: "put-task", Task: &current})
	})
	return err == nil && !background, err
}
func (h *Harness) sessionPrepareAfterCompaction(runtime *TaskRuntime, task Task, cp generationCheckpoint) error {
	_, err := h.session.invocationCommit(context.Background(), task.ID, func(tx *Tx) error {
		if err := runtime.check(); err != nil {
			return err
		}
		current := tx.state.Tasks[task.ID]
		if h.closing.Load() || taskAborted(current) {
			return ErrSealed
		}
		child, ok := tx.state.Tasks[cp.Compaction]
		if !ok || !terminalStatus(child.Status) || h.scheduler.invocations[child.ID] != nil {
			return reject("compaction still live")
		}
		var err error
		current.Checkpoint, err = dtoObject(cp, tx.limits)
		if err != nil {
			return err
		}
		current.Status = "pending"
		return tx.stage(Write{Op: "put-task", Task: &current})
	})
	return err
}

// Use actual usage at the newest visible assistant as the anchor, estimating
// only the later context. Head markers reset that anchor's applicability.
func estimateContextTokens(view ContextView) int {
	tokens := 0
	for _, message := range view.Messages {
		tokens += goai.EstimateMessageTokens(receiptMessage(message))
	}
	for i := len(view.Contributions) - 1; i >= 0; i-- {
		if view.Head != nil && view.Entries[i].ID <= view.Head.ID {
			break
		}
		messages := view.Contributions[i]
		for j := len(messages) - 1; j >= 0; j-- {
			message := messages[j]
			if message.Role != goai.RoleAssistant || message.Usage == nil {
				continue
			}
			actual := goai.CalculateContextTokens(message.Usage)
			if actual <= 0 {
				continue
			}
			tokens = actual
			for _, later := range messages[j+1:] {
				tokens += goai.EstimateMessageTokens(receiptMessage(later))
			}
			for _, contributions := range view.Contributions[i+1:] {
				for _, later := range contributions {
					tokens += goai.EstimateMessageTokens(receiptMessage(later))
				}
			}
			return tokens
		}
	}
	return tokens
}
