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
func inputMetadataEntry(metadata *Entry, id, conversation ID, message JSON, receipt MessageReceipt, by ID) Entry {
	if metadata == nil {
		return Entry{ID: id, Conversation: conversation, Kind: "message", Value: message, ByTask: by}
	}
	return Entry{ID: id, Conversation: conversation, Kind: metadata.Kind, Value: JSON{}, Data: metadata.Data, HasData: metadata.HasData, Model: []MessageReceipt{receipt}, ByTask: by}
}
