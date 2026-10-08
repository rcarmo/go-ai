package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
)

// EntryDefinition narrows by kind only; it owns no executable callback or schema.
type EntryDefinition struct{ kind string }

func DefineEntry(kind string) (*EntryDefinition, error) {
	if !validKind(kind) {
		return nil, reject("invalid entry kind")
	}
	return &EntryDefinition{kind}, nil
}
func (d *EntryDefinition) Is(entry Entry) bool { return d != nil && d.kind == entry.Kind }

// EntryContent is additive to the legacy object Value surface. Data may be a
// primitive, array or object; HasData distinguishes explicit null from absent.
// Model contributions are strict detached protocol messages, not credentials.
type EntryContent struct {
	Value    JSON
	Data     any
	HasData  bool
	Model    []goai.Message
	Edits    []ContextEdit
	Head     ID
	HeadSelf bool
	ByTask   ID
}
type ContextEdit struct {
	Target   ID               `json:"target"`
	Action   string           `json:"action"` // omit or replace
	Messages []MessageReceipt `json:"messages,omitempty"`
}

func (t *Tx) AppendTypedEntry(def *EntryDefinition, conversation ID, c EntryContent) (Entry, error) {
	if e := t.enter(); e != nil {
		return Entry{}, e
	}
	defer t.leave()
	if def == nil {
		return Entry{}, reject("nil entry definition")
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

// TypedEntry reads the latest transaction overlay through an entry token.
func (t *Tx) TypedEntry(def *EntryDefinition, conversation, id ID) (Entry, bool, error) {
	if err := t.enter(); err != nil {
		return Entry{}, false, err
	}
	defer t.leave()
	if def == nil {
		return Entry{}, false, reject("nil entry definition")
	}
	state, err := t.current()
	if err != nil {
		return Entry{}, false, err
	}
	entry, found := state.Entries[id]
	if !found || !entryVisible(state, conversation, id) || !def.Is(entry) {
		return Entry{}, false, nil
	}
	owned, err := copyEntry(entry, t.limits)
	return owned, err == nil, err
}
func (s *Session) TypedEntry(ctx context.Context, def *EntryDefinition, conversation, id ID) (Entry, bool, error) {
	if e := s.enter(ctx); e != nil {
		return Entry{}, false, e
	}
	defer s.leave()
	if def == nil {
		return Entry{}, false, reject("nil entry definition")
	}
	state, e := s.store.Snapshot(ctx)
	if e != nil {
		return Entry{}, false, e
	}
	if !entryVisible(state, conversation, id) {
		return Entry{}, false, nil
	}
	v := state.Entries[id]
	if !def.Is(v) {
		return Entry{}, false, nil
	}
	owned, e := copyEntry(v, s.limits)
	return owned, e == nil, e
}
func contributionReceipts(messages []goai.Message, l Limits) ([]MessageReceipt, error) {
	if messages == nil {
		return nil, nil
	}
	result := make([]MessageReceipt, len(messages))
	for i, message := range messages {
		r, e := contributionReceipt(message, l)
		if e != nil {
			return nil, e
		}
		result[i] = r
	}
	return result, nil
}
func copyMessages(messages []MessageReceipt, l Limits) ([]MessageReceipt, error) {
	if messages == nil {
		return nil, nil
	}
	// Message fields are trusted DTO envelopes, but content arguments/details
	// still cannot gain struct/marshaler authority through that envelope.
	for _, m := range messages {
		if m.ErrorCode != "" || (m.HasDetails && m.Details != nil) || (!m.HasDetails && m.DetailsValue != nil) {
			return nil, reject("invalid entry contribution details/error union")
		}
		for _, tool := range m.ToolsAdded {
			if _, e := copyObject(tool.Parameters, l); e != nil {
				return nil, e
			}
		}
		if _, e := dtoObject(m, l); e != nil {
			return nil, e
		}
		checked := m
		checked.Content = append([]goai.ContentBlock(nil), m.Content...)
		if e := restoreReceiptArguments(&checked, l); e != nil {
			return nil, e
		}
		if _, e := contributionReceipt(receiptMessage(checked), l); e != nil {
			return nil, e
		}
	}
	owned := make([]MessageReceipt, len(messages))
	for i, message := range messages {
		// Existing durable DTO conversion owns raw tool schemas and protocol
		// envelopes without invoking caller marshal authority in nested JSON.
		// A legacy caller may supply explicit empty arguments without the new
		// witness. Validate first, then record presence before DTO encoding.
		message.Content = append([]goai.ContentBlock(nil), message.Content...)
		if e := restoreReceiptArguments(&message, l); e != nil {
			return nil, e
		}
		for i, block := range message.Content {
			if block.Type == "toolCall" && len(block.Arguments) == 0 {
				found := false
				for _, witness := range message.EmptyArguments {
					if witness == i {
						found = true
					}
				}
				if !found {
					message.EmptyArguments = append(append([]int(nil), message.EmptyArguments...), i)
				}
			}
		}
		value, e := dtoObject(message, l)
		if e != nil {
			return nil, e
		}
		if e = fromObject(value, &owned[i], l); e != nil {
			return nil, e
		}
		if e = restoreReceiptArguments(&owned[i], l); e != nil {
			return nil, e
		}
	}
	return owned, nil
}
func copyEntry(v Entry, l Limits) (Entry, error) {
	value, e := copyObject(v.Value, l)
	if e != nil {
		return Entry{}, e
	}
	v.Value = value
	if v.HasData {
		v.Data, e = ownJSONValue(v.Data, l)
		if e != nil {
			return Entry{}, e
		}
	} else if v.Data != nil {
		return Entry{}, reject("entry data requires HasData")
	}
	v.Model, e = copyMessages(v.Model, l)
	if e != nil {
		return Entry{}, e
	}
	edits := make([]ContextEdit, len(v.Edits))
	for i, edit := range v.Edits {
		if edit.Target == 0 || uint64(edit.Target) > MaxID || (edit.Action != "omit" && edit.Action != "replace") || (edit.Action == "omit" && len(edit.Messages) != 0) {
			return Entry{}, reject("invalid context edit")
		}
		edit.Messages, e = copyMessages(edit.Messages, l)
		if e != nil {
			return Entry{}, e
		}
		edits[i] = edit
	}
	if v.Edits != nil {
		v.Edits = edits
	}
	return v, nil
}

// ContextView is a detached raw active transcript plus its reduced provider
// contributions. Edits apply within one captured head/tail ancestry snapshot.
type ContextView struct {
	Head          *Entry
	Entries       []Entry
	Contributions [][]MessageReceipt
	Messages      []MessageReceipt
}

func deriveContextView(state Snapshot, conversation, at ID, l Limits) (ContextView, error) {
	if at != 0 && !entryVisible(state, conversation, at) {
		return ContextView{}, reject("context cutoff not visible")
	}
	history, e := visibleHistory(state, EntryQuery{Conversation: conversation, MaxEntryID: at})
	if e != nil {
		return ContextView{}, e
	}
	return deriveContextHistory(history, l)
}

func deriveContextHistory(history []Entry, l Limits) (ContextView, error) {
	var e error
	view := ContextView{}
	var head ID
	for _, entry := range history {
		if entry.Head != 0 {
			copy, e := copyEntry(entry, l)
			if e != nil {
				return view, e
			}
			view.Head = &copy
			head = entry.Head
			break
		}
	}
	edits := map[ID]ContextEdit{}
	rangeEntries := []Entry{}
	for i := len(history) - 1; i >= 0; i-- {
		entry := history[i]
		if entry.ID < head {
			continue
		}
		rangeEntries = append(rangeEntries, entry)
		for _, edit := range entry.Edits {
			edits[edit.Target] = edit
		}
	}
	if view.Head != nil {
		view.Entries = append(view.Entries, *view.Head)
	}
	for _, entry := range rangeEntries {
		if view.Head != nil && entry.Head != 0 {
			continue
		}
		copy, e := copyEntry(entry, l)
		if e != nil {
			return view, e
		}
		view.Entries = append(view.Entries, copy)
	}
	for _, entry := range view.Entries {
		contribution := entry.Model
		if contribution == nil && entry.Kind == "message" {
			var receipt messageReceipt
			if e = fromObject(entry.Value, &receipt, l); e != nil {
				return view, e
			}
			contribution = []MessageReceipt{receipt}
		}
		if edit, exists := edits[entry.ID]; exists {
			if edit.Action == "omit" {
				contribution = nil
			} else {
				contribution = edit.Messages
			}
		}
		filtered := []MessageReceipt{}
		for _, receipt := range contribution {
			if receipt.Role == goai.RoleAssistant && (receipt.StopReason == goai.StopReasonError || receipt.StopReason == goai.StopReasonAborted || receipt.StopReason == goai.StopReasonDeferred) {
				continue
			}
			object, e := dtoObject(receipt, l)
			if e != nil {
				return view, e
			}
			var copy MessageReceipt
			if e = fromObject(object, &copy, l); e != nil {
				return view, e
			}
			filtered = append(filtered, copy)
		}
		view.Contributions = append(view.Contributions, filtered)
		view.Messages = append(view.Messages, filtered...)
	}
	view.Messages = leadContextWithSystem(orderContextToolResults(view.Messages))
	return view, nil
}
func orderContextToolResults(messages []MessageReceipt) []MessageReceipt {
	result := []MessageReceipt{}
	for i, message := range messages {
		if message.Role == goai.RoleToolResult {
			continue
		}
		result = append(result, message)
		if message.Role != goai.RoleAssistant {
			continue
		}
		results := map[string]MessageReceipt{}
		for j := i + 1; j < len(messages) && messages[j].Role != goai.RoleAssistant; j++ {
			candidate := messages[j]
			if candidate.Role == goai.RoleToolResult {
				if _, exists := results[candidate.ToolCallID]; !exists {
					results[candidate.ToolCallID] = candidate
				}
			}
		}
		for _, block := range message.Content {
			if block.Type != "toolCall" {
				continue
			}
			if receipt, exists := results[block.ID]; exists {
				result = append(result, receipt)
			} else {
				result = append(result, MessageReceipt{Role: goai.RoleToolResult, ToolCallID: block.ID, ToolName: block.Name, IsError: true, Timestamp: message.Timestamp, Content: []goai.ContentBlock{{Type: "text", Text: "Tool result unavailable: history ends before this call completed."}}, Details: JSON{"reason": "missing_result"}})
			}
		}
	}
	return result
}
func (s *Session) ContextView(ctx context.Context, conversation, at ID) (ContextView, error) {
	if e := s.enter(ctx); e != nil {
		return ContextView{}, e
	}
	defer s.leave()
	state, e := s.taskSnapshot(ctx)
	if e != nil {
		return ContextView{}, e
	}
	return s.cachedContextView(state, conversation, at)
}
