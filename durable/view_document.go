package durable

// Incarnations belong to the publication-time head, not the last delivered
// value. They contain no document bodies and are installed only after receipt.
type conversationIncarnation struct {
	id      ID
	version uint64
}

func conversationIncarnations(state Snapshot, conversation ID) map[string]conversationIncarnation {
	result := map[string]conversationIncarnation{}
	for _, doc := range state.Documents {
		if doc.Scope == "conversation" && doc.Owner == conversation && !doc.Retired && !doc.Family && doc.Key == "" && mountedKind(doc.Kind) {
			result[doc.Kind] = conversationIncarnation{doc.ID, doc.Version}
		}
	}
	return result
}

func projectConversationDocuments(before ConversationViewCopy, mounted map[string]conversationIncarnation, publication CommitPublication, limits Limits) (map[string]JSON, map[string]conversationIncarnation, []Operation, error) {
	docs := make(map[string]JSON, len(before.Docs))
	for kind, value := range before.Docs {
		docs[kind] = value
	}
	incarnations := make(map[string]conversationIncarnation, len(mounted))
	for kind, incarnation := range mounted {
		incarnations[kind] = incarnation
	}
	ops := []Operation{}
	for _, change := range publication.Documents {
		doc := change.Record
		if doc.Scope != "conversation" || doc.Owner != before.Conversation.ID || doc.Family || doc.Key != "" || !mountedKind(doc.Kind) {
			continue
		}
		previous, exists := incarnations[doc.Kind]
		path := []any{"docs", doc.Kind}
		if doc.Retired || change.Value == nil {
			if !exists || previous.id != doc.ID {
				continue
			}
			delete(incarnations, doc.Kind)
			delete(docs, doc.Kind)
			ops = append(ops, Operation{"d", path})
			continue
		}
		value, err := copyObject(change.Value, limits)
		if err != nil {
			return nil, nil, nil, err
		}
		if !exists || previous.id != doc.ID || previous.version != change.Version {
			ops = append(ops, Operation{"s", path, map[string]any(value)})
		} else {
			// Own each watch's payloads before prefixing: peers must not share the
			// publication's mutable operation containers with a callback.
			changes, err := ownOperations(change.Ops, limits)
			if err != nil {
				return nil, nil, nil, err
			}
			// Opaque native writes without an operation log still publish their value.
			if len(changes) == 0 && !equalJSONValue(docs[doc.Kind], value) {
				changes = []Operation{{"r", map[string]any(value)}}
			}
			for _, operation := range changes {
				if operation[0] == "r" {
					ops = append(ops, Operation{"s", path, operation[1]})
				} else {
					prefixed := append(Operation{}, operation...)
					prefixed[1] = append(append([]any{}, path...), operation[1].([]any)...)
					ops = append(ops, prefixed)
				}
			}
		}
		docs[doc.Kind] = value
		incarnations[doc.Kind] = conversationIncarnation{doc.ID, change.Version}
	}
	return docs, incarnations, ops, nil
}

func mountedConversationIncarnations(state Snapshot, id ID) map[string]conversationIncarnation {
	result := conversationIncarnations(state, id)
	for _, definition := range mountedDocumentDefinitions {
		if incarnation, ok := result[definition.options.Kind]; ok {
			incarnation.version = definition.options.Version
			result[definition.options.Kind] = incarnation
		}
	}
	return result
}
