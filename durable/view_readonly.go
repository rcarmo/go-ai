package durable

// ConversationView is a committed transcript window. Entries and documents are
// persistent readonly JSON nodes; unchanged branches retain pointer identity.
// Copy explicitly converts the view to detached mutable records.
type ConversationView struct {
	Conversation Conversation  `json:"conversation"`
	Entries      ReadonlyValue `json:"entries"`
	Before       ID            `json:"before,omitempty"`
	Docs         ReadonlyValue `json:"docs"`
}

func (v ConversationView) Copy(limits Limits) (ConversationViewCopy, error) {
	if err := limits.validate(); err != nil {
		return ConversationViewCopy{}, err
	}
	body, err := dtoObject(v, limits)
	if err != nil {
		return ConversationViewCopy{}, err
	}
	var copy ConversationViewCopy
	if err := fromObject(body, &copy, limits); err != nil {
		return ConversationViewCopy{}, err
	}
	return copy, nil
}

func freezeConversationView(value ConversationViewCopy, limits Limits) (ConversationView, error) {
	body, err := dtoObject(value, limits)
	if err != nil {
		return ConversationView{}, err
	}
	root, err := NewImmutableValue(body, limits)
	if err != nil {
		return ConversationView{}, err
	}
	docs, _ := root.Get("docs")
	entries, _ := root.Get("entries")
	return ConversationView{Conversation: value.Conversation, Before: value.Before, Docs: readonlyValue(docs), Entries: readonlyValue(entries)}, nil
}

// The live subscriptions are the mount's owners. Reuse their adopted documents
// on attachment; removing the last subscription releases the mount implicitly.
func (s *Session) shareConversationDocuments(value ConversationView, state Snapshot) ConversationView {
	s.observerMu.Lock()
	defer s.observerMu.Unlock()
	for sub := range s.subscriptions {
		if peer := sub.conversation; peer != nil {
			peer.mu.Lock()
			if peer.head.Conversation.ID == value.Conversation.ID && peer.projectionErr == nil && peer.readonlyHead.Docs != nil {
				value.Docs = peer.readonlyHead.Docs
				peer.mu.Unlock()
				return value
			}
			peer.mu.Unlock()
		}
	}
	if !s.hasNativeReadImage() {
		return value
	}
	// A new mount borrows loaded builtin trackers; subsequent document edits
	// still use the mount's independent applyImmutable branch below.
	children := make(map[string]*ImmutableValue, len(value.Docs.immutableNode().mapValue))
	for kind, node := range value.Docs.immutableNode().mapValue {
		children[kind] = node
	}
	for _, record := range state.Documents {
		if record.Scope != "conversation" || record.Owner != value.Conversation.ID || record.Retired || record.Family || !mountedKind(record.Kind) {
			continue
		}
		version := uint64(1)
		if record.Kind == "pi.live" {
			version = 3
		} // pinned builtin definition, not the stored base version
		if node := s.cachedReadonlyDocument(record.ID, version); node != nil {
			children[record.Kind] = node.immutableNode()
		}
	}
	value.Docs = readonlyValue(&ImmutableValue{kind: "object", mapValue: children})
	return value
}

// Freeze only inserted retained entries. Existing entry nodes survive append
// and trimming, including when the delivery operation log falls back to a root.
func advanceReadonlyConversation(before ConversationView, next ConversationViewCopy, appended []Entry, drop int, documents []Operation, limits Limits, shared map[ID]*ImmutableValue) (ConversationView, error) {
	result := ConversationView{Conversation: next.Conversation, Before: next.Before, Entries: before.Entries, Docs: before.Docs}
	if docs, ok := shared[next.Conversation.ID]; ok {
		result.Docs = readonlyValue(docs)
	} else {
		if len(documents) > 0 {
			root := &ImmutableValue{kind: "object", mapValue: map[string]*ImmutableValue{"docs": before.Docs.immutableNode()}}
			// The delivery log may exceed its array-member budget and fall
			// back to a bounded diff. Apply each original change independently
			// so that this does not reject a valid final mounted value.
			for _, operation := range documents {
				applied, err := applyImmutableValue(root, []Operation{operation}, limits)
				if err != nil {
					return ConversationView{}, err
				}
				root = applied
			}
			docs, _ := root.Get("docs")
			result.Docs = readonlyValue(docs)
		}
		if shared != nil {
			shared[next.Conversation.ID] = result.Docs.immutableNode()
		}
	}
	if len(appended) > 0 {
		children := make([]*ImmutableValue, 0, len(next.Entries))
		old := before.Entries.immutableNode().arrayValue
		oldStart := drop
		if oldStart > len(old) {
			oldStart = len(old)
		}
		children = append(children, old[oldStart:]...)
		skip := drop - len(old)
		if skip < 0 {
			skip = 0
		}
		for _, entry := range appended[skip:] {
			body, err := conversationEntryObject(entry, limits)
			if err != nil {
				return ConversationView{}, err
			}
			node, err := NewImmutableValue(body, limits)
			if err != nil {
				return ConversationView{}, err
			}
			children = append(children, node)
		}
		result.Entries = readonlyValue(&ImmutableValue{kind: "array", arrayValue: children})
	}
	return result, nil
}

// Public view records use the pinned public codecs, not SQLite's method-free
// native envelopes. Caller payloads are bounded before these exact codecs run.
func conversationRecordObject(value Conversation, limits Limits) (JSON, error) {
	wire, err := value.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var object JSON
	if err := decodeStrict(wire, limits, limits.MaxDocumentBytes, &object); err != nil {
		return nil, err
	}
	return object, nil
}
func conversationEntryObject(value Entry, limits Limits) (JSON, error) {
	if _, err := encodeBounded(value, limits, limits.MaxRecordBytes); err != nil {
		return nil, err
	}
	wire, err := value.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var object JSON
	if err := decodeStrict(wire, limits, limits.MaxRecordBytes, &object); err != nil {
		return nil, err
	}
	return object, nil
}
func conversationViewObject(value ConversationViewCopy, limits Limits) (JSON, error) {
	if _, err := encodeBounded(value, limits, limits.MaxDocumentBytes); err != nil {
		return nil, err
	}
	conversation, err := conversationRecordObject(value.Conversation, limits)
	if err != nil {
		return nil, err
	}
	entries := make([]any, len(value.Entries))
	for i, entry := range value.Entries {
		object, err := conversationEntryObject(entry, limits)
		if err != nil {
			return nil, err
		}
		entries[i] = object
	}
	result := JSON{"conversation": conversation, "entries": entries, "docs": value.Docs}
	if value.Before != 0 {
		result["before"] = value.Before
	}
	return result, nil
}
