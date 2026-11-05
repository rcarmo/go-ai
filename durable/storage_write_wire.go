package durable

import (
	"encoding/json"
)

// DocumentCreateScope is the tagged owner of an original document record.
// Session documents have neither ID; other scopes have exactly their named ID.
type DocumentCreateScope struct {
	Kind           string `json:"kind"`
	ConversationID ID     `json:"conversationId,omitempty"`
	TaskID         ID     `json:"taskId,omitempty"`
}

// DocumentCreate omits storage-owned lifecycle stamps and materialised content.
// A non-nil Key distinguishes a family member with an empty key from a singleton.
type DocumentCreate struct {
	ID      ID                  `json:"id"`
	Kind    string              `json:"kind"`
	Key     *string             `json:"key,omitempty"`
	Scope   DocumentCreateScope `json:"scope"`
	History string              `json:"history,omitempty"`
	Fork    string              `json:"fork,omitempty"`
}

// DocumentContent is either a full base or a same-version delta. Value is set
// only for a base; Ops is non-nil (possibly empty) only for a delta.
type DocumentContent struct {
	Version uint64      `json:"version"`
	Kind    string      `json:"kind"`
	Value   JSON        `json:"value,omitempty"`
	Ops     []Operation `json:"ops,omitempty"`
}

// StorageWrite is the original public storage command union. Value must be a
// Conversation, Entry, TaskRecord or SubmissionRecord (or a pointer to one),
// matching Type. Document commands use Record, Content, Source and ID instead.
// The private Write envelope remains unchanged in SQLite frames/checkpoints.
type StorageWrite struct {
	Type    string
	Value   any
	Record  *DocumentCreate
	Content *DocumentContent
	Source  *DocumentCopySource
	ID      ID
}

func (r DocumentCreate) validate() error {
	if r.ID == 0 || r.Kind == "" {
		return reject("document create identity")
	}
	switch r.Scope.Kind {
	case "session":
		if r.Scope.ConversationID != 0 || r.Scope.TaskID != 0 || r.History != "" || r.Fork != "" {
			return reject("session document scope")
		}
	case "task":
		if r.Scope.TaskID == 0 || r.Scope.ConversationID != 0 || r.History != "" || r.Fork != "" {
			return reject("task document scope")
		}
	case "conversation":
		if r.Scope.ConversationID == 0 || r.Scope.TaskID != 0 {
			return reject("conversation document scope")
		}
		if r.History != "latest" && r.History != "rewindable" {
			return reject("document history")
		}
		if r.Fork != "initial" && r.Fork != "current" && !(r.Fork == "asOf" && r.History == "rewindable") {
			return reject("document fork")
		}
	default:
		return reject("document scope")
	}
	return nil
}

func (c DocumentContent) wire() (map[string]any, error) {
	if c.Version == 0 {
		return nil, reject("document content version")
	}
	wire := map[string]any{"version": c.Version, "kind": c.Kind}
	switch c.Kind {
	case "base":
		if c.Value == nil || c.Ops != nil {
			return nil, reject("document base union")
		}
		wire["value"] = c.Value
	case "delta":
		if c.Value != nil || c.Ops == nil {
			return nil, reject("document delta union")
		}
		wire["ops"] = c.Ops
	default:
		return nil, reject("document content kind")
	}
	return wire, nil
}

func (w StorageWrite) MarshalJSON() ([]byte, error) {
	var out orderedJSON
	out.field("type", w.Type)
	recordValue := w.Type == "conversation" || w.Type == "entry" || w.Type == "task" || w.Type == "submission"
	if recordValue {
		if w.Record != nil || w.Content != nil || w.Source != nil || w.ID != 0 || w.Value == nil {
			return nil, reject("storage record union")
		}
		var data []byte
		var err error
		// Do not invoke caller-supplied marshal authority through the union.
		switch v := w.Value.(type) {
		case Conversation:
			if w.Type == "conversation" {
				data, err = v.MarshalJSON()
			}
		case *Conversation:
			if w.Type == "conversation" && v != nil {
				data, err = v.MarshalJSON()
			}
		case Entry:
			if w.Type == "entry" {
				data, err = v.MarshalJSON()
			}
		case *Entry:
			if w.Type == "entry" && v != nil {
				data, err = v.MarshalJSON()
			}
		case TaskRecord:
			if w.Type == "task" {
				data, err = v.MarshalJSON()
			}
		case *TaskRecord:
			if w.Type == "task" && v != nil {
				data, err = v.MarshalJSON()
			}
		case SubmissionRecord:
			if w.Type == "submission" {
				data, err = v.MarshalJSON()
			}
		case *SubmissionRecord:
			if w.Type == "submission" && v != nil {
				data, err = v.MarshalJSON()
			}
		}
		if err != nil {
			return nil, err
		}
		if data == nil {
			return nil, reject("storage record value type")
		}
		out.raw("value", data)
	} else {
		if w.Value != nil {
			return nil, reject("storage document union")
		}
		switch w.Type {
		case "document.create", "document.copy":
			if w.ID != 0 || w.Record == nil {
				return nil, reject("document creation union")
			}
			if err := w.Record.validate(); err != nil {
				return nil, err
			}
			out.field("record", *w.Record)
			if w.Type == "document.create" {
				if w.Source != nil || w.Content == nil || w.Content.Kind != "base" {
					return nil, reject("document creation content")
				}
			} else {
				if w.Content != nil || w.Source == nil || w.Source.ID == 0 {
					return nil, reject("document copy source")
				}
				var point any = w.Source.At.Seq
				if w.Source.At.Current {
					if w.Source.At.Seq != 0 {
						return nil, reject("document copy point")
					}
					point = "current"
				}
				out.field("source", map[string]any{"id": w.Source.ID, "at": point})
			}
		case "document.change", "document.retire":
			if w.ID == 0 || w.Record != nil || w.Source != nil {
				return nil, reject("document mutation union")
			}
			if (w.Type == "document.change") != (w.Content != nil) {
				return nil, reject("document mutation content")
			}
			out.field("id", w.ID)
		default:
			return nil, reject("storage write type")
		}
		if w.Content != nil {
			content, err := w.Content.wire()
			if err != nil {
				return nil, err
			}
			out.field("content", content)
		}
	}
	data, err := out.bytes()
	if err == nil && len(data) > maximumDurableLimits().MaxRecordBytes {
		return nil, reject("storage write byte limit")
	}
	return data, err
}

func (w *StorageWrite) UnmarshalJSON(data []byte) error {
	var envelope map[string]json.RawMessage
	if err := decodePublicJSON(data, &envelope); err != nil {
		return err
	}
	var result StorageWrite
	if err := decodePublicJSON(envelope["type"], &result.Type); err != nil {
		return err
	}
	fields := []string{"type"}
	switch result.Type {
	case "conversation":
		result.Value = new(Conversation)
		fields = append(fields, "value")
	case "entry":
		result.Value = new(Entry)
		fields = append(fields, "value")
	case "task":
		result.Value = new(TaskRecord)
		fields = append(fields, "value")
	case "submission":
		result.Value = new(SubmissionRecord)
		fields = append(fields, "value")
	case "document.create":
		fields = append(fields, "record", "content")
	case "document.copy":
		fields = append(fields, "record", "source")
	case "document.change":
		fields = append(fields, "id", "content")
	case "document.retire":
		fields = append(fields, "id")
	default:
		return reject("storage write type")
	}
	if len(envelope) != len(fields) {
		return reject("storage write fields")
	}
	for _, name := range fields {
		raw, ok := envelope[name]
		if !ok || string(raw) == "null" {
			return reject("storage write " + name + " absent")
		}
	}
	if result.Value != nil {
		if err := decodePublicJSON(envelope["value"], result.Value); err != nil {
			return err
		}
	}
	if raw, ok := envelope["record"]; ok {
		result.Record = new(DocumentCreate)
		if err := decodePublicJSON(raw, result.Record); err != nil {
			return err
		}
	}
	if raw, ok := envelope["id"]; ok {
		if err := decodePublicJSON(raw, &result.ID); err != nil {
			return err
		}
	}
	if raw, ok := envelope["content"]; ok {
		result.Content = new(DocumentContent)
		if err := decodePublicJSON(raw, result.Content); err != nil {
			return err
		}
		var members map[string]json.RawMessage
		if err := decodePublicJSON(raw, &members); err != nil {
			return err
		}
		if len(members) != 3 {
			return reject("document content fields")
		}
	}
	if raw, ok := envelope["source"]; ok {
		var source struct {
			ID ID              `json:"id"`
			At json.RawMessage `json:"at"`
		}
		if err := decodePublicJSON(raw, &source); err != nil {
			return err
		}
		result.Source = &DocumentCopySource{ID: source.ID}
		if string(source.At) == `"current"` {
			result.Source.At.Current = true
		} else {
			if len(source.At) == 0 || string(source.At) == "null" {
				return reject("document copy point absent")
			}
			if err := decodePublicJSON(source.At, &result.Source.At.Seq); err != nil {
				return err
			}
		}
	}
	// Reuse the same union validation for Go-built and decoded commands; it also
	// enforces the native JSON ownership/resource limits on nested values.
	if _, err := result.MarshalJSON(); err != nil {
		return err
	}
	*w = result
	return nil
}
