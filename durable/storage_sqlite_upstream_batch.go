package durable

import (
	"math/bits"
	"strconv"
	"strings"
)

func (s *SQLiteStorage) flushUpstreamPatch(p upstreamWritePatch) error {
	for _, table := range []struct{ name, columns, conflict string }{
		{"record_ids", "id,record_type", ""},
		{"conversations", "id,owner_conversation_id,owner_task_id,record", ""},
		{"entries", "id,conversation_id,head,commit_seq,record", ""},
		{"tasks", "id,conversation_id,kind,status,abort_requested,background,record", "ON CONFLICT(id) DO UPDATE SET status=excluded.status,abort_requested=excluded.abort_requested,background=excluded.background,record=excluded.record"},
		{"submissions", "id,conversation_id,request_id,status,record", "ON CONFLICT(id) DO UPDATE SET request_id=excluded.request_id,status=excluded.status,record=excluded.record"},
		{"documents", "id,kind,family,key_value,scope_kind,owner_id,created_at,retired_at,record", "ON CONFLICT(id) DO UPDATE SET retired_at=excluded.retired_at,record=excluded.record"},
	} {
		if rows := p.rows[table.name]; len(rows) > 0 {
			prefix := "INSERT INTO " + s.upstreamTable(table.name) + " (" + table.columns + ") "
			if err := s.insertUpstreamRows(prefix, table.conflict, rows); err != nil {
				return err
			}
		}
	}
	cleared := ids(p.cleared)
	for len(cleared) > 0 {
		// These are at most sixteen ASCII digits per ID; this count keeps
		// the whole JSON parameter comfortably below the byte cap.
		n := min(len(cleared), sqliteFrameArrayBytes/17)
		raw, err := upstreamJSON(cleared[:n])
		if err != nil {
			return err
		}
		if err := s.exec("DELETE FROM "+s.upstreamTable("document_revisions")+" WHERE document_id IN (SELECT value FROM json_each(?))", raw); err != nil {
			return err
		}
		cleared = cleared[n:]
	}
	var revisions [][]any
	for _, id := range ids(p.revisions) {
		revisions = append(revisions, p.revisions[id]...)
	}
	if len(revisions) == 0 {
		return nil
	}
	return s.insertUpstreamRows("INSERT INTO "+s.upstreamTable("document_revisions")+" (document_id,seq,kind,version,content) ", "", revisions)
}

// Compact rows use fixed SQL with a bounded JSON argument. Large rows bind
// columns individually in power-of-two shapes, as in the pinned source.
func (s *SQLiteStorage) insertUpstreamRows(prefix, conflict string, rows [][]any) error {
	width := len(rows[0])
	compact := make([]string, width)
	for i := range width {
		compact[i] = "value ->> " + strconv.Itoa(i)
	}
	compactSQL := prefix + "SELECT " + strings.Join(compact, ",") + " FROM json_each(?) WHERE 1 ORDER BY key " + conflict
	values := "(?" + strings.Repeat(",?", width-1) + ")"
	large := func(row []any) bool {
		text := 0
		for _, val := range row {
			if str, ok := val.(string); ok {
				text += len(str)
			}
		}
		return text > sqliteCompactFrameBytes
	}
	var buf strings.Builder
	for len(rows) > 0 {
		if large(rows[0]) {
			n := 1
			for n < len(rows) && n < sqliteFrameParameters/width && large(rows[n]) {
				n++
			}
			n = 1 << (bits.Len(uint(n)) - 1)
			args := make([]any, 0, width*n)
			for _, row := range rows[:n] {
				args = append(args, row...)
			}
			if err := s.exec(prefix+"VALUES "+values+strings.Repeat(","+values, n-1)+" "+conflict, args...); err != nil {
				return err
			}
			rows = rows[n:]
			continue
		}
		buf.Reset()
		buf.WriteByte('[')
		for len(rows) > 0 && !large(rows[0]) {
			raw, err := upstreamJSON(rows[0])
			if err != nil {
				return err
			}
			if buf.Len()+len(raw)+2 > sqliteFrameArrayBytes {
				break
			}
			if buf.Len() > 1 {
				buf.WriteByte(',')
			}
			buf.WriteString(raw)
			rows = rows[1:]
		}
		buf.WriteByte(']')
		if err := s.exec(compactSQL, buf.String()); err != nil {
			return err
		}
	}
	return nil
}
