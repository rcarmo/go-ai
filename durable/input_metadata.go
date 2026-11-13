package durable

import "strings"

func validateInputMetadata(entry *Entry) error {
	if entry.Kind == "" || strings.HasPrefix(entry.Kind, "pi.") {
		return reject("input entry kind must be non-empty and not start with pi.")
	}
	if entry.ID != 0 || entry.Conversation != 0 || entry.Seq != 0 || entry.Position != 0 || entry.Head != 0 || entry.Model != nil || entry.Edits != nil || entry.ByTask != 0 || entry.Value != nil {
		return reject("input entry accepts only kind and data")
	}
	return nil
}
func inputMetadataEntry(metadata *storedEntry, id, conversation ID, message JSON, receipt MessageReceipt, by ID) Entry {
	if metadata == nil {
		return Entry{ID: id, Conversation: conversation, Kind: userEntryKind, Model: []MessageReceipt{receipt}, ByTask: by}
	}
	return Entry{ID: id, Conversation: conversation, Kind: metadata.Kind, Value: JSON{}, Data: metadata.Data, HasData: metadata.HasData, Model: []MessageReceipt{receipt}, ByTask: by}
}

// appendQueuedInput follows inbox.ts appendInput: the Inbox item, not the
// historical scheduler checkpoint, owns both content and picked entry metadata.
func appendQueuedInput(tx *Tx, conversation, submission ID, now int64, by ID) (ID, error) {
	inbox, err := builtin(tx, conversation, "pi.inbox")
	if err != nil {
		return 0, err
	}
	value, err := inbox.Get()
	if err != nil {
		return 0, err
	}
	items, ok := value["items"].([]any)
	if !ok {
		return 0, reject("inbox shape")
	}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			return 0, reject("inbox item")
		}
		if submissionEntryID(item["id"]) != submission || (item["mode"] != "steer" && item["mode"] != "followUp") {
			continue
		}
		content, exists := item["content"]
		if !exists {
			return 0, reject("queued input content is missing")
		}
		var receipt MessageReceipt
		if err := fromObject(JSON{"role": "user", "content": content, "timestamp": now}, &receipt, tx.limits); err != nil {
			return 0, err
		}
		var metadata *storedEntry
		if raw, exists := item["entry"]; exists {
			entry, ok := raw.(map[string]any)
			if !ok {
				return 0, reject("input entry shape")
			}
			kind, ok := entry["kind"].(string)
			if !ok {
				return 0, reject("input entry kind")
			}
			metadata = &storedEntry{Kind: kind}
			metadata.Data, metadata.HasData = entry["data"]
		}
		id, err := tx.MintID()
		if err != nil {
			return 0, err
		}
		if err := tx.AppendEntry(inputMetadataEntry(metadata, id, conversation, nil, receipt, by)); err != nil {
			return 0, err
		}
		return id, nil
	}
	return 0, reject("queued input does not exist")
}
