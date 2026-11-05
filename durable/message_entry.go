package durable

import goai "github.com/rcarmo/go-ai"

// Original harness transcript kinds (src/entries.ts). Message entries carry
// `model:[message]`; tool results also carry `data:{diagnostics}`.
const (
	userEntryKind       = "pi.user"
	assistantEntryKind  = "pi.assistant"
	toolResultEntryKind = "pi.tool-result"
	legacyMessageKind   = "message" // native stores before the v1.5.0 entry shape
)

func messageEntryKind(role goai.Role) string {
	switch role {
	case goai.RoleAssistant:
		return assistantEntryKind
	case goai.RoleToolResult:
		return toolResultEntryKind
	default:
		return userEntryKind
	}
}

// appendMessage appends one model message as an original-shaped entry.
// Producers still write the legacy native shape: native receipts carry fields
// (errorCode, tagged details) that original `model` messages reject. See
// docs/upstream-v1.5.0/durable-semantic-port.md.
func (t *Tx) appendMessage(id, conversation, by ID, value JSON) error {
	return t.AppendEntry(Entry{ID: id, Conversation: conversation, Kind: legacyMessageKind, Value: value, ByTask: by})
}

func messageEntry(id, conversation, by ID, value JSON, l Limits) (Entry, error) {
	var receipt MessageReceipt
	if err := fromObject(value, &receipt, l); err != nil {
		return Entry{}, err
	}
	entry := Entry{ID: id, Conversation: conversation, Kind: messageEntryKind(receipt.Role), Value: JSON{}, Model: []MessageReceipt{receipt}, ByTask: by}
	if receipt.Role == goai.RoleToolResult {
		diagnostics := []any{}
		for _, d := range receipt.Diagnostics {
			object, err := dtoObject(d, l)
			if err != nil {
				return Entry{}, err
			}
			diagnostics = append(diagnostics, map[string]any(object))
		}
		entry.Data, entry.HasData = map[string]any{"diagnostics": diagnostics}, true
	}
	return entry, nil
}

// isMessageEntry reports whether an entry is a harness-produced single model
// message, in either the original or the legacy native shape.
func isMessageEntry(entry Entry) bool {
	switch entry.Kind {
	case legacyMessageKind:
		return true
	case userEntryKind, assistantEntryKind, toolResultEntryKind:
		return len(entry.Model) == 1
	}
	return false
}

// entryMessage returns the single model message of a harness message entry.
func entryMessage(entry Entry, l Limits) (MessageReceipt, error) {
	if entry.Kind != legacyMessageKind {
		if len(entry.Model) != 1 {
			return MessageReceipt{}, reject("entry has no single model message")
		}
		copied, err := copyMessages(entry.Model, l)
		if err != nil {
			return MessageReceipt{}, err
		}
		return copied[0], nil
	}
	var receipt MessageReceipt
	err := fromObject(entry.Value, &receipt, l)
	return receipt, err
}

// entryMessageValue is entryMessage in the JSON-object form used by
// submission answers and checkpoints.
func entryMessageValue(entry Entry, l Limits) (JSON, error) {
	if entry.Kind == legacyMessageKind {
		return copyObject(entry.Value, l)
	}
	receipt, err := entryMessage(entry, l)
	if err != nil {
		return nil, err
	}
	return dtoObject(receipt, l)
}

// entryValueEquals compares a message entry with a stored message object.
func entryValueEquals(entry Entry, value JSON, l Limits) bool {
	own, err := entryMessageValue(entry, l)
	return err == nil && equalTaskValue(&TaskValue{Present: true, Value: own}, &TaskValue{Present: true, Value: value}, l)
}

// mustEntryValue returns the message object, or nil for non-message entries.
func mustEntryValue(entry Entry, l Limits) JSON {
	value, err := entryMessageValue(entry, l)
	if err != nil {
		return nil
	}
	return value
}
