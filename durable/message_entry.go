package durable

import (
	goai "github.com/rcarmo/go-ai"
)

// Original harness transcript kinds (src/entries.ts). Message entries carry
// `model:[message]`; tool results also carry `data:{diagnostics}`.
const (
	userEntryKind       = "pi.user"
	assistantEntryKind  = "pi.assistant"
	toolResultEntryKind = "pi.tool-result"
	legacyMessageKind   = "message" // native stores before the v1.5.0 entry shape
)

// appendMessage appends one model message as the original harness does: kind
// `pi.user`/`pi.assistant`/`pi.tool-result` with `model:[message]`, and tool
// results additionally carry `data:{diagnostics}`. A payload that is not a
// single model message keeps the legacy native `message` shape.
func (t *Tx) appendMessage(id, conversation, by ID, value JSON) error {
	var receipt MessageReceipt
	if err := fromObject(value, &receipt, t.limits); err != nil {
		return t.appendEntryRecord(Entry{ID: id, Conversation: conversation, Kind: legacyMessageKind, Value: value, ByTask: by})
	}
	entry := Entry{ID: id, Conversation: conversation, Model: []MessageReceipt{receipt}, ByTask: by}
	switch receipt.Role {
	case goai.RoleUser:
		entry.Kind = userEntryKind
	case goai.RoleAssistant:
		entry.Kind = assistantEntryKind
	case "toolResult":
		entry.Kind = toolResultEntryKind
		data, err := toolResultEntryData(value, t.limits)
		if err != nil {
			return err
		}
		entry.Data = data
		entry.HasData = true
	default:
		return t.appendEntryRecord(Entry{ID: id, Conversation: conversation, Kind: legacyMessageKind, Value: value, ByTask: by})
	}
	return t.appendEntryRecord(entry)
}

// toolResultEntryData is the original `data` payload of a tool-result entry:
// `{diagnostics:[...]}`, which upstream notes may hold none. The caller hands
// over the wire-projected receipt, so its diagnostics are taken as they are
// persisted: raw argument-error text lives in the receipt's UTF-16 witness, and
// reading the in-memory structs instead would put text into entry data that the
// stored payload itself does not carry.
func toolResultEntryData(value JSON, l Limits) (JSON, error) {
	items, present := value["diagnostics"]
	if !present || items == nil {
		items = []any{}
	}
	if _, ok := items.([]any); !ok {
		return nil, reject("tool result diagnostics")
	}
	data := JSON{"diagnostics": items}
	if _, err := ownJSONValue(data, l); err != nil {
		return nil, err
	}
	return data, nil
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
