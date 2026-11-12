package durable

import "context"

// canonicalStoredTask refuses legacy envelopes with no canonical input/version;
// Snapshot remains the explicit native-data inspection path for those records.
func canonicalStoredTask(task Task, limits Limits) (TaskRecord, error) {
	record, err := CanonicalTask(task, limits)
	if err != nil {
		return TaskRecord{}, err
	}
	if record.Version == 0 || record.Input == nil || !record.Input.Present {
		return TaskRecord{}, reject("legacy task has no canonical record")
	}
	return record, nil
}

// Task returns a detached canonical record, or found=false for an unused ID.
func (c *storeCore) Task(ctx context.Context, id ID) (TaskRecord, bool, error) {
	if err := c.enter(ctx); err != nil {
		return TaskRecord{}, false, err
	}
	defer c.leave()
	if err := checkReadIDs(id); err != nil {
		return TaskRecord{}, false, err
	}
	task, ok := c.state.Tasks[id]
	if !ok {
		return TaskRecord{}, false, nil
	}
	record, err := canonicalStoredTask(task, c.limits)
	return record, true, err
}

func (s *Session) Task(ctx context.Context, id ID) (record TaskRecord, found bool, err error) {
	if err = s.enter(ctx); err != nil {
		return
	}
	outcome := s.jobOutcome
	defer func() {
		s.leave()
		err = s.deliveredError(outcome, err)
		if err != nil {
			record = TaskRecord{}
			found = false
		}
	}()
	if err = checkReadIDs(id); err != nil {
		return
	}
	if err = s.touch(ctx, taskKey(id)); err != nil {
		return
	}
	if !s.hasNativeReadImage() {
		return s.guardedStorage().Task(ctx, id)
	}
	var state Snapshot
	state, err = s.taskSnapshot(ctx)
	if err != nil {
		return
	}
	task, ok := state.Tasks[id]
	found = ok
	if !ok {
		return
	}
	record, err = canonicalStoredTask(task, s.limits)
	return
}

func (t *Tx) Task(id ID) (TaskRecord, bool, error) {
	if err := t.enterTableRead("task", taskKey(id)); err != nil {
		return TaskRecord{}, false, err
	}
	defer t.leave()
	if err := checkReadIDs(id); err != nil {
		return TaskRecord{}, false, err
	}
	task, ok := t.state.Tasks[id]
	if !ok {
		return TaskRecord{}, false, nil
	}
	record, err := canonicalStoredTask(task, t.limits)
	return record, true, err
}

// Conversation returns the original immutable-identity record by global ID.
func (c *storeCore) Conversation(ctx context.Context, id ID) (Conversation, bool, error) {
	if err := c.enter(ctx); err != nil {
		return Conversation{}, false, err
	}
	defer c.leave()
	if err := checkReadIDs(id); err != nil {
		return Conversation{}, false, err
	}
	record, found := c.state.Conversations[id]
	return record, found, nil
}

// Entry implements Storage.entry(id) and Storage.entry(conversationId, id).
func (c *storeCore) Entry(ctx context.Context, ids ...ID) (StoredEntry, bool, error) {
	if err := c.enter(ctx); err != nil {
		return StoredEntry{}, false, err
	}
	defer c.leave()
	return lookupStoredEntry(c.state, c.limits, ids)
}
func lookupStoredEntry(state Snapshot, limits Limits, ids []ID) (StoredEntry, bool, error) {
	if len(ids) != 1 && len(ids) != 2 {
		return StoredEntry{}, false, storageRequest("Storage.entry() requires an entry ID")
	}
	if err := checkReadIDs(ids...); err != nil {
		return StoredEntry{}, false, err
	}
	id := ids[len(ids)-1]
	if len(ids) == 2 {
		if _, found := state.Conversations[ids[0]]; !found {
			return StoredEntry{}, false, storageRequest("Unknown conversation: " + idText(ids[0]))
		}
		if !entryVisible(state, ids[0], id) {
			return StoredEntry{}, false, nil
		}
	}
	record, found := state.Entries[id]
	if !found {
		return StoredEntry{}, false, nil
	}
	sequence := record.Seq
	owned, err := copyEntry(record, limits)
	if err != nil {
		return StoredEntry{}, false, err
	}
	owned.Seq, owned.Position = 0, 0
	return StoredEntry{Entry: owned, CommitSeq: sequence}, true, nil
}

func (s *Session) Conversation(ctx context.Context, id ID) (record Conversation, found bool, err error) {
	if err = s.enter(ctx); err != nil {
		return
	}
	outcome := s.jobOutcome
	defer func() {
		s.leave()
		err = s.deliveredError(outcome, err)
		if err != nil {
			record = Conversation{}
			found = false
		}
	}()
	if err = checkReadIDs(id); err != nil {
		return
	}
	if err = s.touch(ctx, conversationKey(id)); err != nil {
		return
	}
	if !s.hasNativeReadImage() {
		return s.guardedStorage().Conversation(ctx, id)
	}
	state, e := s.taskSnapshot(ctx)
	if e != nil {
		err = e
		return
	}
	record, found = state.Conversations[id]
	return
}
func (s *Session) Entry(ctx context.Context, ids ...ID) (record StoredEntry, found bool, err error) {
	if err = s.enter(ctx); err != nil {
		return
	}
	outcome := s.jobOutcome
	defer func() {
		s.leave()
		err = s.deliveredError(outcome, err)
		if err != nil {
			record = StoredEntry{}
			found = false
		}
	}()
	if len(ids) != 1 && len(ids) != 2 {
		return StoredEntry{}, false, storageRequest("Storage.entry() requires an entry ID")
	}
	if err = checkReadIDs(ids...); err != nil {
		return
	}
	if len(ids) == 2 {
		if err = s.touch(ctx, conversationKey(ids[0])); err != nil {
			return
		}
	}
	if err = s.touch(ctx, entryKey(ids[len(ids)-1])); err != nil {
		return
	}
	if !s.hasNativeReadImage() {
		return s.guardedStorage().Entry(ctx, ids...)
	}
	state, e := s.taskSnapshot(ctx)
	if e != nil {
		err = e
		return
	}
	return lookupStoredEntry(state, s.limits, ids)
}

func (t *Tx) Conversation(id ID) (Conversation, bool, error) {
	if err := t.enterTableRead("conversation", conversationKey(id)); err != nil {
		return Conversation{}, false, err
	}
	defer t.leave()
	if err := checkReadIDs(id); err != nil {
		return Conversation{}, false, err
	}
	record, found := t.state.Conversations[id]
	return record, found, nil
}
func (t *Tx) Entry(id ID) (Entry, bool, error) {
	if err := t.enterTableRead("entry", entryKey(id)); err != nil {
		return Entry{}, false, err
	}
	defer t.leave()
	stored, found, err := lookupStoredEntry(t.state, t.limits, []ID{id})
	return stored.Entry, found, err
}
