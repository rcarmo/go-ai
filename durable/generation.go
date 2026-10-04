package durable

import (
	"context"
	"errors"
	"fmt"
	goai "github.com/rcarmo/go-ai"
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
		if _, e = h.session.Commit(context.Background(), func(tx *Tx) error { return tx.PutTask(task) }); e != nil {
			return false
		}
		if h.life.Err() != nil {
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
		for _, m := range cp.Messages {
			conv.Messages = append(conv.Messages, receiptMessage(m))
		}
		receipt, success, received := h.drain(goai.Stream(h.life, model, conv, options), cp.Model)
		// Close joins the full provider channel, including a noncooperative provider.
		// Its cancellation never invents an aborted durable terminal outcome.
		if h.life.Err() != nil && !received {
			return false
		}
		if e = h.finish(task, cp, receipt, success); e != nil {
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
	messages, e := contextReceipts(s, task.Conversation, h.session.limits)
	if e != nil {
		return e
	}
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
	cp.Phase = "intent"
	cp.Messages = append(messages, userReceipt(cp.Input))
	value, e := dtoObject(cp, h.session.limits)
	if e != nil {
		return e
	}
	task.Status = "running"
	task.Checkpoint = value
	_, e = h.session.Commit(context.Background(), func(tx *Tx) error {
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
	return e
}
func errorReceipt(code string) messageReceipt {
	return messageReceipt{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, Content: []goai.ContentBlock{}, ErrorCode: code}
}
func (h *Harness) drain(events <-chan goai.Event, pinned *goai.Model) (messageReceipt, bool, bool) {
	if pinned == nil {
		return errorReceipt("missing_pinned_model"), false, false
	}
	terminal := 0
	result := errorReceipt("missing_terminal")
	success := false
	if events == nil {
		return errorReceipt("nil_stream"), false, false
	}
	// No raw frame visibility. Only terminal sanitized, bounded DTOs are retained.
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
				ok = false
				r.ErrorCode = "tools_unsupported"
			default:
				ok = false
				r.ErrorCode = "content_unsupported"
			}
		}
		switch message.StopReason {
		case goai.StopReasonStop, goai.StopReasonLength:
		case goai.StopReasonToolUse:
			ok = false
			r.ErrorCode = "tools_unsupported"
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
		return errorReceipt("invalid_terminal_count"), false, terminal > 0
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
	if success {
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
