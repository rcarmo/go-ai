package durable

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// No side tables are written into an upstream file. The native serial
// validator has already accepted a prefix; convert its writes to the source
// tables within one SQLite transaction, then let the caller deliver receipts.
func (s *SQLiteStorage) appendUpstreamSeries(frames []storageFrame) error {
	if err := s.exec("BEGIN IMMEDIATE"); err != nil {
		return reject("SQLite write admission unavailable")
	}
	if err := s.storeUpstreamSeries(frames); err != nil {
		if rollback := s.exec("ROLLBACK"); rollback != nil {
			return errors.Join(err, rollback)
		}
		return reject("upstream SQLite write rejected: " + err.Error())
	}
	return s.exec("COMMIT")
}

func (s *SQLiteStorage) storeUpstreamSeries(frames []storageFrame) error {
	base := s.state
	table := s.upstreamTable("durable_metadata")
	var nextId string
	var nextSeq uint64
	if err := s.db.QueryRowContext(context.Background(), "SELECT next_id,next_seq FROM "+table+" WHERE singleton=1").Scan(&nextId, &nextSeq); err != nil {
		return err
	}
	if nextSeq != base.Seq+1 || nextId != strconv.FormatUint(base.HighWater+1, 10) {
		return reject("upstream SQLite prefix changed externally")
	}
	for _, frame := range frames {
		switch frame.kind {
		case 1:
			base.HighWater = frame.high
		case 2:
			var record commitRecord
			if err := decodeStrict(frame.payload, s.limits, s.limits.MaxFramePayloadBytes, &record); err != nil {
				return err
			}
			next, err := prepareExplicit(base, record, s.limits)
			if err != nil {
				return err
			}
			if next.HighWater != frame.high {
				return ErrCorrupt
			}
			if err := s.writeUpstreamRecords(base, next, record.Writes); err != nil {
				return err
			}
			base = next
		default:
			return ErrCorrupt
		}
	}
	return s.exec("UPDATE "+table+" SET next_id=?,next_seq=? WHERE singleton=1", strconv.FormatUint(base.HighWater+1, 10), base.Seq+1)
}

func sqliteOptionalID(id ID) any {
	if id == 0 {
		return nil
	}
	return id
}

func sqliteOptionalSeq(seq uint64) any {
	if seq == 0 {
		return nil
	}
	return seq
}

func upstreamJSON(value any) (string, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

// Indexed string columns are JSON-encoded, unlike the TEXT columns holding
// status/scope. Preserve literal Unicode line/paragraph separators as JS does.
func upstreamIndexedString(value string) string {
	encoded, _ := upstreamJSON(value) // strings cannot fail JSON encoding
	var b strings.Builder
	for n := 0; n < len(encoded); n++ {
		if encoded[n] == '\\' && n+1 < len(encoded) {
			if strings.HasPrefix(encoded[n:], `\u2028`) || strings.HasPrefix(encoded[n:], `\u2029`) {
				if encoded[n+5] == '8' {
					b.WriteRune('\u2028')
				} else {
					b.WriteRune('\u2029')
				}
				n += 5
				continue
			}
			b.WriteByte(encoded[n])
			n++
		}
		b.WriteByte(encoded[n])
	}
	return b.String()
}

func (s *SQLiteStorage) insertUpstreamRow(table, columns, conflict string, vals ...any) error {
	return s.exec("INSERT INTO "+s.upstreamTable(table)+" ("+columns+") VALUES (?"+strings.Repeat(",?", len(vals)-1)+") "+conflict, vals...)
}

func (s *SQLiteStorage) writeUpstreamRecords(before, after Snapshot, writes []Write) error {
	documents := make(map[ID]bool)
	completed := make(map[ID]bool)
	for _, w := range writes {
		var id ID
		var kind string
		var fresh bool
		switch w.Op {
		case "create-conversation":
			id, kind, fresh = w.Conversation.ID, "conversation", true
			r := after.Conversations[id]
			raw, err := upstreamJSON(r)
			if err != nil {
				return err
			}
			if err := s.insertUpstreamRow("conversations", "id,owner_conversation_id,owner_task_id,record", "", id, sqliteOptionalID(r.OwnerConversation), sqliteOptionalID(r.Owner), raw); err != nil {
				return err
			}
		case "append-entry":
			id, kind, fresh = w.Entry.ID, "entry", true
			r := after.Entries[id]
			raw, err := upstreamJSON(r)
			if err != nil {
				return err
			}
			if err := s.insertUpstreamRow("entries", "id,conversation_id,head,commit_seq,record", "", id, r.Conversation, sqliteOptionalID(r.Head), r.Seq, raw); err != nil {
				return err
			}
		case "put-task":
			id, kind = w.Task.ID, "task"
			if completed[id] {
				continue
			}
			completed[id] = true
			_, existed := before.Tasks[id]
			fresh = !existed
			r, err := CanonicalTask(after.Tasks[id], s.limits)
			if err != nil {
				return err
			}
			raw, err := upstreamJSON(r)
			if err != nil {
				return err
			}
			if err := s.insertUpstreamRow("tasks", "id,conversation_id,kind,status,abort_requested,background,record", "ON CONFLICT(id) DO UPDATE SET status=excluded.status,abort_requested=excluded.abort_requested,background=excluded.background,record=excluded.record", id, r.Conversation, upstreamIndexedString(r.Kind), r.State.Status, r.AbortRequested, r.Background, raw); err != nil {
				return err
			}
		case "put-submission":
			id, kind = w.Submission.ID, "submission"
			if completed[id] {
				continue
			}
			completed[id] = true
			_, existed := before.Submissions[id]
			fresh = !existed
			r := publicSubmission(after.Submissions[id])
			raw, err := upstreamJSON(r)
			if err != nil {
				return err
			}
			var request any
			if r.RequestIDPresent || r.RequestID != "" {
				request = upstreamIndexedString(r.RequestID)
			}
			if err := s.insertUpstreamRow("submissions", "id,conversation_id,request_id,status,record", "ON CONFLICT(id) DO UPDATE SET request_id=excluded.request_id,status=excluded.status,record=excluded.record", id, r.Conversation, request, r.Status, raw); err != nil {
				return err
			}
		case "put-document", "copy-document", "retire-document":
			documents[w.Document.ID] = true
		case "delta-document":
			documents[w.Delta.ID] = true
		default:
			return reject("unsupported upstream SQLite command")
		}
		if fresh {
			if err := s.insertUpstreamRow("record_ids", "id,record_type", "", id, kind); err != nil {
				return err
			}
		}
	}
	for _, id := range ids(documents) {
		doc := after.Documents[id]
		if _, existed := before.Documents[id]; !existed {
			if err := s.insertUpstreamRow("record_ids", "id,record_type", "", id, "document"); err != nil {
				return err
			}
		}
		record := struct {
			*DocumentCreate
			CreatedAt uint64 `json:"createdAt"`
			RetiredAt uint64 `json:"retiredAt,omitempty"`
		}{storageDocumentCreate(doc), doc.CreatedAt, doc.RetiredAt}
		raw, err := upstreamJSON(record)
		if err != nil {
			return err
		}
		if err = s.insertUpstreamRow("documents", "id,kind,family,key_value,scope_kind,owner_id,created_at,retired_at,record", "ON CONFLICT(id) DO UPDATE SET retired_at=excluded.retired_at,record=excluded.record", id, upstreamIndexedString(doc.Kind), documentFamily(doc), upstreamIndexedString(doc.Key), doc.Scope, doc.Owner, doc.CreatedAt, sqliteOptionalSeq(doc.RetiredAt), raw); err != nil {
			return err
		}
		revisions := after.DocumentRevisions[id]
		if documentHistory(doc) != "rewindable" && (doc.Retired || (len(revisions) > 0 && revisions[len(revisions)-1].Kind != "delta")) {
			if err := s.exec("DELETE FROM "+s.upstreamTable("document_revisions")+" WHERE document_id=?", id); err != nil {
				return err
			}
			if doc.Retired {
				continue
			}
		}
		for _, r := range revisions {
			if r.Seq <= before.Seq {
				continue
			}
			var value any = r.Value
			kind := r.Kind
			if kind == "delta" {
				value = r.Ops
			} else {
				kind = "base"
			}
			raw, err := upstreamJSON(value)
			if err != nil {
				return err
			}
			if err = s.insertUpstreamRow("document_revisions", "document_id,seq,kind,version,content", "", id, r.Seq, kind, r.Version, raw); err != nil {
				return err
			}
		}
	}
	return nil
}
