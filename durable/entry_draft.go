package durable

import "fmt"

// EntryDraft is the source JSON object supplied before identity and task
// attribution. Omit optional keys to omit fields; data:nil is explicit null.
// head accepts an entry ID or "self". The Session assigns id/conversationId
// and byTaskId, overriding any such properties supplied by an untyped caller.
type EntryDraft = JSON

// AppendEntry mirrors Transaction.#appendEntry at the pinned source: require
// the conversation, mint identity, copy strict JSON with scoped attribution,
// stage the record, and return the complete record to the callback.
func (t *Tx) AppendEntry(conversation ID, draft EntryDraft) (Entry, error) {
	if err := t.enterKeys(conversationKey(conversation)); err != nil {
		return Entry{}, err
	}
	defer t.leave()
	t.hasTableWrite = true
	if err := t.requireConversation(conversation); err != nil {
		return Entry{}, reject(fmt.Sprintf("Conversation %d does not exist", conversation))
	}
	id, err := t.store.mintID(t.ctx, true)
	if err != nil {
		return Entry{}, err
	}
	t.state.HighWater = uint64(id)
	// Apply the source's object spread/overrides before strict JSON copying:
	// overwritten caller properties must not be validated or acquire authority.
	record := make(JSON, len(draft)+3)
	for key, value := range draft {
		record[key] = value
	}
	record["id"], record["conversationId"] = id, conversation
	delete(record, "byTaskId")
	if t.byTask != 0 {
		record["byTaskId"] = t.byTask
	}
	if head, ok := record["head"].(string); ok && head == "self" {
		record["head"] = id
	}
	owned, err := copyObject(record, t.limits)
	if err != nil {
		return Entry{}, err
	}
	wire, err := encodeBounded(owned, t.limits, t.limits.MaxRecordBytes)
	if err != nil {
		return Entry{}, err
	}
	var entry Entry
	if err = entry.UnmarshalJSON(wire); err != nil {
		return Entry{}, err
	}
	if err = t.stage(Write{Op: "append-entry", Entry: &entry}); err != nil {
		return Entry{}, err
	}
	return copyEntry(entry, t.limits)
}

// TypedEntryDraft omits kind: AppendTypedEntry supplies it from the token.
// An untyped caller's kind is overridden, matching the source overload.
type TypedEntryDraft = JSON

func (t *Tx) AppendTypedEntry(token *EntryDefinition, conversation ID, draft TypedEntryDraft) (Entry, error) {
	if token == nil {
		return Entry{}, reject("nil entry definition")
	}
	record := make(JSON, len(draft)+1)
	for key, value := range draft {
		record[key] = value
	}
	record["kind"] = token.kind
	return t.AppendEntry(conversation, record)
}
