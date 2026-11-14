package durable

import (
	"fmt"
)

func appendPassiveWrite(tx *Tx, conversation ID, value JSON) error {
	id, err := tx.MintID()
	if err != nil {
		return err
	}
	draft := Entry{ID: id, Conversation: conversation, Kind: userEntryKind}
	if raw, ok := value["entry"].(map[string]any); ok {
		// Native submission drafts use the stored envelope, not the public codec.
		stored := storedEntry(draft)
		if err := fromObject(JSON(raw), &stored, tx.limits); err != nil {
			return err
		}
		draft = Entry(stored)
		draft.ID, draft.Conversation, draft.Seq, draft.Position = id, conversation, 0, 0
	} else if message, ok := value["inputMessage"].(map[string]any); ok {
		var mr MessageReceipt
		if err = fromObject(JSON(message), &mr, tx.limits); err != nil {
			return err
		}
		draft.Model = []MessageReceipt{mr}
	} else {
		text, _ := value["content"].(string)
		draft.Model = []MessageReceipt{userReceipt(text)}
	}
	if err := tx.appendEntryRecord(draft); err != nil {
		return err
	}
	value["placedEntry"] = id
	return nil
}
func (h *Harness) placeQueuedWrites(tx *Tx, conversation ID) error {
	_, err := h.placeBoundaryWrites(tx, conversation, false)
	return err
}

func admitCompactionWrite(tx *Tx, conversation, task ID, draft Entry) (ID, error) {
	request := fmt.Sprintf("compaction:%d", task)
	for _, sub := range tx.state.Submissions {
		if sub.Conversation == conversation && sub.RequestID == request {
			return sub.ID, nil
		}
	}
	wire, err := dtoObject(draft, tx.limits)
	if err != nil {
		return 0, err
	}
	id, err := tx.MintID()
	if err != nil {
		return 0, err
	}
	value := JSON{"entry": map[string]any(wire)}
	busy := false
	for _, work := range tx.state.Tasks {
		if work.Conversation == conversation && work.Kind == "pi.generation" && !terminalStatus(work.Status) {
			busy = true
			break
		}
	}
	status := "done"
	if busy {
		status = "pending"
	}
	sub := Submission{ID: id, Conversation: conversation, RequestID: request, Type: "write", Status: status, Value: value}
	if err = tx.PutSubmission(sub); err != nil {
		return 0, err
	}
	if busy {
		entry, draftErr := inboxEntryDraft(draft, tx.limits)
		if draftErr != nil {
			return 0, draftErr
		}
		inbox, inboxErr := builtin(tx, conversation, "pi.inbox")
		if inboxErr != nil {
			return 0, inboxErr
		}
		err = inbox.Update(func(value JSON) error {
			items, ok := value["items"].([]any)
			if !ok {
				return reject("inbox shape")
			}
			value["items"] = append(items, JSON{"id": id, "mode": "write", "entry": entry})
			return nil
		})
	} else {
		state, e := tx.current()
		if e != nil {
			return 0, e
		}
		stale, e := passiveHeadStale(state, conversation, value, tx.limits)
		if e != nil {
			return 0, e
		}
		if stale {
			sub.Status = "failed"
			value["errorCode"] = "stale"
			err = tx.PutSubmission(sub)
		} else {
			err = appendPassiveWrite(tx, conversation, value)
			if err == nil {
				sub.Value = value
				err = tx.PutSubmission(sub)
			}
		}
	}
	return id, err
}

func passiveHeadStale(state Snapshot, conversation ID, value JSON, limits Limits) (bool, error) {
	raw, ok := value["entry"].(map[string]any)
	if !ok {
		return false, nil
	}
	// Native submission drafts use the stored envelope, not the public codec.
	var draft storedEntry
	if err := fromObject(JSON(raw), &draft, limits); err != nil {
		return false, err
	}
	if draft.Head == 0 {
		return false, nil
	}
	view, err := deriveContextView(state, conversation, 0, limits)
	if err != nil {
		return false, err
	}
	return view.Head != nil && draft.Head < view.Head.Head, nil
}

// inboxEntryDraft stores the canonical EntryDraft, not the private submission
// envelope. The public record codec supplies model/data/head/edits JSON; placement
// identity and task attribution are assigned only when the draft is appended.
func inboxEntryDraft(entry Entry, limits Limits) (JSON, error) {
	wire, err := entry.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var draft JSON
	if err = decodeStrict(wire, limits, limits.MaxRecordBytes, &draft); err != nil {
		return nil, err
	}
	for _, key := range []string{"id", "conversationId", "byTaskId", "value"} {
		delete(draft, key)
	}
	return draft, nil
}

// appendInboxEntry is the EntryDraft append in pinned inbox.ts applyBoundary.
// The draft is canonical JSON. Resolve head:"self" only after assigning its ID,
// then use the public Entry codec rather than the native stored-entry envelope.
func appendInboxEntry(tx *Tx, conversation ID, draft JSON) (ID, error) {
	entry, err := tx.AppendEntry(conversation, draft)
	return entry.ID, err
}
