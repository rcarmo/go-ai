package durable

import "context"

// A decorated Storage remains the persistence authority. The core retained by
// Session in this mode is only its receipted validation image; it has no sink.
func (s *Session) reserveStorageID(ctx context.Context) (ID, error) {
	if !s.delegatedStorage {
		return s.core.mintID(ctx, true)
	}
	id, err := s.store.MintID(ctx)
	if err == nil {
		s.core.ownershipMu.Lock()
		if uint64(id) > s.core.readHighWater {
			s.core.readHighWater = uint64(id)
		}
		s.core.ownershipMu.Unlock()
	}
	return id, err
}

func storageDocumentCreate(document Document) *DocumentCreate {
	scope := DocumentCreateScope{Kind: document.Scope}
	record := &DocumentCreate{ID: document.ID, Kind: document.Kind, Scope: scope}
	switch document.Scope {
	case "conversation":
		record.Scope.ConversationID = document.Owner
		record.History = documentHistory(document)
		record.Fork = documentFork(document)
	case "task":
		record.Scope.TaskID = document.Owner
	}
	if document.Family || document.Key != "" {
		key := document.Key
		record.Key = &key
	}
	return record
}

func publicSessionBatch(exists func(ID) bool, batch Batch, limits Limits) ([]StorageWrite, error) {
	commands := make([]StorageWrite, 0, len(batch.Writes))
	for _, write := range batch.Writes {
		var command StorageWrite
		switch write.Op {
		case "create-conversation":
			command = StorageWrite{Type: "conversation", Value: *write.Conversation}
		case "append-entry":
			command = StorageWrite{Type: "entry", Value: *write.Entry}
		case "put-task":
			record, err := CanonicalTask(*write.Task, limits)
			if err != nil {
				return nil, err
			}
			command = StorageWrite{Type: "task", Value: record}
		case "put-submission":
			command = StorageWrite{Type: "submission", Value: publicSubmission(*write.Submission)}
		case "put-document":
			document := write.Document
			content := &DocumentContent{Kind: "base", Version: document.Version, Value: document.Value}
			if !exists(document.ID) {
				command = StorageWrite{Type: "document.create", Record: storageDocumentCreate(*document), Content: content}
			} else {
				command = StorageWrite{Type: "document.change", ID: document.ID, Content: content}
			}
		case "delta-document":
			command = StorageWrite{Type: "document.change", ID: write.Delta.ID, Content: &DocumentContent{Kind: "delta", Version: write.Delta.Version, Ops: write.Delta.Ops}}
		case "copy-document":
			command = StorageWrite{Type: "document.copy", Record: storageDocumentCreate(*write.Document), Source: write.Source}
		case "retire-document":
			command = StorageWrite{Type: "document.retire", ID: write.Document.ID}
		default:
			return nil, reject("unsupported Session storage command")
		}
		commands = append(commands, command)
	}
	return commands, nil
}

func (s *Session) commitStorageParts(ctx context.Context, batches []Batch, receive func(commitReceipt) error, expected *uint64) (commitGroupResult, error) {
	if !s.delegatedStorage {
		if expected != nil {
			return s.core.commitPartsAt(ctx, batches, receive, true, expected)
		}
		return s.core.commitParts(ctx, batches, receive, true)
	}
	if err := s.core.enter(ctx); err != nil {
		return commitGroupResult{}, err
	}
	defer s.core.leave()
	if expected != nil && *expected != s.core.state.Seq {
		return commitGroupResult{}, reject("stale storage sequence")
	}
	commands := make([][]StorageWrite, len(batches))
	s.core.ownershipMu.Lock()
	high := s.core.readHighWater
	s.core.ownershipMu.Unlock()
	// Encode commands independently of settlement. Only received batches
	// advance the projection, with the sequence assigned by Storage.
	parts := make([]publicCommitPart, len(batches))
	documents := make(map[ID]bool)
	exists := func(id ID) bool {
		if live, changed := documents[id]; changed {
			return live
		}
		doc, found := s.core.state.Documents[id]
		return found && !doc.Retired
	}
	for index, batch := range batches {
		var err error
		commands[index], err = publicSessionBatch(exists, batch, s.limits)
		if err != nil {
			return commitGroupResult{}, err
		}
		// Storage receives its own command envelopes. Keep the replay input
		// detached, including nested values the host may retain or mutate.
		payload, err := encodeBounded(batch, s.limits, s.limits.MaxFramePayloadBytes)
		if err != nil {
			return commitGroupResult{}, err
		}
		part := &parts[index]
		part.high = high
		if err := decodeStrict(payload, s.limits, s.limits.MaxFramePayloadBytes, &part.batch); err != nil {
			return commitGroupResult{}, err
		}
		for i, write := range batch.Writes {
			part.batch.Writes[i].copyValue = write.copyValue
			if write.Document != nil {
				documents[write.Document.ID] = write.Op != "retire-document"
			}
		}
	}
	result := commitGroupResult{}
	var receiptError error
	stored, err := s.store.Commit(ctx, commands, func(index int, seq uint64) {
		if receiptError != nil {
			return
		}
		if index != result.stored || index < 0 || index >= len(parts) {
			receiptError = reject("invalid public Storage receipt")
			return
		}
		receipt := commitReceipt{index: index, seq: seq, public: &parts[index]}
		next, err := receipt.nextImage(s.core.state, s.limits)
		if err != nil {
			receiptError = err
			return
		}
		s.core.state = next
		result.stored++
		result.lastSeq = seq
		if receive != nil {
			receiptError = receive(receipt)
		}
	})
	if receiptError != nil {
		return result, receiptError
	}
	if err != nil {
		return result, err
	}
	if stored.Stored != result.stored {
		return result, reject("public Storage omitted a durability receipt")
	}
	if stored.Refused != nil {
		result.refused = &commitRefusal{value: stored.Refused.Value()}
	}
	return result, nil
}

func (s *Session) closeStorage(ctx context.Context) error {
	if !s.delegatedStorage {
		return s.core.close(ctx, true)
	}
	err := s.store.Close(ctx)
	if closeErr := s.core.close(ctx, true); err == nil {
		err = closeErr
	}
	return err
}
