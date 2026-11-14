package durable

import "fmt"

// CreateConversation follows the source ownership-options constructor. The
// record's identity is minted here; a task owner supplies its conversation.
func (t *Tx) CreateConversation(options JSON) (Conversation, error) {
	if err := t.enterKeys(); err != nil {
		return Conversation{}, err
	}
	defer t.leave()
	t.hasTableWrite = true
	record, err := t.allocateConversationRecord(options)
	if err != nil {
		return Conversation{}, err
	}
	if err = t.stageConversationRecord(record); err != nil {
		return Conversation{}, err
	}
	return record, nil
}

// Entered by the create/fork source overloads before any parent-copy read.
func (t *Tx) allocateConversationRecord(options JSON) (Conversation, error) {
	if options == nil {
		return Conversation{}, reject("Cannot read properties of null (reading 'ownership')")
	}
	ownership, present := options["ownership"]
	if !present {
		return Conversation{}, reject("Cannot read properties of undefined (reading 'kind')")
	}
	if ownership == nil {
		return Conversation{}, reject("Cannot read properties of null (reading 'kind')")
	}
	var kind, ownerValue any
	ownerPresent := false
	switch value := ownership.(type) {
	case JSON:
		kind = value["kind"]
		ownerValue, ownerPresent = value["taskId"]
	case map[string]any:
		kind = value["kind"]
		ownerValue, ownerPresent = value["taskId"]
	}
	// Capture before minting/awaiting. The source's lookup guard is the
	// extracted taskId !== undefined; an omitted ID remains ownerless.
	taskOwned := kind == "task" && ownerPresent
	owner := ID(0)
	if taskOwned {
		if id, ok := ownerValue.(ID); ok {
			owner = id
		} else {
			owner = submissionEntryID(ownerValue)
		}
	}
	id, err := t.store.mintID(t.ctx, true)
	if err != nil {
		return Conversation{}, err
	}
	t.state.HighWater = uint64(id)
	record := Conversation{ID: id}
	if taskOwned {
		var task Task
		found := false
		for i := len(t.writes) - 1; i >= 0; i-- {
			if candidate := t.writes[i].Task; candidate != nil && candidate.ID == owner {
				task, found = *candidate, true
				break
			}
		}
		if !found {
			if err := t.touchKeys(taskKey(owner)); err != nil {
				return Conversation{}, err
			}
			task, found = t.state.Tasks[owner]
		}
		if !found {
			text := fmt.Sprint(ownerValue)
			if ownerValue == nil {
				text = "null"
			}
			return Conversation{}, reject(fmt.Sprintf("Conversation owner task %s does not exist", text))
		}
		record.Owner, record.OwnerConversation = owner, task.Conversation
	}
	return record, nil
}

// ForkConversation is the source ownership-options fork overload. Allocation
// and owner lookup precede the persisted cutoff read, even when it is missing.
func (t *Tx) ForkConversation(parent, at ID, options JSON) (Conversation, error) {
	if err := t.enterKeys(); err != nil {
		return Conversation{}, err
	}
	defer t.leave()
	t.hasTableWrite = true
	child, err := t.allocateConversationRecord(options)
	if err != nil {
		return Conversation{}, err
	}
	if err = t.touchKeys(conversationKey(parent), entryKey(at)); err != nil {
		return Conversation{}, err
	}
	if !entryVisible(t.state, parent, at) {
		return Conversation{}, reject(fmt.Sprintf("Entry %d is not visible from conversation %d", at, parent))
	}
	entry := t.state.Entries[at]
	// The source scans the entry-owning ancestor as of the cutoff and the
	// immediate parent currently; each scope has its own publication gate.
	if err = t.touchKeys(scopeKey("conversation", entry.Conversation), scopeKey("conversation", parent)); err != nil {
		return Conversation{}, err
	}
	child.Parent, child.ParentAt = parent, at
	return t.stageForkConversation(child, entry)
}
