package durable

import "context"

// CommitResult reports what one public grouped Storage.Commit call stored and
// which batch was refused, if one was. Refused is nil when every batch in the
// call became durable. It mirrors the original `{ stored, refused?: unknown }`,
// which resolves rather than rejects on a refusal and carries the refused value
// verbatim; this call still rejects on a backend failure, and the receipts
// already delivered still stand in that case.
type CommitResult struct {
	Stored  int
	Refused *Refusal
}

// Commit persists the original public storage command batches in order, each as
// one atomic commit with its own sequence, and calls receive once per durable
// batch, in order, synchronously. Batches before a refused one stay durable; it
// and the ones after it are not stored. A backend failure rejects the call, but
// the receipts already delivered still stand. receive is a durability receipt,
// not publication: it runs before the call resolves and never substitutes for
// the Session's own later publication.
//
// The command union is translated to the private native envelope under the core
// line, so a document change or retirement may consult the stored incarnation
// it applies to. Records the native store cannot represent are refused before
// they become durable rather than written in a different shape.
func (c *storeCore) Commit(ctx context.Context, batches [][]StorageWrite, receive func(index int, seq uint64)) (CommitResult, error) {
	var sink func(commitReceipt) error
	if receive != nil {
		sink = func(receipt commitReceipt) error {
			receive(receipt.index, receipt.seq)
			return nil
		}
	}
	result, err := c.commitResolvedParts(ctx, len(batches), func(base Snapshot, index int) (Batch, error) {
		return publicBatch(base, batches[index], c.limits)
	}, sink, false, nil)
	if err != nil {
		return CommitResult{Stored: result.stored}, err
	}
	if result.refused != nil {
		return CommitResult{Stored: result.stored, Refused: &Refusal{refusal: result.refused}}, nil
	}
	return CommitResult{Stored: result.stored}, nil
}

// publicBatch validates one original command batch and translates it. The
// address of a document change or retirement comes from the running base image,
// which the caller holds the core line for.
func publicBatch(base Snapshot, writes []StorageWrite, l Limits) (Batch, error) {
	if len(writes) > l.MaxWrites {
		return Batch{}, reject("invalid batch count")
	}
	batch := Batch{Writes: make([]Write, 0, len(writes))}
	for _, command := range writes {
		write, err := publicWrite(base, command, l)
		if err != nil {
			return Batch{}, err
		}
		batch.Writes = append(batch.Writes, write)
	}
	return batch, nil
}

func publicWrite(base Snapshot, command StorageWrite, l Limits) (Write, error) {
	switch command.Type {
	case "conversation":
		conversation, ok := recordValue[Conversation](command.Value)
		if !ok {
			return Write{}, reject("storage record value type")
		}
		// OwnerConversation is derived from storage on every adoption and is
		// never persisted; a caller-supplied value must not become authority.
		conversation.OwnerConversation = 0
		return Write{Op: "create-conversation", Conversation: &conversation}, nil
	case "entry":
		entry, ok := recordValue[Entry](command.Value)
		if !ok {
			return Write{}, reject("storage record value type")
		}
		// The original entry carries `value` only for a legacy message payload;
		// the private native envelope always owns a JSON root.
		if entry.Value == nil {
			entry.Value = JSON{}
		}
		return Write{Op: "append-entry", Entry: &entry}, nil
	case "task":
		record, ok := recordValue[TaskRecord](command.Value)
		if !ok {
			return Write{}, reject("storage record value type")
		}
		task, err := nativeTask(record, l)
		if err != nil {
			return Write{}, err
		}
		return Write{Op: "put-task", Task: &task}, nil
	case "submission":
		record, ok := recordValue[SubmissionRecord](command.Value)
		if !ok {
			return Write{}, reject("storage record value type")
		}
		submission, err := nativeSubmission(record, l)
		if err != nil {
			return Write{}, err
		}
		return Write{Op: "put-submission", Submission: &submission}, nil
	case "document.create":
		if command.Content == nil || command.Content.Kind != "base" || command.Record == nil || command.ID != 0 || command.Source != nil {
			return Write{}, reject("document creation union")
		}
		document, err := nativeDocumentAddress(*command.Record, command.Content.Version, command.Content.Value)
		if err != nil {
			return Write{}, err
		}
		return Write{Op: "put-document", Document: &document}, nil
	case "document.change":
		if command.Content == nil || command.Record != nil || command.Source != nil || command.ID == 0 {
			return Write{}, reject("document mutation union")
		}
		switch command.Content.Kind {
		case "delta":
			return Write{Op: "delta-document", Delta: &DocumentDelta{ID: command.ID, Version: command.Content.Version, Ops: command.Content.Ops}}, nil
		case "base":
			old, ok := base.Documents[command.ID]
			if !ok || old.Retired {
				return Write{}, reject("unknown document incarnation")
			}
			next := old
			next.Value, next.Version, next.DeltasSinceBase = command.Content.Value, command.Content.Version, 0
			return Write{Op: "put-document", Document: &next}, nil
		default:
			return Write{}, reject("document content kind")
		}
	case "document.retire":
		if command.Record != nil || command.Content != nil || command.Source != nil || command.ID == 0 {
			return Write{}, reject("document retirement union")
		}
		old, ok := base.Documents[command.ID]
		if !ok || old.Retired {
			return Write{}, reject("unknown document incarnation")
		}
		return Write{Op: "retire-document", Document: &old}, nil
	case "document.copy":
		if command.Record == nil || command.Source == nil || command.ID != 0 || command.Content != nil {
			return Write{}, reject("document copy union")
		}
		target, err := nativeDocumentAddress(*command.Record, 0, nil)
		if err != nil {
			return Write{}, err
		}
		return Write{Op: "copy-document", Document: &target, Source: command.Source}, nil
	default:
		return Write{}, reject("storage write type")
	}
}

// recordValue accepts a record command payload by value or pointer, matching
// the union codec's declared type without admitting any other Go value.
func recordValue[T any](value any) (T, bool) {
	switch typed := value.(type) {
	case T:
		return typed, true
	case *T:
		if typed != nil {
			return *typed, true
		}
	}
	var zero T
	return zero, false
}

// documentAddress builds the native document record of an original document
// record plus its content version. Retirement stamps and materialised delta
// counts stay storage-owned and are never taken from the caller.
func nativeDocumentAddress(record DocumentCreate, version uint64, value JSON) (Document, error) {
	if err := record.validate(); err != nil {
		return Document{}, err
	}
	document := Document{ID: record.ID, Kind: record.Kind, Version: version, Value: value}
	if record.Key != nil {
		document.Key, document.Family = *record.Key, true
	}
	switch record.Scope.Kind {
	case "session":
		document.Scope = "session"
	case "task":
		document.Scope, document.Owner = "task", record.Scope.TaskID
	case "conversation":
		document.Scope, document.Owner = "conversation", record.Scope.ConversationID
		document.History, document.Fork = record.History, record.Fork
	}
	return document, nil
}

// These two adapters still use private checkpoints that cannot be rebuilt from
// their public records alone. Compaction and application pi.* tasks use the
// generic envelope; a namespace prefix is not execution authority.
func builtinTaskKind(kind string) bool { return kind == "pi.generation" || kind == "pi.tool" }

// nativeTask rebuilds the versioned native execution envelope of a public
// runtime record for the kinds the native store owns generically. Built-in
// kinds keep their own raw checkpoint contract and their state is not derivable
// from the public record alone, so they are refused rather than written in a
// different shape.
func nativeTask(record TaskRecord, l Limits) (Task, error) {
	if record.Input == nil || !record.Input.Present {
		return Task{}, reject("task record input is absent")
	}
	if record.Version == 0 || record.Version > MaxID {
		return Task{}, reject("native task execution version")
	}
	if builtinTaskKind(record.Kind) {
		return Task{}, reject("builtin task write requires its native checkpoint")
	}
	task := Task{ID: record.ID, Conversation: record.Conversation, Owner: record.Owner, Kind: record.Kind, Status: record.State.Status, Checkpoint: JSON{}, Trace: record.Trace, Links: record.Links, Restarts: record.Restarts, AbandonOnRestart: record.AbandonOnRestart, AbortReason: record.AbortReason, StartedAt: record.StartedAt, EndedAt: record.EndedAt}
	task.Execution = &TaskExecution{Tag: nativeTaskTag, Native: &NativeTaskExecution{Version: record.Version, Input: record.Input, Background: record.Background, AbortRequested: record.AbortRequested, Memos: record.Memos, State: record.State}}
	return copyTask(task, l)
}
