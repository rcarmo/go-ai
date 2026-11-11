package durable

import (
	"encoding/json"
	"strconv"
)

// The canonical projection is sealed before storage. An unsupported private
// legacy record can end a host guard, never undo a durable commit or expose its
// native envelope as a purported source record.
type preparedPublicCommit struct {
	value     CommitPublication
	documents map[ID]int
	err       error
}

func (s *Session) preparePublicCommit(publication sessionPublication, state Snapshot, revisions map[ID]readonlyDocumentRevision) preparedPublicCommit {
	prepared := preparedPublicCommit{value: CommitPublication{Seq: publication.Seq}, documents: map[ID]int{}}
	nodes := make([]*ImmutableValue, 0, len(publication.Tables)+len(publication.Documents))
	fail := func(err error) preparedPublicCommit { prepared.err = err; return prepared }
	for _, write := range publication.Tables {
		var command StorageWrite
		switch {
		case write.Conversation != nil:
			command = StorageWrite{Type: "conversation", Value: *write.Conversation}
		case write.Entry != nil:
			command = StorageWrite{Type: "entry", Value: *write.Entry}
		case write.Task != nil:
			record, err := CanonicalTask(*write.Task, s.limits)
			if err != nil {
				return fail(err)
			}
			command = StorageWrite{Type: "task", Value: record}
		case write.Submission != nil:
			command = StorageWrite{Type: "submission", Value: publicSubmission(*write.Submission)}
		default:
			return fail(reject("unsupported publication table change"))
		}
		wire, err := command.MarshalJSON()
		if err != nil {
			return fail(err)
		}
		var object JSON
		if err = decodeStrict(wire, s.limits, s.limits.MaxRecordBytes, &object); err != nil {
			return fail(err)
		}
		node, err := NewImmutableValue(object, s.limits)
		if err != nil {
			return fail(err)
		}
		nodes = append(nodes, node)
	}
	for _, change := range publication.Documents {
		object := JSON{"type": "document", "kind": "change", "record": publicDocumentMetadata(change.Record)}
		switch change.Record.Scope {
		case "conversation":
			object["conversationId"] = change.Record.Owner
		case "task":
			if task, ok := state.Tasks[change.Record.Owner]; ok {
				object["conversationId"] = task.Conversation
			} else {
				return fail(reject("publication task document owner absent"))
			}
		}
		var value ReadonlyValue
		switch {
		case change.Source != nil:
			object["type"] = "document.copy"
			delete(object, "kind")
			point := any(change.Source.At.Seq)
			if change.Source.At.Current {
				point = "current"
			}
			object["source"] = JSON{"id": change.Source.ID, "at": point}
		case change.Record.Retired:
			object["kind"] = "retire"
		default:
			value = revisions[change.Record.ID].value
			if value == nil {
				value = s.cachedReadonlyDocument(change.Record.ID, change.Version)
			}
			if value == nil {
				node, err := NewImmutableValue(change.Value, s.limits)
				if err != nil {
					return fail(err)
				}
				value = readonlyValue(node)
			}
			ops := change.Ops
			if ops == nil {
				ops = []Operation{}
			}
			object["version"], object["value"], object["ops"] = change.Version, value, ops
		}
		node, err := NewImmutableValue(object, s.limits)
		if err != nil {
			return fail(err)
		}
		// Freezing metadata must not detach the exact adopted tracker.
		if value != nil {
			node.mapValue["value"] = value.immutableNode()
		}
		prepared.documents[change.Record.ID] = len(nodes)
		nodes = append(nodes, node)
	}
	prepared.value.Changes = readonlyValue(&ImmutableValue{kind: "array", arrayValue: nodes})
	if _, err := encodeBounded(prepared.value.Changes, s.limits, s.limits.MaxRetainedBytesAsInt()); err != nil {
		return fail(err)
	}
	return prepared
}
func publicDocumentMetadata(record Document) JSON {
	scope := JSON{"kind": record.Scope}
	switch record.Scope {
	case "conversation":
		scope["conversationId"] = record.Owner
	case "task":
		scope["taskId"] = record.Owner
	}
	metadata := JSON{"id": record.ID, "kind": record.Kind, "scope": scope, "createdAt": record.CreatedAt}
	if record.Family {
		metadata["key"] = record.Key
	}
	if record.Scope == "conversation" {
		metadata["history"] = documentHistory(record)
		metadata["fork"] = documentFork(record)
	}
	if record.RetiredAt != 0 {
		metadata["retiredAt"] = record.RetiredAt
	}
	return metadata
}

// Sequence-dependent stamps are the only fields changed after preparation.
// Nodes are still private until dispatch, so no previous public value mutates.
func (prepared *preparedPublicCommit) stamp(publication sessionPublication) {
	prepared.value.Seq = publication.Seq
	if prepared.err != nil {
		return
	}
	for _, change := range publication.Documents {
		index, ok := prepared.documents[change.Record.ID]
		if !ok {
			continue
		}
		node := prepared.value.Changes.immutableNode().arrayValue[index]
		record := node.mapValue["record"]
		record.mapValue["createdAt"] = &ImmutableValue{kind: "number", number: json.Number(strconv.FormatUint(change.Record.CreatedAt, 10))}
		if change.Record.RetiredAt != 0 {
			record.mapValue["retiredAt"] = &ImmutableValue{kind: "number", number: json.Number(strconv.FormatUint(change.Record.RetiredAt, 10))}
		}
	}
}
