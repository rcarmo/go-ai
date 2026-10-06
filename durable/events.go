package durable

import (
	"context"
	"sort"
	"strings"
)

// AgentEvent is derived only from adopted records. Raw provider events are never
// forwarded before persistence. A snapshot replaces queued batches on overflow.
type AgentEvent struct {
	Type         string
	Seq          uint64
	Entry        *Entry
	Submission   *Submission
	Task         ID
	Kind         string
	ToolCallID   string
	ToolName     string
	Args         JSON
	Output       string
	Message      *MessageReceipt
	Changes      []MessageChange
	OutputChange *ToolOutputChange
	Inputs       []ID
	RetryAt      int64
	Attempt      uint64
	Snapshot     *ConversationSnapshot
	Agent        JSON
	Usage        JSON
	Items        []QueuedItem
	PollAt       int64
	Reason       string
	Blocking     bool
	Error        string
	Details      any
	HasDetails   bool
	DroppedBytes uint64
	DroppedLines uint64
	Diagnostics  []ToolDiagnostic
}
type QueuedItem struct {
	ID   ID
	Mode string
}
type ConversationSnapshot struct {
	Conversation ID
	Entries      []Entry
	Tasks        []TaskRecord
	Documents    map[string]JSON
	Submissions  []Submission
}
type AgentEventStream struct {
	conversation ID
	initial      ConversationSnapshot
	current      Snapshot
	sub          *CommitSubscription
	limits       Limits
}

func conversationSnapshot(state Snapshot, id ID, limits Limits) (ConversationSnapshot, error) {
	result := ConversationSnapshot{Conversation: id, Documents: map[string]JSON{}, Entries: []Entry{}, Tasks: []TaskRecord{}, Submissions: []Submission{}}
	view, err := buildConversationView(state, id, limits)
	if err != nil {
		return result, err
	}
	result.Entries = view.Entries
	result.Documents = view.Docs
	for _, tid := range ids(state.Tasks) {
		task := state.Tasks[tid]
		if task.Conversation != id || terminalStatus(task.Status) {
			continue
		}
		record, err := CanonicalTask(task, limits)
		if err != nil {
			return result, err
		}
		result.Tasks = append(result.Tasks, record)
	}
	for _, sid := range ids(state.Submissions) {
		sub := state.Submissions[sid]
		if sub.Conversation != id || terminalStatus(sub.Status) {
			continue
		}
		sub.Value, err = copyObject(sub.Value, limits)
		if err != nil {
			return result, err
		}
		result.Submissions = append(result.Submissions, sub)
	}
	return result, nil
}
func (d Document) ConversationScope(id ID) bool {
	return d.Scope == "conversation" && d.Owner == id && !d.Retired
}
func (h *Harness) WatchEvents(ctx context.Context, conversation ID) (*AgentEventStream, error) {
	if h.closing.Load() {
		return nil, ErrClosed
	}
	state, sub, err := h.session.SubscribeCommits(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := state.Conversations[conversation]; !ok {
		sub.Stop()
		return nil, reject("unknown conversation")
	}
	initial, err := conversationSnapshot(state, conversation, h.session.limits)
	if err != nil {
		sub.Stop()
		return nil, err
	}
	return &AgentEventStream{conversation: conversation, initial: initial, current: state, sub: sub, limits: h.session.limits}, nil
}
func (s *AgentEventStream) Snapshot() (ConversationSnapshot, error) {
	// Rebuild from the immutable registration baseline rather than returning
	// callback-owned/current state. Public snapshots have independent JSON.
	return copyConversationSnapshot(s.initial, s.limits)
}
func copyConversationSnapshot(value ConversationSnapshot, limits Limits) (ConversationSnapshot, error) {
	result := ConversationSnapshot{Conversation: value.Conversation, Documents: map[string]JSON{}}
	for _, entry := range value.Entries {
		copy, err := copyEntry(entry, limits)
		if err != nil {
			return result, err
		}
		result.Entries = append(result.Entries, copy)
	}
	for _, record := range value.Tasks {
		wire, err := encodeBounded(record, limits, limits.MaxRecordBytes)
		if err != nil {
			return result, err
		}
		var copy TaskRecord
		if err = decodeStrict(wire, limits, limits.MaxRecordBytes, &copy); err != nil {
			return result, err
		}
		result.Tasks = append(result.Tasks, copy)
	}
	for key, value := range value.Documents {
		copy, err := copyObject(value, limits)
		if err != nil {
			return result, err
		}
		result.Documents[key] = copy
	}
	for _, sub := range value.Submissions {
		copy, err := copyObject(sub.Value, limits)
		if err != nil {
			return result, err
		}
		sub.Value = copy
		result.Submissions = append(result.Submissions, sub)
	}
	return result, nil
}
func (s *AgentEventStream) Closed() <-chan struct{} { return s.sub.Closed() }
func (s *AgentEventStream) End() (WatchEnd, bool)   { return s.sub.End() }
func (s *AgentEventStream) Stop() WatchEnd          { return s.sub.Stop() }
func (s *AgentEventStream) Start(listener func(context.Context, []AgentEvent) error) error {
	if listener == nil {
		return reject("nil event listener")
	}
	return s.sub.Start(func(ctx context.Context, frame PublicationFrame) error {
		if frame.Snapshot != nil {
			s.current = *frame.Snapshot
			value, err := conversationSnapshot(s.current, s.conversation, s.limits)
			if err != nil {
				return err
			}
			return listener(ctx, []AgentEvent{{Type: "snapshot", Seq: s.current.Seq, Snapshot: &value}})
		}
		next := candidateTables(s.current)
		next.Seq = frame.Publication.Seq
		events := []AgentEvent{}
		for _, write := range frame.Publication.Tables {
			switch {
			case write.Task != nil:
				task := *write.Task
				old := s.current.Tasks[task.ID]
				next.Tasks[task.ID] = task
				if task.Conversation != s.conversation {
					continue
				}
				if task.Kind == "pi.tool" {
					var before, after toolCheckpoint
					if err := fromObject(task.Checkpoint, &after, s.limits); err != nil {
						return err
					}
					if old.Checkpoint != nil {
						if err := fromObject(old.Checkpoint, &before, s.limits); err != nil {
							return err
						}
					}
					base := AgentEvent{Seq: next.Seq, Task: task.ID, ToolCallID: after.CallID, ToolName: after.Offer.Name}
					if after.Started && !before.Started {
						event := base
						event.Type = "tool_execution_start"
						var err error
						event.Args, err = copyObject(after.Arguments, s.limits)
						if err != nil {
							return err
						}
						events = append(events, event)
					}
					detailsChanged := before.HasDetails != after.HasDetails || !equalJSONValue(before.Details, after.Details)
					diagnosticsChanged := !equalJSONValue(before.Diagnostics, after.Diagnostics)
					if after.Output != before.Output || detailsChanged || diagnosticsChanged || after.DroppedBytes != before.DroppedBytes || after.DroppedLines != before.DroppedLines {
						event := base
						event.Type = "tool_execution_update"
						event.DroppedBytes, event.DroppedLines = after.DroppedBytes, after.DroppedLines
						if diagnosticsChanged {
							from := 0
							if len(before.Diagnostics) <= len(after.Diagnostics) && equalJSONValue(before.Diagnostics, after.Diagnostics[:len(before.Diagnostics)]) {
								from = len(before.Diagnostics)
							}
							event.Diagnostics = append([]ToolDiagnostic{}, after.Diagnostics[from:]...)
						}
						if after.Output != before.Output {
							event.Output = after.Output
							event.OutputChange = outputChange(before.Output, after.Output)
						}
						if detailsChanged {
							event.HasDetails = true
							var err error
							event.Details, err = ownJSONValue(after.Details, s.limits)
							if err != nil {
								return err
							}
						}
						events = append(events, event)
					}
					schedulerMissing := task.Execution != nil && task.Execution.Builtin != nil && task.Execution.Builtin.Hold != nil && task.Execution.Builtin.Hold.Action == "scheduler-tool-missing"
					ended := taskHasDecidedOutcome(task) && !taskHasDecidedOutcome(old)
					if schedulerMissing {
						ended = terminalStatus(task.Status) && !terminalStatus(old.Status)
					}
					if ended {
						event := base
						event.Type = "tool_execution_end"
						if after.Result != nil {
							copy, err := dtoObject(after.Result, s.limits)
							if err != nil {
								return err
							}
							var message MessageReceipt
							if err = fromObject(copy, &message, s.limits); err != nil {
								return err
							}
							event.Message = &message
						}
						events = append(events, event)
					}
				} else if task.Kind == "pi.generation" {
					var before, after generationCheckpoint
					if err := fromObject(task.Checkpoint, &after, s.limits); err != nil {
						return err
					}
					if old.Checkpoint != nil {
						if err := fromObject(old.Checkpoint, &before, s.limits); err != nil {
							return err
						}
					}

					if after.Partial != nil && !equalJSONValue(after.Partial, before.Partial) {
						copy, err := dtoObject(after.Partial, s.limits)
						if err != nil {
							return err
						}
						var message MessageReceipt
						if err = fromObject(copy, &message, s.limits); err != nil {
							return err
						}
						kind := "message_update"
						if before.Partial == nil {
							kind = "message_start"
						}
						event := AgentEvent{Type: kind, Seq: next.Seq, Task: task.ID, Message: &message}
						if kind == "message_update" {
							event.Changes, err = partialMessageChanges(*before.Partial, message, s.limits)
							if err != nil {
								return err
							}
							if message.Usage != nil {
								event.Usage, err = dtoObject(message.Usage, s.limits)
								if err != nil {
									return err
								}
							}
						}
						events = append(events, event)
					}
					if after.Phase == "retry" && before.Phase != "retry" {
						events = append(events, AgentEvent{Type: "auto_retry_start", Seq: next.Seq, Task: task.ID, RetryAt: after.RetryUntil, Attempt: after.Attempt})
					}
					if after.Phase == "poll" && (before.Phase != "poll" || before.PollAt != after.PollAt) {
						events = append(events, AgentEvent{Type: "deferred_poll", Seq: next.Seq, Task: task.ID, PollAt: after.PollAt})
					}
					if before.Phase == "retry" && after.Phase != "retry" {
						events = append(events, AgentEvent{Type: "auto_retry_end", Seq: next.Seq, Task: task.ID, Attempt: after.Attempt})
					}
					if taskHasDecidedOutcome(task) && !taskHasDecidedOutcome(old) {
						events = append(events, AgentEvent{Type: "turn_end", Seq: next.Seq, Task: task.ID})
					}
				}
				if terminalStatus(task.Status) && !terminalStatus(old.Status) {
					view, err := CanonicalTask(task, s.limits)
					if err != nil {
						return err
					}
					if view.State.Outcome != nil && (view.State.Outcome.Status == "faulted" || view.State.Outcome.Status == "orphaned") {
						message := view.State.Outcome.Reason
						if view.State.Outcome.Error != nil {
							message = view.State.Outcome.Error.Message
						}
						events = append(events, AgentEvent{Type: "task_failed", Seq: next.Seq, Task: task.ID, Kind: task.Kind, Error: message})
					}
				}
			case write.Entry != nil:
				entry := *write.Entry
				next.Entries[entry.ID] = entry
				if entry.Conversation != s.conversation {
					continue
				}
				copy, err := copyEntry(entry, s.limits)
				if err != nil {
					return err
				}
				events = append(events, AgentEvent{Type: "entry_appended", Seq: next.Seq, Entry: &copy})
				if entry.Kind == "message" || len(entry.Model) > 0 {
					second, err := copyEntry(entry, s.limits)
					if err != nil {
						return err
					}
					events = append(events, AgentEvent{Type: "message_end", Seq: next.Seq, Entry: &second})
				}
			case write.Submission != nil:
				sub := *write.Submission
				next.Submissions[sub.ID] = sub
				if sub.Conversation == s.conversation {
					copy, err := copyObject(sub.Value, s.limits)
					if err != nil {
						return err
					}
					sub.Value = copy
					events = append(events, AgentEvent{Type: "submission", Seq: next.Seq, Submission: &sub})
				}
			case write.Conversation != nil:
				next.Conversations[write.Conversation.ID] = *write.Conversation
			}
		}
		for _, change := range frame.Publication.Documents {
			doc := change.Record
			old := s.current.Documents[doc.ID]
			doc.Value = change.Value
			next.Documents[doc.ID] = doc
			if doc.Scope != "conversation" || doc.Owner != s.conversation || doc.Family || doc.Key != "" {
				continue
			}
			kind := map[string]string{"pi.agent": "agent_changed", "pi.usage": "usage_changed", "pi.inbox": "inbox_update"}[doc.Kind]
			if kind != "" {
				event := AgentEvent{Type: kind, Seq: next.Seq}
				if kind == "inbox_update" {
					event.Items = queuedEventItems(doc.Value)
				} else {
					value := doc.Value
					if value == nil {
						value = JSON{}
					}
					copy, err := copyObject(value, s.limits)
					if err != nil {
						return err
					}
					if kind == "agent_changed" {
						event.Agent = copy
					} else {
						event.Usage = copy
					}
				}
				events = append(events, event)
			}
			if doc.Kind == "pi.live" {
				previous, current := eventCompactionStatuses(old.Value), eventCompactionStatuses(doc.Value)
				for _, id := range ids(previous) {
					if _, ok := current[id]; !ok {
						events = append(events, AgentEvent{Type: "compaction_end", Seq: next.Seq, Task: id, Reason: previous[id].Reason})
					}
				}
				for _, id := range ids(current) {
					if _, ok := previous[id]; !ok {
						status := current[id]
						status.Type = "compaction_start"
						status.Seq = next.Seq
						events = append(events, status)
					}
				}
				was := old.Value["run"]
				now := doc.Value["run"]
				oldInputs, newInputs := eventRunInputs(was), eventRunInputs(now)
				var oldFirst, newFirst ID
				if len(oldInputs) > 0 {
					oldFirst = oldInputs[0]
				}
				if len(newInputs) > 0 {
					newFirst = newInputs[0]
				}
				// The first input identifies the run; successor task handoff
				// keeps it. Direct queued-user replacement ends/starts runs.
				changed := oldFirst != newFirst || (was == nil) != (now == nil)
				if changed && was != nil {
					events = append(events, AgentEvent{Type: "run_end", Seq: next.Seq, Inputs: oldInputs})
				}
				if changed && now != nil {
					events = append(events, AgentEvent{Type: "run_start", Seq: next.Seq, Inputs: newInputs})
				}
				runTask := func(value any) ID {
					if run, ok := value.(map[string]any); ok {
						if number, ok := exactNumber(run["task"]); ok && number.IsInt() && number.Num().IsUint64() {
							return ID(number.Num().Uint64())
						}
					}
					return 0
				}
				oldTask, newTask := runTask(was), runTask(now)
				if newTask != 0 && newTask != oldTask {
					if task, ok := next.Tasks[newTask]; ok && task.Kind == "pi.generation" {
						events = append(events, AgentEvent{Type: "turn_start", Seq: next.Seq, Task: newTask})
					}
				}
			}
		}
		before := s.current
		s.current = next
		for i := range events {
			if strings.HasPrefix(events[i].Type, "tool_execution") && events[i].Kind == "" {
				events[i].Kind = "pi.tool"
			}
		}
		if len(events) == 0 {
			return nil
		}
		// Progress first, then messages/ends, followed by committed state
		// and starts. Stable ordering preserves entries and tool call order.
		sort.SliceStable(events, func(i, j int) bool { return eventOrder(events[i].Type) < eventOrder(events[j].Type) })
		events, err := arrangeEntryEvents(events, before, s.conversation, s.limits)
		if err != nil {
			return err
		}
		return listener(ctx, events)
	})
}

func eventRunInputs(run any) []ID {
	result := []ID{}
	object, ok := run.(map[string]any)
	if !ok {
		return result
	}
	items, _ := object["inputs"].([]any)
	for _, item := range items {
		number, ok := exactNumber(item)
		if ok {
			result = append(result, ID(number.Num().Uint64()))
		}
	}
	return result
}
func queuedEventItems(value JSON) []QueuedItem {
	result := []QueuedItem{}
	items, _ := value["items"].([]any)
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		number, ok := exactNumber(object["id"])
		if !ok {
			continue
		}
		mode, _ := object["mode"].(string)
		result = append(result, QueuedItem{ID: ID(number.Num().Uint64()), Mode: mode})
	}
	return result
}
func eventCompactionStatuses(value JSON) map[ID]AgentEvent {
	result := map[ID]AgentEvent{}
	items, _ := value["compactions"].([]any)
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		number, ok := exactNumber(object["taskId"])
		if !ok || !number.IsInt() {
			continue
		}
		id := ID(number.Num().Uint64())
		reason, _ := object["reason"].(string)
		blocking, _ := object["blocking"].(bool)
		result[id] = AgentEvent{Task: id, Reason: reason, Blocking: blocking}
	}
	return result
}
func eventOrder(kind string) int {
	switch kind {
	case "tool_execution_start", "tool_execution_update", "message_start", "message_update", "auto_retry_start", "auto_retry_end", "deferred_poll":
		return 0
	case "tool_execution_end":
		return 1
	case "entry_appended", "message_end":
		return 2
	case "compaction_end", "task_failed":
		return 3
	case "turn_end":
		return 4
	case "run_end":
		return 5
	case "submission":
		return 6
	case "inbox_update", "agent_changed", "usage_changed":
		return 7
	case "compaction_start":
		return 8
	case "run_start":
		return 9
	case "turn_start":
		return 10
	}
	return 0
}
