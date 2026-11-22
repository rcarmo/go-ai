package durable

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type sqliteCopyRejected struct {
	target ID
	cause  error
}

func (e *sqliteCopyRejected) Error() string {
	return "Document copy " + idText(e.target) + " was rejected"
}
func (e *sqliteCopyRejected) Unwrap() error { return e.cause }

// Copy sources read the actual transaction, including prior batches flushed
// by the same commit. Verify the already staged image before publishing it.
// Separate database failures from materialization errors, even when both
// happen to carry the same Go error type. Expose only the original cause.
type sqliteCopyReadFailure struct{ error }

func (s *SQLiteStorage) checkUpstreamCopySources(base Snapshot, writes []Write) error {
	for _, w := range writes {
		if w.Op != "copy-document" {
			continue
		}
		if w.Source == nil || w.Document == nil {
			return ErrCorrupt
		}
		got, found, err := s.readUpstreamCopySource(*w.Source)
		if err != nil {
			var read *sqliteCopyReadFailure
			if errors.As(err, &read) {
				err = read.error
			}
			return &sqliteCopyRejected{target: w.Document.ID, cause: err}
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
		return d, false, &sqliteCopyReadFailure{err}
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
	if err != nil {
		return d, false, &sqliteCopyReadFailure{err}
	}
	if raw == nil {
		return d, false, nil
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
		if err == sql.ErrNoRows {
			return d, false, nil
		}
		return d, false, &sqliteCopyReadFailure{err}
	}
	if err := decodeStrict(raw, s.limits, s.limits.MaxDocumentBytes, &d.Value); err != nil {
		return d, false, err
	}
	tail, err := s.db.QueryContext(ctx, "SELECT kind, version, content FROM "+table+" WHERE document_id = ? AND seq > ? AND seq <= ? ORDER BY seq", source.ID, seq, upper)
	if err != nil {
		return d, false, &sqliteCopyReadFailure{err}
	}
	defer tail.Close()
	for tail.Next() {
		var kind string
		var version uint64
		if err := tail.Scan(&kind, &version, &raw); err != nil {
			return d, false, &sqliteCopyReadFailure{err}
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
		return d, false, &sqliteCopyReadFailure{err}
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

// A failed native copy may not have reached the SQL lookup. Validate its
// shape without reading any sources: copy creates have the same claims and
// destination addresses as ordinary creates. The placeholder never settles.
func (s *SQLiteStorage) checkRefusedCopyStructure(base Snapshot, batch Batch, ordinal uint64) error {
	if err := validateBatchJSON(batch, s.limits); err != nil {
		return err
	}
	changed := map[ID]bool{}
	for _, w := range batch.Writes {
		if w.Document != nil {
			changed[w.Document.ID] = true
		}
		if w.Delta != nil {
			changed[w.Delta.ID] = true
		}
	}
	shape := append([]Write(nil), batch.Writes...)
	for i, w := range shape {
		if w.Op != "copy-document" {
			continue
		}
		if changed[w.Source.ID] {
			return reject("Document copy " + idText(w.Document.ID) + " source is changed in the copy batch")
		}
		target := *w.Document
		target.Version, target.Value = 1, JSON{}
		shape[i] = Write{Op: "put-document", Document: &target}
	}
	_, _, err := s.prepareCommit(base, Batch{Writes: shape}, ordinal, false)
	return err
}

func (s *SQLiteStorage) checkRefusedCopySources(writes []Write) (refusal, failure error) {
	for _, w := range writes {
		if w.Op != "copy-document" {
			continue
		}
		source, found, err := s.readUpstreamCopySource(*w.Source)
		if err != nil {
			var read *sqliteCopyReadFailure
			if errors.As(err, &read) {
				return nil, &sqliteCopyRejected{target: w.Document.ID, cause: read.error}
			}
			return &sqliteCopyRejected{target: w.Document.ID, cause: err}, nil
		}
		if !found {
			return &sqliteCopyRejected{target: w.Document.ID, cause: fmt.Errorf("Fork source document %d cannot be read", w.Source.ID)}, nil
		}
		target := w.Document
		if target.Scope != "conversation" || source.Scope != "conversation" || target.Kind != source.Kind || target.Key != source.Key || documentFamily(*target) != documentFamily(source) || documentHistory(*target) != documentHistory(source) || documentFork(*target) != documentFork(source) {
			return &sqliteCopyRejected{target: target.ID, cause: fmt.Errorf("Fork source document %d does not match the copied record", w.Source.ID)}, nil
		}
	}
	return nil, nil
}
