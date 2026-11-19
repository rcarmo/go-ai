package durable

import (
	"context"
	"encoding/json"
	"fmt"
)

// Copy sources read the actual transaction, including prior batches flushed
// by the same commit. Verify the already staged image before publishing it.
func (s *SQLiteStorage) checkUpstreamCopySources(base Snapshot, writes []Write) error {
	for _, w := range writes {
		if w.Op != "copy-document" {
			continue
		}
		if w.Source == nil {
			return ErrCorrupt
		}
		got, found, err := s.readUpstreamCopySource(*w.Source)
		if err != nil {
			return err
		}
		want, exists, err := materializeDocument(base, w.Source.ID, w.Source.At, s.limits)
		if err != nil {
			return err
		}
		if !found || !exists || !sameAddress(got, want) || documentHistory(got) != documentHistory(want) || documentFork(got) != documentFork(want) || got.Version != want.Version || !equalJSONValue(got.Value, want.Value) {
			return fmt.Errorf("upstream SQLite copy source %d differs from staged document", w.Source.ID)
		}
	}
	return nil
}

func (s *SQLiteStorage) readUpstreamCopySource(source DocumentCopySource) (d Document, found bool, err error) {
	ctx := context.Background()
	// Use one bounded primary-key lookup; distinguish absence from a query failure.
	rows, err := s.db.QueryContext(ctx, "SELECT record FROM "+s.upstreamTable("documents")+" WHERE id = ?", source.ID)
	if err != nil {
		return d, false, err
	}
	var raw []byte
	if rows.Next() {
		err = rows.Scan(&raw)
	} else {
		err = rows.Err()
	}
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	if err != nil || raw == nil {
		return d, false, err
	}
	var record struct {
		DocumentCreate
		CreatedAt uint64 `json:"createdAt"`
		RetiredAt uint64 `json:"retiredAt"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return d, false, err
	}
	if record.ID != source.ID {
		return d, false, ErrCorrupt
	}
	d, err = nativeDocumentAddress(record.DocumentCreate, 1, JSON{})
	if err != nil {
		return d, false, err
	}
	d.CreatedAt, d.RetiredAt, d.Retired = record.CreatedAt, record.RetiredAt, record.RetiredAt != 0
	if !source.At.Current && documentHistory(d) != "rewindable" {
		return d, false, storageRequest("Document " + idText(source.ID) + " does not retain historical content")
	}
	if !aliveDocument(d, source.At) {
		return d, false, nil
	}
	upper := source.At.Seq
	if source.At.Current {
		upper = MaxID
	}
	table := s.upstreamTable("document_revisions")
	var seq uint64
	if err := s.db.QueryRowContext(ctx, "SELECT seq, version, content FROM "+table+" WHERE document_id = ? AND kind = 'base' AND seq <= ? ORDER BY seq DESC LIMIT 1", source.ID, upper).Scan(&seq, &d.Version, &raw); err != nil {
		return d, false, err
	}
	if err := decodeStrict(raw, s.limits, s.limits.MaxDocumentBytes, &d.Value); err != nil {
		return d, false, err
	}
	tail, err := s.db.QueryContext(ctx, "SELECT kind, version, content FROM "+table+" WHERE document_id = ? AND seq > ? AND seq <= ? ORDER BY seq", source.ID, seq, upper)
	if err != nil {
		return d, false, err
	}
	defer tail.Close()
	for tail.Next() {
		var kind string
		var version uint64
		if err := tail.Scan(&kind, &version, &raw); err != nil {
			return d, false, err
		}
		if kind != "delta" || version != d.Version {
			return d, false, ErrCorrupt
		}
		var ops []Operation
		if err := decodeStrictRoot(raw, s.limits, s.limits.MaxRecordBytes, &ops, '['); err != nil {
			return d, false, err
		}
		value, err := ApplyOperations(d.Value, ops, s.limits)
		if err != nil {
			return d, false, err
		}
		obj, ok := value.(map[string]any)
		if !ok {
			return d, false, ErrCorrupt
		}
		d.Value = JSON(obj)
		d.DeltasSinceBase++
	}
	if err := tail.Err(); err != nil {
		return d, false, err
	}
	return d, true, nil
}

func batchHasDocumentCopy(writes []Write) bool {
	for _, w := range writes {
		if w.Op == "copy-document" {
			return true
		}
	}
	return false
}
