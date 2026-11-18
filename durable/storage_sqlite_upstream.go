package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"unicode/utf8"
)

// The prefix is validated at OpenSQLite before any SQL is constructed. Table
// suffixes below are fixed identifiers from the upstream migration set.
func (s *SQLiteStorage) upstreamTable(name string) string { return `"` + s.prefix + name + `"` }

func (s *SQLiteStorage) hasUpstreamSQLiteSchema() (bool, error) {
	var found int
	err := s.db.QueryRowContext(context.Background(), "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='table' AND name=?)", s.prefix+"durable_schema").Scan(&found)
	return found != 0, err
}

// readUpstreamSQLitePrefix projects source schema v1/v2 into the public read
// image. The caller holds one SQLite read transaction. This neither initializes
// nor upgrades foreign databases, even when a known migration is pending.
func (s *SQLiteStorage) readUpstreamSQLitePrefix() error {
	ctx := context.Background()
	var version uint64
	if err := s.db.QueryRowContext(ctx, "SELECT version FROM "+s.upstreamTable("durable_schema")+" WHERE singleton=1").Scan(&version); err != nil || version < 1 || version > upstreamSQLiteSchemaVersion {
		return fmt.Errorf("durable: unsupported upstream SQLite schema: %w", ErrCorrupt)
	}
	var nextID string
	var nextSeq uint64
	if err := s.db.QueryRowContext(ctx, "SELECT next_id,next_seq FROM "+s.upstreamTable("durable_metadata")+" WHERE singleton=1").Scan(&nextID, &nextSeq); err != nil {
		return fmt.Errorf("durable: read upstream SQLite metadata: %w", err)
	}
	next, err := strconv.ParseUint(nextID, 10, 64)
	if err != nil || next < 2 || next > MaxID+1 || nextSeq == 0 || nextSeq > MaxID+1 {
		return ErrCorrupt
	}
	s.state.HighWater, s.state.Seq = next-1, nextSeq-1
	state, l := &s.state, s.limits
	ids := make(map[ID]struct{})
	var bytesRead int64
	charge := func(raw []byte) error {
		bytesRead += int64(len(raw))
		if len(raw) > l.MaxRecordBytes || bytesRead > l.MaxRetainedBytes || !utf8.Valid(raw) {
			return ErrCorrupt
		}
		return nil
	}
	claim := func(id ID, raw []byte) error {
		if _, exists := ids[id]; id == 0 || uint64(id) >= next || exists || len(ids) >= l.MaxRecords {
			return ErrCorrupt
		}
		ids[id] = struct{}{}
		return charge(raw)
	}
	tables := []struct {
		name string
		read func(ID, []byte, uint64) error
	}{
		{"conversations", func(id ID, raw []byte, _ uint64) error {
			var record Conversation
			if err := json.Unmarshal(raw, &record); err != nil || record.ID != id {
				return ErrCorrupt
			}
			state.Conversations[id] = record
			return nil
		}},
		{"entries", func(id ID, raw []byte, seq uint64) error {
			var record Entry
			if err := json.Unmarshal(raw, &record); err != nil || record.ID != id || seq == 0 || seq >= nextSeq {
				return ErrCorrupt
			}
			record.Seq = seq
			state.Entries[id] = nativeEntryDiagnosticData(record)
			return nil
		}},
		{"tasks", func(id ID, raw []byte, _ uint64) error {
			var record TaskRecord
			if err := json.Unmarshal(raw, &record); err != nil || record.ID != id {
				return ErrCorrupt
			}
			stored, err := nativeTask(record, l)
			if err == nil {
				state.Tasks[id] = stored
			}
			return err
		}},
		{"submissions", func(id ID, raw []byte, _ uint64) error {
			var record SubmissionRecord
			if err := json.Unmarshal(raw, &record); err != nil || record.ID != id {
				return ErrCorrupt
			}
			stored, err := nativeSubmission(record, l)
			if err == nil {
				state.Submissions[id] = stored
			}
			return err
		}},
		{"documents", func(id ID, raw []byte, _ uint64) error {
			var record struct {
				DocumentCreate
				CreatedAt uint64 `json:"createdAt"`
				RetiredAt uint64 `json:"retiredAt"`
			}
			if err := json.Unmarshal(raw, &record); err != nil || record.ID != id || record.CreatedAt == 0 || record.CreatedAt >= nextSeq || record.RetiredAt >= nextSeq || (record.RetiredAt != 0 && record.RetiredAt < record.CreatedAt) {
				return ErrCorrupt
			}
			if err := record.DocumentCreate.validate(); err != nil {
				return err
			}
			doc := Document{ID: id, Scope: record.Scope.Kind, Kind: record.Kind, History: record.History, Fork: record.Fork, CreatedAt: record.CreatedAt, RetiredAt: record.RetiredAt, Retired: record.RetiredAt != 0}
			if record.Key != nil {
				doc.Key, doc.Family = *record.Key, true
			}
			switch doc.Scope {
			case "conversation":
				doc.Owner = record.Scope.ConversationID
			case "task":
				doc.Owner = record.Scope.TaskID
			}
			state.Documents[id] = doc
			return nil
		}},
	}
	for _, table := range tables {
		seq := "0"
		if table.name == "entries" {
			seq = "commit_seq"
		}
		query := "SELECT id,record," + seq + " FROM " + s.upstreamTable(table.name)
		if err := s.eachUpstreamSQLiteRow(query, func(scan sqliteRowScan) error {
			var id ID
			var raw []byte
			var seq uint64
			if err := scan(&id, &raw, &seq); err != nil {
				return err
			}
			if err := claim(id, raw); err != nil {
				return err
			}
			return table.read(id, raw, seq)
		}); err != nil {
			return fmt.Errorf("durable: read upstream SQLite %s: %w", table.name, err)
		}
	}
	state.DocumentRevisions = make(map[ID][]DocumentRevision)
	query := "SELECT document_id,seq,kind,version,content FROM " + s.upstreamTable("document_revisions") + " ORDER BY document_id,seq"
	if err := s.eachUpstreamSQLiteRow(query, func(scan sqliteRowScan) error {
		var id ID
		var r DocumentRevision
		var raw []byte
		if err := scan(&id, &r.Seq, &r.Kind, &r.Version, &raw); err != nil {
			return err
		}
		doc, exists := state.Documents[id]
		if !exists || r.Seq < doc.CreatedAt || r.Seq >= nextSeq || r.Version == 0 || r.Version > MaxID || (doc.Retired && r.Seq > doc.RetiredAt) {
			return ErrCorrupt
		}
		if err := charge(raw); err != nil {
			return err
		}
		switch r.Kind {
		case "base":
			if err := decodeStrict(raw, l, l.MaxDocumentBytes, &r.Value); err != nil || r.Value == nil {
				return ErrCorrupt
			}
		case "delta":
			if err := decodeStrictRoot(raw, l, l.MaxRecordBytes, &r.Ops, '['); err != nil || r.Ops == nil {
				return ErrCorrupt
			}
		default:
			return ErrCorrupt
		}
		state.DocumentRevisions[id] = append(state.DocumentRevisions[id], r)
		return nil
	}); err != nil {
		return fmt.Errorf("durable: read upstream SQLite document revisions: %w", err)
	}
	for id, doc := range state.Documents {
		if doc.Retired {
			continue
		}
		if len(state.DocumentRevisions[id]) == 0 {
			return ErrCorrupt
		}
		value, found, err := materializeDocument(*state, id, CurrentDocumentPoint(), l)
		if err != nil || !found {
			return fmt.Errorf("durable: materialize upstream SQLite document %d: %w", id, errorsOrCorrupt(err))
		}
		state.Documents[id] = value
	}
	return nil
}

type sqliteRowScan func(...any) error

func (s *SQLiteStorage) eachUpstreamSQLiteRow(query string, visit func(sqliteRowScan) error) error {
	rows, err := s.db.QueryContext(context.Background(), query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := visit(rows.Scan); err != nil {
			return err
		}
	}
	return rows.Err()
}

func errorsOrCorrupt(err error) error {
	if err == nil {
		return ErrCorrupt
	}
	return err
}
