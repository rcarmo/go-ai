package durable

import goai "github.com/rcarmo/go-ai"

// Test-only adapter. Producers write the upstream entry shape (pi.user /
// pi.assistant / pi.tool-result with model:[message]); tests written against the
// legacy `value` payload project through here so their assertions stay about
// behaviour rather than which envelope carried it. Production never calls this.
// legacyEntryValue projects the payload a test asserts on. Application-kind and
// metadata entries keep their object payload in `value`; harness message entries
// store it in `model:[message]`. Production never calls this.
func legacyEntryValue(entry Entry) JSON {
	if len(entry.Value) > 0 {
		return entry.Value
	}
	if len(entry.Model) == 1 {
		if obj, err := dtoObject(entry.Model[0], maximumDurableLimits()); err == nil {
			return obj
		}
	}
	return JSON{}
}

func decodeEntryMessage(entry Entry, l Limits, target *MessageReceipt) error {
	message, err := entryMessage(entry, l)
	if err != nil {
		return err
	}
	*target = message
	return nil
}

// decodeDocumentValue decodes a document's stored object payload. Distinct from
// decodeEntryMessage so tests keep the record kind explicit.
func decodeDocumentValue(doc Document, l Limits, target any) error {
	return fromObject(doc.Value, target, l)
}

// messageEntryValue is the stored message payload of a harness message entry in
// wire form. Entries carry `model:[message]`; the legacy `value` bag is absent.
func messageEntryValue(entry Entry) JSON {
	value, err := entryMessageValue(entry, maximumDurableLimits())
	if err != nil {
		return nil
	}
	return value
}

// opItemIsUserMessage reports whether a persisted entry operation item is the
// harness user message, in the upstream shape (kind `pi.user` with the role in
// `model:[message]`) or the legacy native shape (kind `message` with the role
// in `value`). Tests that assert on an operation tuple compare against an
// independently rebuilt operation, so they inspect the raw item map.
func opItemIsUserMessage(item map[string]any) bool {
	if item["kind"] == userEntryKind {
		model, _ := item["model"].([]any)
		if len(model) == 1 {
			message, _ := model[0].(map[string]any)
			return message["role"] == string(goai.RoleUser)
		}
		return false
	}
	if item["kind"] == legacyMessageKind {
		value, _ := item["value"].(map[string]any)
		return value["role"] == string(goai.RoleUser)
	}
	return false
}
