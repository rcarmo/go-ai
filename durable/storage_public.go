package durable

import (
	"context"
	"fmt"
)

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
	return c.commitPublic(ctx, batches, receive, nil)
}

func (c *storeCore) commitPublic(ctx context.Context, batches [][]StorageWrite, receive func(int, uint64), begin func() (func() error, error)) (CommitResult, error) {
	var sink func(commitReceipt) error
	if receive != nil {
		sink = func(receipt commitReceipt) error {
			receive(receipt.index, receipt.seq)
			return nil
		}
	}
	result, err := c.commitResolvedParts(ctx, len(batches), func(base Snapshot, index int) (Batch, error) {
		return publicBatch(base, batches[index], c.limits)
	}, sink, false, nil, begin)
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
	if err := checkDocumentCommandMultiplicity(writes); err != nil {
		return Batch{}, err
	}
	if err := checkPublicRecordClaims(base, writes); err != nil {
		return Batch{}, err
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

// Claims are checked before looking up the document actions. A repeated task
// or submission is an upsert; conversations, entries and document creates
// are immutable claims.
func checkPublicRecordClaims(base Snapshot, writes []StorageWrite) error {
	seen := make(map[ID]string)
	for _, command := range writes {
		id, kind, mutable := publicRecordClaim(command)
		if kind == "" || id == 0 || uint64(id) > MaxID {
			continue
		}
		existing := snapshotRecordKind(base, id)
		earlier := seen[id]
		switch {
		case existing != "" && (!mutable || existing != kind):
			return reject(fmt.Sprintf("ID %d already belongs to %s", id, existing))
		case earlier != "" && !mutable:
			return reject(fmt.Sprintf("ID %d is written more than once", id))
		case earlier != "" && earlier != kind:
			return reject(fmt.Sprintf("ID %d is written as two record types", id))
		}
		seen[id] = kind
	}
	return nil
}

func publicRecordClaim(write StorageWrite) (id ID, kind string, mutable bool) {
	switch write.Type {
	case "conversation":
		if r, ok := recordValue[Conversation](write.Value); ok {
			return r.ID, "conversation", false
		}
	case "entry":
		if r, ok := recordValue[Entry](write.Value); ok {
			return r.ID, "entry", false
		}
	case "task":
		if r, ok := recordValue[TaskRecord](write.Value); ok {
			return r.ID, "task", true
		}
	case "submission":
		if r, ok := recordValue[SubmissionRecord](write.Value); ok {
			return r.ID, "submission", true
		}
	case "document.create", "document.copy":
		if write.Record != nil {
			return write.Record.ID, "document", false
		}
	}
	return 0, "", false
}

// Stage document actions before resolving any incarnation, as SQLite does
// for each accepted part. A missing document does not mask duplicate commands.
func checkDocumentCommandMultiplicity(writes []StorageWrite) error {
	type action uint8
	const (
		content action = 1 << iota
		retire
	)
	var seen map[ID]action
	for _, write := range writes {
		id := write.ID
		command := content
		switch write.Type {
		case "document.create", "document.copy":
			if write.Record == nil {
				continue // the union decoder reports malformed Go commands
			}
			id = write.Record.ID
		case "document.change":
		case "document.retire":
			command = retire
		default:
			continue
		}
		if seen[id]&command != 0 {
			if command == retire {
				return reject(fmt.Sprintf("Document %d is retired more than once", id))
			}
			return reject(fmt.Sprintf("Document %d has more than one content command", id))
		}
		if seen == nil {
			seen = make(map[ID]action)
		}
		seen[id] |= command
	}
	return nil
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
		entry = nativeEntryDiagnosticData(entry)
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

// nativeTask stores the public record in the generic versioned envelope.
// Storage admission is definition-free: builtin names, versions, inputs and
// opaque checkpoints gain execution meaning only in the Harness.
func nativeTask(record TaskRecord, l Limits) (Task, error) {
	if record.Input == nil || !record.Input.Present {
		return Task{}, reject("task record input is absent")
	}
	if record.Version == 0 || record.Version > MaxID {
		return Task{}, reject("native task execution version")
	}
	task := Task{ID: record.ID, Conversation: record.Conversation, Owner: record.Owner, Kind: record.Kind, Status: record.State.Status, Checkpoint: JSON{}, Trace: record.Trace, Links: record.Links, Restarts: record.Restarts, AbandonOnRestart: record.AbandonOnRestart, AbortReason: record.AbortReason, StartedAt: record.StartedAt, EndedAt: record.EndedAt}
	task.Execution = &TaskExecution{Tag: nativeTaskTag, Native: &NativeTaskExecution{Version: record.Version, Input: record.Input, Background: record.Background, AbortRequested: record.AbortRequested, Memos: record.Memos, State: record.State}}
	if record.State.Status == "terminal" && record.State.Outcome != nil {
		task.Status = outcomeRawStatus(record.State.Outcome)
	}
	return copyTask(task, l)
}

func snapshotRecordKind(base Snapshot, id ID) string {
	if _, ok := base.Conversations[id]; ok {
		return "conversation"
	}
	if _, ok := base.Entries[id]; ok {
		return "entry"
	}
	if _, ok := base.Tasks[id]; ok {
		return "task"
	}
	if _, ok := base.Submissions[id]; ok {
		return "submission"
	}
	if _, ok := base.Documents[id]; ok {
		return "document"
	}
	return ""
}
