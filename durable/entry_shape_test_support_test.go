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

// Historical native fixture only, not a public constructor or source JSON
// acceptance adapter. Source constructor tests call the public methods directly.
// entryContentFixture is additive to the legacy object Value surface. Data may be a
// primitive, array or object; HasData distinguishes explicit null from absent.
// Model contributions are strict detached protocol messages, not credentials.
type entryContentFixture struct {
	Value    JSON
	Data     any
	HasData  bool
	Model    []goai.Message
	Edits    []ContextEdit
	Head     ID
	HeadSelf bool
	ByTask   ID
}

func (t *Tx) appendTypedEntryFixture(def *EntryDefinition, conversation ID, c entryContentFixture) (Entry, error) {
	if e := t.enterKeys(conversationKey(conversation)); e != nil {
		return Entry{}, e
	}
	defer t.leave()
	t.hasTableWrite = true
	if def == nil {
		return Entry{}, reject("nil entry definition")
	}
	if err := t.requireConversation(conversation); err != nil {
		return Entry{}, err
	}
	id, e := t.store.mintID(t.ctx, true)
	if e != nil {
		return Entry{}, e
	}
	t.state.HighWater = uint64(id)
	head := c.Head
	if c.HeadSelf {
		if head != 0 {
			return Entry{}, reject("ambiguous entry head")
		}
		head = id
	}
	value := c.Value
	if value == nil {
		value = JSON{}
	}
	model, e := contributionReceipts(c.Model, t.limits)
	if e != nil {
		return Entry{}, e
	}
	v := Entry{ID: id, Conversation: conversation, Kind: def.kind, Value: value, Head: head, Data: c.Data, HasData: c.HasData, Model: model, Edits: c.Edits, ByTask: c.ByTask}
	owned, e := copyEntry(v, t.limits)
	if e != nil {
		return Entry{}, e
	}
	if e = t.stage(Write{Op: "append-entry", Entry: &owned}); e != nil {
		return Entry{}, e
	}
	return copyEntry(owned, t.limits)
}
