package durable

import (
	"fmt"
	"maps"
)

// setTask is the internal Transaction replacement operation, not public Tx.
// Committed lookup is deferred until assembly; only an existing staged
// candidate imposes the immediate terminal/conversation guards.
func (t *Tx) setTask(value TaskRecord) error {
	if err := t.enterKeys(); err != nil {
		return err
	}
	defer t.leave()
	return t.stageCanonicalTask(value)
}

func (t *Tx) stageCanonicalTask(value TaskRecord) error {
	t.hasTableWrite = true
	candidate, found, err := t.stagedCanonicalTask(value.ID)
	if err != nil {
		return err
	}
	if found {
		if candidate.State.Status == "terminal" {
			return reject(fmt.Sprintf("Task %d already has a terminal candidate", value.ID))
		}
		if candidate.Conversation != value.Conversation {
			return reject(fmt.Sprintf("Task %d cannot change conversations", value.ID))
		}
		if candidate.StartedAt != nil {
			value.StartedAt = candidate.StartedAt
		}
		if candidate.EndedAt != nil {
			value.EndedAt = candidate.EndedAt
		}
		if candidate.Memos != nil && value.State.Status != "terminal" && value.State.Status != "completing" {
			// Preserve memos written earlier in this transaction (for example,
			// a task's signal to itself before returning its next state).
			memos := maps.Clone(candidate.Memos)
			maps.Copy(memos, value.Memos)
			value.Memos = memos
		}
	}
	if value.State.Status == "running" && value.StartedAt == nil {
		stamp := t.session.now()
		value.StartedAt = &stamp
	}
	if value.State.Status == "terminal" && value.EndedAt == nil {
		stamp := t.session.now()
		value.EndedAt = &stamp
	}
	owned, err := copyPublicTaskRecord(value, t.limits)
	if err != nil {
		return err
	}
	if t.taskCandidates == nil {
		t.taskCandidates = map[ID]TaskRecord{}
	}
	if _, exists := t.taskCandidates[value.ID]; !exists {
		t.taskCandidateOrder = append(t.taskCandidateOrder, value.ID)
	}
	t.taskCandidates[value.ID] = owned
	return nil
}

func (t *Tx) stagedCanonicalTask(id ID) (TaskRecord, bool, error) {
	if value, found := t.taskCandidates[id]; found {
		return value, true, nil
	}
	for i := len(t.writes) - 1; i >= 0; i-- {
		if value := t.writes[i].Task; value != nil && value.ID == id {
			record, err := CanonicalTask(*value, t.limits)
			return record, err == nil, err
		}
	}
	return TaskRecord{}, false, nil
}

func (t *Tx) candidateCanonicalTask(id ID) (TaskRecord, bool, error) {
	if value, found, err := t.stagedCanonicalTask(id); found || err != nil {
		return value, found, err
	}
	if err := t.touchKeys(taskKey(id)); err != nil {
		return TaskRecord{}, false, err
	}
	value, found := t.state.Tasks[id]
	if !found {
		return TaskRecord{}, false, nil
	}
	record, err := CanonicalTask(value, t.limits)
	return record, err == nil, err
}

// Document/task ownership reads need the latest candidate without converting
// a setter into an early committed-table lookup or an early storage write.
func (t *Tx) withCanonicalTaskCandidates(state Snapshot) (Snapshot, error) {
	if len(t.taskCandidates) == 0 {
		return state, nil
	}
	state.Tasks = maps.Clone(state.Tasks)
	for id, record := range t.taskCandidates {
		value, err := nativeTask(record, t.limits)
		if err != nil {
			return Snapshot{}, err
		}
		state.Tasks[id] = value
	}
	return state, nil
}

// Source assembly validates final owners, then committed replacements, before
// admitting any record. No canonical setter operation is exported to callers.
func (t *Tx) finalizeCanonicalTasks() error {
	if len(t.taskCandidates) == 0 {
		return nil
	}
	created := map[ID]bool{}
	for _, write := range t.writes {
		if write.Task != nil {
			if _, found := t.state.Tasks[write.Task.ID]; !found {
				created[write.Task.ID] = true
			}
		}
	}
	validateOwner := func(id ID, what string) error {
		record, found, err := t.candidateCanonicalTask(id)
		if err != nil {
			return err
		}
		if !found {
			return reject(fmt.Sprintf("%s %d does not exist", what, id))
		}
		if record.State.Status == "terminal" || record.State.Status == "completing" {
			return reject(fmt.Sprintf("%s %d is %s", what, id, record.State.Status))
		}
		if record.AbortRequested {
			return reject(fmt.Sprintf("%s %d is abort-marked", what, id))
		}
		return nil
	}
	for _, write := range t.writes {
		if write.Conversation != nil && write.Op == "create-conversation" && write.Conversation.Owner != 0 {
			if err := validateOwner(write.Conversation.Owner, "Conversation owner task"); err != nil {
				return err
			}
		}
		if write.Task != nil && created[write.Task.ID] {
			owner := write.Task.Owner
			if record, found := t.taskCandidates[write.Task.ID]; found {
				owner = record.Owner
			}
			if owner != 0 {
				if err := validateOwner(owner, "Task owner"); err != nil {
					return err
				}
			}
		}
	}
	for _, id := range t.taskCandidateOrder {
		if created[id] {
			continue
		}
		if err := t.touchKeys(taskKey(id)); err != nil {
			return err
		}
		committed, found := t.state.Tasks[id]
		if !found {
			return reject(fmt.Sprintf("Task %d does not exist", id))
		}
		if terminalStatus(committed.Status) {
			return reject(fmt.Sprintf("Task %d is already terminal", id))
		}
		if committed.Conversation != t.taskCandidates[id].Conversation {
			return reject(fmt.Sprintf("Task %d cannot change conversations", id))
		}
	}
	converted := map[ID]Task{}
	for _, id := range t.taskCandidateOrder {
		value, err := nativeTask(t.taskCandidates[id], t.limits)
		if err != nil {
			return err
		}
		converted[id] = value
	}
	writes := make([]Write, 0, len(t.writes)+len(converted))
	placed := map[ID]bool{}
	for _, write := range t.writes {
		if write.Task != nil {
			if value, replace := converted[write.Task.ID]; replace {
				if !placed[value.ID] {
					owned := value
					writes = append(writes, Write{Op: "put-task", Task: &owned})
					placed[value.ID] = true
				}
				continue
			}
		}
		writes = append(writes, write)
	}
	for _, id := range t.taskCandidateOrder {
		if !placed[id] {
			value := converted[id]
			writes = append(writes, Write{Op: "put-task", Task: &value})
		}
	}
	t.writes = writes
	if t.sourceTaskReplacements == nil {
		t.sourceTaskReplacements = map[ID]bool{}
	}
	for _, id := range t.taskCandidateOrder {
		t.sourceTaskReplacements[id] = true
	}
	t.taskCandidates, t.taskCandidateOrder = nil, nil
	t.stagedState, t.stagedWrites, t.publicationState = nil, nil, nil
	return nil
}
