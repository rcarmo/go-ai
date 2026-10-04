package durable

import (
	"context"
	"errors"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"math"
	"sort"
	"strconv"
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
func (h *Harness) runConversation(id ID) bool {
	for {
		if h.life.Err() != nil {
			return false
		}
		task, cp, ok, e := h.nextTask(id)
		if e != nil {
			return false
		}
		if !ok {
			return true
		}
		if cp.Abort {
			if e = h.drainAborted(task, cp); e != nil {
				return false
			}
			continue
		}
		if cp.Phase == "tools" {
			if e = h.runOwnedTools(&task, &cp); e != nil {
				if _, latest, ok, err := h.nextTask(id); err == nil && ok && latest.Abort {
					continue
				}
				return false
			}
			continue
		}
		if cp.Phase == "queued" {
			if e = h.prepareRequest(&task, &cp); e != nil {
				var rejected *StorageRejected
				if h.life.Err() == nil && errors.As(e, &rejected) {
					if h.finish(task, cp, errorReceipt("invalid_preparation"), false) == nil {
						h.notify()
						continue
					}
				}
				return false
			}
		}
		if cp.Partial != nil {
			if e = h.sessionCommitInterruptedPartial(task, cp); e != nil {
				return false
			}
			cp.Partial = nil
		}
		// On recovery reuse persisted model/settings/cutoff. A new logical attempt is
		// committed before remote dispatch. Generic providers have no exactly-once
		// billing guarantee; an interrupted request can be billed again.
		if cp.Attempt >= MaxID {
			_ = h.finish(task, cp, messageReceipt{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, ErrorCode: "attempt_exhausted"}, false)
			continue
		}
		cp.Attempt++
		task.Status = "running"
		value, e := dtoObject(cp, h.session.limits)
		if e != nil {
			return false
		}
		task.Checkpoint = value
		if _, e = h.session.Commit(context.Background(), func(tx *Tx) error {
			var current generationCheckpoint
			if e := fromObject(tx.state.Tasks[task.ID].Checkpoint, &current, h.session.limits); e != nil {
				return e
			}
			if current.Abort {
				return reject("generation aborted")
			}
			return tx.PutTask(task)
		}); e != nil {
			if _, latest, ok, err := h.nextTask(id); err == nil && ok && latest.Abort {
				continue
			}
			return false
		}
		if h.life.Err() != nil {
			return false
		}
		if cp.Model == nil {
			return false
		}
		local := h.options.Models(cp.Agent.Model.Provider, cp.Agent.Model.ID)
		model, e := cloneModel(cp.Model, h.session.limits)
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
			if h.finish(task, cp, errorReceipt("model_unavailable"), false) != nil {
				return false
			}
			continue
		}
		var options *goai.StreamOptions
		if h.options.RequestOptions != nil {
			options, e = h.options.RequestOptions(h.life, cp.Agent.Model)
			if e != nil {
				if h.life.Err() != nil {
					return false
				}
				if h.finish(task, cp, errorReceipt("request_options_unavailable"), false) != nil {
					return false
				}
				continue
			}
		}
		options, e = cloneOptions(options, h.session.limits)
		if e != nil {
			if h.finish(task, cp, errorReceipt("invalid_options"), false) != nil {
				return false
			}
			continue
		}
		if options.Deferred != nil {
			if h.finish(task, cp, errorReceipt("deferred_unsupported"), false) != nil {
				return false
			}
			continue
		}
		options.Temperature = cp.Agent.Settings.Temperature
		options.MaxTokens = cp.Agent.Settings.MaxTokens
		conv := &goai.Context{SystemPrompt: cp.Agent.SystemPrompt}
		conv.Tools, e = protocolTools(cp.Offered, h.session.limits)
		if e != nil {
			return false
		}
		for _, m := range cp.Messages {
			conv.Messages = append(conv.Messages, receiptMessage(m))
		}
		h.mu.Lock()
		admission, admissionErr := h.session.Snapshot(context.Background())
		if admissionErr != nil {
			h.mu.Unlock()
			return false
		}
		var admitted generationCheckpoint
		if admissionErr = fromObject(admission.Tasks[task.ID].Checkpoint, &admitted, h.session.limits); admissionErr != nil {
			h.mu.Unlock()
			return false
		}
		if admitted.Abort {
			h.mu.Unlock()
			continue
		}
		if h.closing.Load() {
			h.mu.Unlock()
			return false
		}
		requestCtx, requestCancel := context.WithCancel(h.life)
		h.invocations[task.ID] = requestCancel
		h.mu.Unlock()
		receipt, success, received := h.drain(goai.Stream(requestCtx, model, conv, options), cp.Model, task.ID)
		h.mu.Lock()
		delete(h.invocations, task.ID)
		h.mu.Unlock()
		requestCancel()
		// Close joins the full provider channel, including a noncooperative provider.
		// Its cancellation never invents an aborted durable terminal outcome.
		if h.life.Err() != nil && !received {
			return false
		}
		if current, checkpoint, ok, err := h.nextTask(id); err != nil {
			return false
		} else if ok && current.ID == task.ID && checkpoint.Abort {
			if e = h.drainAborted(current, checkpoint, receipt); e != nil {
				return false
			}
			continue
		}
		if success && receipt.StopReason == goai.StopReasonToolUse {
			if e = h.acceptTools(task, cp, receipt); e != nil {
				if _, latest, ok, err := h.nextTask(id); err == nil && ok && latest.Abort {
					continue
				}
				return false
			}
			h.notify()
			continue
		}
		if e = h.finish(task, cp, receipt, success); e != nil {
			if _, latest, ok, err := h.nextTask(id); err == nil && ok && latest.Abort {
				continue
			}
			return false
		}
		h.notify()
	}
}
func (h *Harness) prepareRequest(task *Task, cp *generationCheckpoint) (err error) {
	original := *cp
	defer func() {
		if err != nil {
			*cp = original
		}
	}()
	s, e := h.session.Snapshot(context.Background())
	if e != nil {
		return e
	}
	d, ok := agentDocument(s, task.Conversation)
	if !ok {
		return reject("agent unavailable")
	}
	if e = fromObject(d.Value, &cp.Agent, h.session.limits); e != nil {
		return e
	}
	view, e := deriveContextView(s, task.Conversation, 0, h.session.limits)
	if e != nil {
		return e
	}
	messages := view.Messages
	// Validate the process-local options seam before committing a request intent;
	// only auth/read-only host observations are permitted, never payload rewrites.
	if h.options.RequestOptions != nil {
		resolved, err := h.options.RequestOptions(h.life, cp.Agent.Model)
		if err != nil {
			return reject("request options unavailable")
		}
		if _, err = cloneOptions(resolved, h.session.limits); err != nil {
			return err
		}
	}
	// Pin all non-secret model behavior before the intent write. Endpoint and
	// credentials are erased from persistence and rehydrated process-locally.
	model, e := cloneModel(h.options.Models(cp.Agent.Model.Provider, cp.Agent.Model.ID), h.session.limits)
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
	offers, pins, e := h.options.Registry.snapshot(h.session.limits)
	if e != nil {
		return e
	}
	cp.Offered = offers
	cp.Phase = "intent"
	cp.Messages = append(messages, userReceipt(cp.Input))
	value, e := dtoObject(cp, h.session.limits)
	if e != nil {
		return e
	}
	task.Status = "running"
	task.Checkpoint = value
	_, e = h.session.Commit(context.Background(), func(tx *Tx) error {
		var latest generationCheckpoint
		if e := fromObject(tx.state.Tasks[task.ID].Checkpoint, &latest, h.session.limits); e != nil {
			return e
		}
		if latest.Abort {
			return reject("generation aborted")
		}
		id, e := tx.MintID()
		if e != nil {
			return e
		}
		user, e := dtoObject(userReceipt(cp.Input), h.session.limits)
		if e != nil {
			return e
		}
		if e = tx.AppendEntry(Entry{ID: id, Conversation: task.Conversation, Kind: "message", Value: user}); e != nil {
			return e
		}
		if e = tx.PutTask(*task); e != nil {
			return e
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
		return live.Set(JSON{"run": JSON{"task": task.ID, "inputs": []any{cp.Submission}}})
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
	lastPartial := ""
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
				var partial *goai.Message
				switch e := event.(type) {
				case *goai.TextDeltaEvent:
					partial = e.Partial
				case *goai.ThinkingDeltaEvent:
					partial = e.Partial
				}
				if partial != nil {
					text := ""
					for _, c := range partial.Content {
						part := ""
						if c.Type == "text" {
							part = c.Text
						} else if c.Type == "thinking" {
							part = c.Thinking
						}
						if len(part) > MaxToolOutputBytes-len(text) {
							text = ""
							break
						}
						text += part
					}
					if text != "" && text != lastPartial {
						lastPartial = text
						r := messageReceipt{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: text}}, Api: pinned.Api, Provider: pinned.Provider, Model: pinned.ID, StopReason: goai.StopReasonAborted}
						if err := h.commitPartial(taskID[0], r); err != nil {
							result = errorReceipt("partial_commit_failed")
							success = false
						}
					}
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
		r := messageReceipt{Role: goai.RoleAssistant, Content: []goai.ContentBlock{}, Api: pinned.Api, Provider: pinned.Provider, Model: pinned.ID, Usage: message.Usage, StopReason: message.StopReason, Timestamp: message.Timestamp, ErrorCode: code}
		for _, c := range message.Content {
			switch c.Type {
			case "text", "thinking":
				r.Content = append(r.Content, goai.ContentBlock{Type: c.Type, Text: c.Text, Thinking: c.Thinking})
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
				r.Content = append(r.Content, goai.ContentBlock{Type: "toolCall", ID: c.ID, Name: c.Name, Arguments: arguments})
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
			if len(r.Content) == 0 {
				ok = false
				r.ErrorCode = "invalid_tool_round"
			}
		case goai.StopReasonDeferred, goai.StopReasonPending:
			ok = false
			r.ErrorCode = "deferred_unsupported"
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
	_, e = h.session.Commit(context.Background(), func(tx *Tx) error {
		sub, ok := tx.state.Submissions[cp.Submission]
		if !ok {
			return reject("submission unavailable")
		}
		if terminalStatus(sub.Status) {
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
		if e = tx.AppendEntry(Entry{ID: id, Conversation: task.Conversation, Kind: "message", Value: value}); e != nil {
			return e
		}
		sub.Status = status
		sub.Value = JSON{"message": value}
		if e = tx.PutSubmission(sub); e != nil {
			return e
		}
		for _, child := range tx.state.Tasks {
			if child.Owner == task.ID && !terminalStatus(child.Status) {
				return reject("owned tools not drained")
			}
		}
		if e = tx.PutTask(task); e != nil {
			return e
		}
		live, e := builtin(tx, task.Conversation, "pi.live")
		if e != nil {
			return e
		}
		if e = live.Set(JSON{}); e != nil {
			return e
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
	if old, ok := models[key].(map[string]any); ok {
		if e := fromObject(JSON(old), &prior, l); e != nil {
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
	_, e := h.session.Commit(context.Background(), func(tx *Tx) error {
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
		return tx.PutTask(task)
	})
	return e
}

func (h *Harness) sessionCommitInterruptedPartial(task Task, cp generationCheckpoint) error {
	_, e := h.session.Commit(context.Background(), func(tx *Tx) error {
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
		receipt.ErrorCode = "interrupted"
		id, e := tx.MintID()
		if e != nil {
			return e
		}
		value, e := dtoObject(receipt, h.session.limits)
		if e != nil {
			return e
		}
		if e = tx.AppendEntry(Entry{ID: id, Conversation: task.Conversation, Kind: "message", Value: value}); e != nil {
			return e
		}
		latest.Partial = nil
		value, e = dtoObject(latest, h.session.limits)
		if e != nil {
			return e
		}
		current.Checkpoint = value
		return tx.PutTask(current)
	})
	return e
}
