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
// SQL for original tables is already namespaced, and the database's original
// failure must reach the caller rather than be translated to a logical refusal.
func (s *SQLiteStorage) execUpstream(text string, args ...any) error {
	_, err := s.db.ExecContext(context.Background(), text, args...)
	return err
}

func (s *SQLiteStorage) Commit(ctx context.Context, batches [][]StorageWrite, receive func(int, uint64)) (CommitResult, error) {
	if !s.upstreamSchema {
		return s.storeCore.Commit(ctx, batches, receive)
	}
	return s.storeCore.commitPublic(ctx, batches, receive, func() (func() error, error) { return s.beginUpstreamPublicGroup(batches) })
}

func (s *SQLiteStorage) beginUpstreamPublicGroup(batches [][]StorageWrite) (func() error, error) {
	if err := s.execUpstream("BEGIN IMMEDIATE"); err != nil {
		return nil, &confirmedSQLiteAbort{cause: err}
	}
	if err := s.checkUpstreamMetadata(); err != nil {
		return nil, s.rollbackUpstream(err)
	}
	basis := s.publicWriteBasis(batches)
	if err := s.checkUpstreamWriteBasis(s.state, basis); err != nil {
		return nil, s.rollbackUpstream(err)
	}
	s.upstreamPublicTx = true
	return func() error {
		if !s.upstreamPublicTx {
			return nil
		}
		s.upstreamPublicTx = false
		// Empty or fully refused group: release the transaction without changing metadata.
		return s.execUpstream("COMMIT")
	}, nil
}

func (s *SQLiteStorage) rollbackUpstream(cause error) error {
	if rollback := s.execUpstream("ROLLBACK"); rollback != nil {
		return errors.Join(cause, rollback)
	}
	return &confirmedSQLiteAbort{cause: cause}
}

func (s *SQLiteStorage) appendUpstreamSeries(frames []storageFrame) error {
	preflight := s.upstreamPublicTx
	s.upstreamPublicTx = false
	if !preflight {
		if err := s.execUpstream("BEGIN IMMEDIATE"); err != nil {
			return &confirmedSQLiteAbort{cause: err}
		}
	}
	if err := s.storeUpstreamSeries(frames, preflight); err != nil {
		return s.rollbackUpstream(err)
	}
	if err := s.execUpstream("COMMIT"); err != nil {
		return err
	}
	s.upstreamHighWater = frames[len(frames)-1].high
	return nil
}

func (s *SQLiteStorage) checkUpstreamMetadata() error {
	table := s.upstreamTable("durable_metadata")
	var nextId string
	var nextSeq uint64
	if err := s.db.QueryRowContext(context.Background(), "SELECT next_id,next_seq FROM "+table+" WHERE singleton=1").Scan(&nextId, &nextSeq); err != nil {
		return err
	}
	if nextSeq != s.state.Seq+1 || nextId != strconv.FormatUint(s.upstreamHighWater+1, 10) {
		return reject("upstream SQLite prefix changed externally")
	}
	return nil
}

func (s *SQLiteStorage) storeUpstreamSeries(frames []storageFrame, preflight bool) error {
	base := s.state
	if !preflight {
		if err := s.checkUpstreamMetadata(); err != nil {
			return err
		}
	}
	records := make([]commitRecord, len(frames))
	basis := newSQLiteWriteBasis()
	for i, frame := range frames {
		if frame.kind == 1 {
			continue
		}
		if frame.kind != 2 {
			return ErrCorrupt
		}
		if err := decodeStrict(frame.payload, s.limits, s.limits.MaxFramePayloadBytes, &records[i]); err != nil {
			return err
		}
		if !preflight {
			basis.collect(base, records[i].Writes)
		}
	}
	if !preflight {
		if err := s.checkUpstreamWriteBasis(base, basis); err != nil {
			return err
		}
	}
	patch := newUpstreamWritePatch()
	for i, frame := range frames {
		if frame.kind == 1 {
			base.HighWater = frame.high
			continue
		}
		record := records[i]
		if batchHasDocumentCopy(record.Writes) {
			if err := s.flushUpstreamPatch(patch); err != nil {
				return err
			}
			patch = newUpstreamWritePatch()
			if err := s.checkUpstreamCopySources(base, record.Writes); err != nil {
				return err
			}
		}
		next, err := prepareExplicit(base, record, s.limits)
		if err != nil {
			return err
		}
		if next.HighWater != frame.high {
			return ErrCorrupt
		}
		if err := patch.collect(base, next, record.Writes, s.limits); err != nil {
			return err
		}
		base = next
	}
	if err := s.flushUpstreamPatch(patch); err != nil {
		return err
	}
	return s.execUpstream("UPDATE "+s.upstreamTable("durable_metadata")+" SET next_id=?,next_seq=? WHERE singleton=1", strconv.FormatUint(base.HighWater+1, 10), base.Seq+1)
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

// A group's SQL writes are emitted by table after all accepted parts have
// been checked. Content-only changes never rewrite document metadata.
type upstreamWritePatch struct {
	rows      map[string][][]any
	positions map[string]map[ID]int
	revisions map[ID][][]any
	cleared   map[ID]bool
}

func newUpstreamWritePatch() upstreamWritePatch {
	return upstreamWritePatch{rows: map[string][][]any{}, revisions: map[ID][][]any{}, cleared: map[ID]bool{}}
}

func (p *upstreamWritePatch) add(table string, values ...any) {
	p.rows[table] = append(p.rows[table], values)
}

// Upserts are last-write-wins across the group, before rows are partitioned
// by size. Keep the first occurrence's position for deterministic packing.
func (p *upstreamWritePatch) upsert(table string, id ID, cells ...any) {
	if p.positions == nil {
		p.positions = make(map[string]map[ID]int)
	}
	positions := p.positions[table]
	if positions == nil {
		positions = make(map[ID]int)
		p.positions[table] = positions
	}
	row := append([]any{id}, cells...)
	if i, ok := positions[id]; ok {
		p.rows[table][i] = row
		return
	}
	positions[id] = len(p.rows[table])
	p.add(table, row...)
}

func (p *upstreamWritePatch) clearRevisions(id ID) {
	p.cleared[id] = true
	delete(p.revisions, id)
}

func (p *upstreamWritePatch) revision(id ID, seq uint64, kind string, version uint64, value any) error {
	raw, err := upstreamJSON(value)
	if err != nil {
		return err
	}
	p.revisions[id] = append(p.revisions[id], []any{id, seq, kind, version, raw})
	return nil
}

func (p *upstreamWritePatch) collect(before, after Snapshot, writes []Write, l Limits) error {
	documents, bases := map[ID]bool{}, map[ID]bool{}
	completed := map[ID]bool{}
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
			p.add("conversations", id, sqliteOptionalID(r.OwnerConversation), sqliteOptionalID(r.Owner), raw)
		case "append-entry":
			id, kind, fresh = w.Entry.ID, "entry", true
			r := after.Entries[id]
			raw, err := upstreamJSON(r)
			if err != nil {
				return err
			}
			p.add("entries", id, r.Conversation, sqliteOptionalID(r.Head), r.Seq, raw)
		case "put-task":
			id, kind = w.Task.ID, "task"
			if completed[id] {
				continue
			}
			completed[id] = true
			_, existed := before.Tasks[id]
			fresh = !existed
			r, err := CanonicalTask(after.Tasks[id], l)
			if err != nil {
				return err
			}
			value, err := publicTaskRecordJSON(r)
			if err != nil {
				return err
			}
			// Encode the actual storage record deterministically; the same
			// final row must not vary with grouping or Go map iteration.
			encoded, err := encodeBoundedOrder(value, l, l.MaxRecordBytes, true)
			if err != nil {
				return err
			}
			raw := string(encoded)
			p.upsert("tasks", id, r.Conversation, upstreamIndexedString(r.Kind), r.State.Status, r.AbortRequested, r.Background, raw)
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
			p.upsert("submissions", id, r.Conversation, request, r.Status, raw)
		case "put-document", "copy-document":
			id = w.Document.ID
			documents[id], bases[id] = true, true
		case "retire-document":
			documents[w.Document.ID] = true
		case "delta-document":
			documents[w.Delta.ID] = true
		default:
			return reject("unsupported upstream SQLite command")
		}
		if fresh {
			p.add("record_ids", id, kind)
		}
	}
	for _, id := range ids(documents) {
		doc := after.Documents[id]
		prior, existed := before.Documents[id]
		if !existed {
			p.add("record_ids", id, "document")
		}
		if !existed || doc.RetiredAt != prior.RetiredAt {
			record := struct {
				*DocumentCreate
				CreatedAt uint64 `json:"createdAt"`
				RetiredAt uint64 `json:"retiredAt,omitempty"`
			}{storageDocumentCreate(doc), doc.CreatedAt, doc.RetiredAt}
			raw, err := upstreamJSON(record)
			if err != nil {
				return err
			}
			p.upsert("documents", id, upstreamIndexedString(doc.Kind), documentFamily(doc), upstreamIndexedString(doc.Key), doc.Scope, doc.Owner, doc.CreatedAt, sqliteOptionalSeq(doc.RetiredAt), raw)
		}
		if documentHistory(doc) != "rewindable" {
			if doc.Retired || bases[id] {
				p.clearRevisions(id)
			}
			if doc.Retired {
				continue
			}
			if bases[id] {
				// The native current-only image may retain only the current base
				// value. The original schema always stores an explicit revision.
				if err := p.revision(id, after.Seq, "base", doc.Version, doc.Value); err != nil {
					return err
				}
				continue
			}
		}
		for _, r := range after.DocumentRevisions[id] {
			if r.Seq <= before.Seq {
				continue
			}
			if r.Kind == "delta" {
				if err := p.revision(id, r.Seq, "delta", r.Version, r.Ops); err != nil {
					return err
				}
			} else if err := p.revision(id, r.Seq, "base", r.Version, r.Value); err != nil {
				return err
			}
		}
	}
	return nil
}
