package durable

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
)

// Check the SQLite snapshot of the keys the staged group depended on before
// flushing any write. The core retains its validation projection, so a changed
// row must not silently authorize a write based on stale document state.
type sqliteWriteBasis struct {
	claims, documents, deltas map[ID]bool
	addresses                 []DocumentAddress
	seenAddresses             map[DocumentAddress]bool
}

func (b *sqliteWriteBasis) collect(before Snapshot, writes []Write) {
	for _, w := range writes {
		switch w.Op {
		case "create-conversation":
			b.claims[w.Conversation.ID] = true
		case "append-entry":
			b.claims[w.Entry.ID] = true
		case "put-task":
			b.claims[w.Task.ID] = true
		case "put-submission":
			b.claims[w.Submission.ID] = true
		case "put-document", "copy-document":
			d := *w.Document
			b.documents[d.ID] = true
			if _, exists := before.Documents[d.ID]; !exists {
				b.claims[d.ID] = true
				addr := documentAddress(d)
				if !b.seenAddresses[addr] {
					b.seenAddresses[addr] = true
					b.addresses = append(b.addresses, addr)
				}
			}
		case "retire-document":
			b.documents[w.Document.ID] = true
		case "delta-document":
			b.documents[w.Delta.ID] = true
			b.deltas[w.Delta.ID] = true
		}
	}
}

func (s *SQLiteStorage) checkUpstreamWriteBasis(base Snapshot, b sqliteWriteBasis) error {
	kindAt := func(id ID) string {
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
	missing := 0
	for id := range b.claims {
		if kindAt(id) != "" {
			missing++
		}
	}
	idErr := errors.New("upstream SQLite record IDs changed externally")
	if err := queryUpstreamParts(s, "SELECT id, record_type FROM "+s.upstreamTable("record_ids")+" WHERE id IN (SELECT value FROM json_each(?))", ids(b.claims), func(rows *sql.Rows) error {
		var id ID
		var kind string
		if err := rows.Scan(&id, &kind); err != nil {
			return err
		}
		if kindAt(id) != kind {
			return idErr
		}
		missing--
		return nil
	}); err != nil {
		return err
	}
	if missing != 0 {
		return idErr
	}
	missing = 0
	for id := range b.documents {
		if _, ok := base.Documents[id]; ok {
			missing++
		}
	}
	docErr := errors.New("upstream SQLite document metadata changed externally")
	if err := queryUpstreamParts(s, "SELECT record FROM "+s.upstreamTable("documents")+" WHERE id IN (SELECT value FROM json_each(?))", ids(b.documents), func(rows *sql.Rows) error {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		var r struct {
			DocumentCreate
			CreatedAt uint64 `json:"createdAt"`
			RetiredAt uint64 `json:"retiredAt"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return err
		}
		d, ok := base.Documents[r.ID]
		if !ok || d.CreatedAt != r.CreatedAt || d.RetiredAt != r.RetiredAt || !reflect.DeepEqual(storageDocumentCreate(d), &r.DocumentCreate) {
			return docErr
		}
		missing--
		return nil
	}); err != nil {
		return err
	}
	if missing != 0 {
		return docErr
	}
	if err := queryUpstreamParts(s, "SELECT requested.value AS document_id, (SELECT version FROM "+s.upstreamTable("document_revisions")+" WHERE document_id = requested.value ORDER BY seq DESC LIMIT 1) AS version FROM json_each(?) AS requested", ids(b.deltas), func(rows *sql.Rows) error {
		var id ID
		var version sql.NullInt64
		if err := rows.Scan(&id, &version); err != nil {
			return err
		}
		var want uint64
		if doc, ok := base.Documents[id]; ok && !doc.Retired {
			want = doc.Version
		} else if revisions := base.DocumentRevisions[id]; len(revisions) > 0 {
			want = revisions[len(revisions)-1].Version
		}
		if version.Valid != (want != 0) || uint64(version.Int64) != want {
			return errors.New("upstream SQLite document version changed externally")
		}
		return nil
	}); err != nil {
		return err
	}
	if len(b.addresses) == 0 {
		return nil
	}
	type columns struct {
		kind, scope string
		owner       ID
		family      bool
		key         string
	}
	addressCols := func(a DocumentAddress) columns {
		return columns{upstreamIndexedString(a.Kind), a.Scope, a.Owner, a.Family, upstreamIndexedString(a.Key)}
	}
	current := make(map[columns]ID)
	for id, d := range base.Documents {
		if !d.Retired && b.seenAddresses[documentAddress(d)] {
			current[addressCols(documentAddress(d))] = id
		}
	}
	addresses := make([][]any, 0, len(b.addresses))
	for _, a := range b.addresses {
		p := addressCols(a)
		family := 0
		if p.family {
			family = 1
		}
		addresses = append(addresses, []any{p.kind, p.scope, p.owner, family, p.key})
	}
	addressErr := errors.New("upstream SQLite document address changed externally")
	if err := queryUpstreamParts(s, `SELECT document.id, document.kind, document.scope_kind, document.owner_id, document.family, document.key_value
 FROM json_each(?) AS address JOIN `+s.upstreamTable("documents")+` AS document
 ON document.kind = json_extract(address.value, '$[0]') AND document.scope_kind = json_extract(address.value, '$[1]')
 AND document.owner_id = json_extract(address.value, '$[2]') AND document.family = json_extract(address.value, '$[3]')
 AND document.key_value = json_extract(address.value, '$[4]') WHERE document.retired_at IS NULL`, addresses, func(rows *sql.Rows) error {
		var id ID
		var p columns
		if err := rows.Scan(&id, &p.kind, &p.scope, &p.owner, &p.family, &p.key); err != nil {
			return err
		}
		if current[p] != id {
			return addressErr
		}
		delete(current, p)
		return nil
	}); err != nil {
		return err
	}
	if len(current) != 0 {
		return addressErr
	}
	return nil
}

// Like the source's readRows, this uses one reusable SQL text per key set and
// bounds its JSON arrays by UTF-16 length, including delimiters.
func queryUpstreamParts[T any](s *SQLiteStorage, text string, parts []T, visit func(*sql.Rows) error) error {
	var raw strings.Builder
	used := 0
	query := func() error {
		if used == 0 {
			return nil
		}
		raw.WriteByte(']')
		rows, err := s.db.QueryContext(context.Background(), text, raw.String())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := visit(rows); err != nil {
				return err
			}
		}
		raw.Reset()
		used = 0
		return rows.Err()
	}
	for _, part := range parts {
		p, err := upstreamJSON(part)
		if err != nil {
			return err
		}
		length := sqliteTextUnits(p)
		if used > 0 && used+1+length > sqliteArrayUnits {
			if err := query(); err != nil {
				return err
			}
		}
		if used == 0 {
			raw.WriteByte('[')
			used = 2
		} else {
			raw.WriteByte(',')
			used++
		}
		raw.WriteString(p)
		used += length
	}
	return query()
}
