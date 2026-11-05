package durable

import (
	"bytes"
	"encoding/json"
	"io"
)

// Conversation and Entry JSON follows pi-durable v1.5.0 ConversationRecord and
// EntryRecord. SQLite frames and checkpoints keep the historical native
// envelope: Write and storageCheckpoint decode through method-free mirrors and
// the native encoder (isNativeJSONType) never invokes these public codecs.

// orderedJSON emits members in the original record's declared key order;
// map iteration order would make encodings nondeterministic.
type orderedJSON struct {
	buf []byte
	err error
}

func (o *orderedJSON) field(name string, value any) {
	if o.err != nil {
		return
	}
	// Canonical (sorted) map keys keep public encodings deterministic, as
	// encoding/json did before this codec existed.
	encoded, err := encodeBoundedOrder(map[string]any{name: value}, maximumDurableLimits(), maximumDurableLimits().MaxRecordBytes, true)
	if err != nil {
		o.err = err
		return
	}
	member := encoded[1 : len(encoded)-1]
	if len(o.buf) == 0 {
		o.buf = append(o.buf, '{')
	} else {
		o.buf = append(o.buf, ',')
	}
	o.buf = append(o.buf, member...)
}

// raw appends an already-encoded member value produced by orderedJSON.
func (o *orderedJSON) raw(name string, value []byte) {
	if o.err != nil {
		return
	}
	key, err := encodeBounded(map[string]any{name: nil}, maximumDurableLimits(), maximumDurableLimits().MaxRecordBytes)
	if err != nil {
		o.err = err
		return
	}
	if len(o.buf) == 0 {
		o.buf = append(o.buf, '{')
	} else {
		o.buf = append(o.buf, ',')
	}
	o.buf = append(o.buf, key[1:len(key)-len("null}")]...)
	o.buf = append(o.buf, value...)
}
func (o *orderedJSON) bytes() ([]byte, error) {
	if o.err != nil {
		return nil, o.err
	}
	if len(o.buf) == 0 {
		return []byte("{}"), nil
	}
	return append(o.buf, '}'), nil
}

func (v Conversation) MarshalJSON() ([]byte, error) {
	var out orderedJSON
	out.field("id", v.ID)
	if v.Parent != 0 {
		var parent orderedJSON
		parent.field("conversationId", v.Parent)
		parent.field("at", v.ParentAt)
		raw, err := parent.bytes()
		if err != nil {
			return nil, err
		}
		out.raw("parent", raw)
	}
	if v.Owner != 0 {
		if v.OwnerConversation == 0 {
			return nil, reject("conversation owner conversation is unknown")
		}
		var owner orderedJSON
		owner.field("conversationId", v.OwnerConversation)
		owner.field("taskId", v.Owner)
		raw, err := owner.bytes()
		if err != nil {
			return nil, err
		}
		out.raw("owner", raw)
	}
	return out.bytes()
}

func (v *Conversation) UnmarshalJSON(data []byte) error {
	var wire struct {
		ID     *ID `json:"id"`
		Parent *struct {
			Conversation *ID `json:"conversationId"`
			At           *ID `json:"at"`
		} `json:"parent"`
		Owner *struct {
			Conversation *ID `json:"conversationId"`
			Task         *ID `json:"taskId"`
		} `json:"owner"`
	}
	if err := decodePublicJSON(data, &wire); err != nil {
		return err
	}
	if wire.ID == nil {
		return reject("conversation id absent")
	}
	result := Conversation{ID: *wire.ID}
	if wire.Parent != nil {
		if wire.Parent.Conversation == nil || wire.Parent.At == nil {
			return reject("conversation parent incomplete")
		}
		result.Parent, result.ParentAt = *wire.Parent.Conversation, *wire.Parent.At
	}
	if wire.Owner != nil {
		if wire.Owner.Conversation == nil || wire.Owner.Task == nil {
			return reject("conversation owner incomplete")
		}
		result.OwnerConversation, result.Owner = *wire.Owner.Conversation, *wire.Owner.Task
	}
	*v = result
	return nil
}

func (v Entry) MarshalJSON() ([]byte, error) {
	var out orderedJSON
	out.field("id", v.ID)
	out.field("conversationId", v.Conversation)
	out.field("kind", v.Kind)
	if v.Model != nil {
		out.field("model", v.Model)
	}
	if v.HasData {
		out.field("data", v.Data)
	} else if v.Data != nil {
		return nil, reject("entry data requires HasData")
	}
	if v.Head != 0 {
		out.field("head", v.Head)
	}
	if v.Edits != nil {
		out.field("edits", v.Edits)
	}
	if v.ByTask != 0 {
		out.field("byTaskId", v.ByTask)
	}
	// Native message entries keep their payload in Value; the original has no
	// such field. It is emitted only when non-empty (see the semantic-port note).
	if len(v.Value) > 0 {
		out.field("value", v.Value)
	}
	return out.bytes()
}

func (v *Entry) UnmarshalJSON(data []byte) error {
	var wire struct {
		ID           *ID              `json:"id"`
		Conversation *ID              `json:"conversationId"`
		Kind         *string          `json:"kind"`
		Model        []MessageReceipt `json:"model"`
		Data         json.RawMessage  `json:"data"`
		Head         ID               `json:"head"`
		Edits        []ContextEdit    `json:"edits"`
		ByTask       ID               `json:"byTaskId"`
		Value        JSON             `json:"value"`
	}
	limits := maximumDurableLimits()
	if err := decodeStrict(data, limits, limits.MaxRecordBytes, &wire); err != nil {
		return err
	}
	if wire.ID == nil || wire.Conversation == nil || wire.Kind == nil {
		return reject("entry id, conversationId or kind absent")
	}
	result := Entry{ID: *wire.ID, Conversation: *wire.Conversation, Kind: *wire.Kind, Model: wire.Model, Head: wire.Head, Edits: wire.Edits, ByTask: wire.ByTask, Value: wire.Value}
	if result.Value == nil {
		result.Value = JSON{}
	}
	if len(wire.Data) > 0 {
		var value any
		if err := decodePublicJSON(wire.Data, &value); err != nil {
			return err
		}
		result.Data, result.HasData = value, true
	}
	owned, err := copyEntry(result, limits)
	if err != nil {
		return err
	}
	*v = owned
	return nil
}

// Method-free storage mirrors: identical fields and tags, no public codec.
type storedConversation Conversation
type storedEntry Entry

func decodeStoredJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return reject("trailing stored data")
	}
	return nil
}

func (w *Write) UnmarshalJSON(data []byte) error {
	var wire struct {
		Op           string              `json:"op"`
		Conversation *storedConversation `json:"conversation,omitempty"`
		Entry        *storedEntry        `json:"entry,omitempty"`
		Task         *Task               `json:"task,omitempty"`
		Submission   *Submission         `json:"submission,omitempty"`
		Document     *Document           `json:"document,omitempty"`
		Source       *DocumentCopySource `json:"source,omitempty"`
		Delta        *DocumentDelta      `json:"delta,omitempty"`
	}
	if err := decodeStoredJSON(data, &wire); err != nil {
		return err
	}
	*w = Write{Op: wire.Op, Conversation: (*Conversation)(wire.Conversation), Entry: (*Entry)(wire.Entry), Task: wire.Task, Submission: wire.Submission, Document: wire.Document, Source: wire.Source, Delta: wire.Delta}
	return nil
}

func (c *storageCheckpoint) UnmarshalJSON(data []byte) error {
	var wire struct {
		Seq           uint64                `json:"seq"`
		HighWater     uint64                `json:"highWater"`
		Ordinal       uint64                `json:"ordinal"`
		Conversations []storedConversation  `json:"conversations"`
		Entries       []storedEntry         `json:"entries"`
		Tasks         []Task                `json:"tasks"`
		Submissions   []Submission          `json:"submissions"`
		Documents     []Document            `json:"documents"`
		Revisions     []checkpointRevisions `json:"revisions"`
	}
	if err := decodeStoredJSON(data, &wire); err != nil {
		return err
	}
	result := storageCheckpoint{Seq: wire.Seq, HighWater: wire.HighWater, Ordinal: wire.Ordinal, Tasks: wire.Tasks, Submissions: wire.Submissions, Documents: wire.Documents, Revisions: wire.Revisions}
	for _, v := range wire.Conversations {
		result.Conversations = append(result.Conversations, Conversation(v))
	}
	for _, v := range wire.Entries {
		result.Entries = append(result.Entries, Entry(v))
	}
	*c = result
	return nil
}

// deriveConversationOwners fills the non-persisted owner conversation from
// the owner task's immutable conversation, as original Tx.createConversation does.
func deriveConversationOwners(s *Snapshot, only map[ID]bool) {
	for id, v := range s.Conversations {
		if v.Owner == 0 || (only != nil && !only[id]) {
			continue
		}
		if task, ok := s.Tasks[v.Owner]; ok {
			v.OwnerConversation = task.Conversation
			s.Conversations[id] = v
		}
	}
}
