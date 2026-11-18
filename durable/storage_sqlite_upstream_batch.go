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
		{"tasks", "id,conversation_id,kind,status,abort_requested,background,record", "ON CONFLICT(id) DO UPDATE SET conversation_id=excluded.conversation_id,kind=excluded.kind,status=excluded.status,abort_requested=excluded.abort_requested,background=excluded.background,record=excluded.record"},
		{"submissions", "id,conversation_id,request_id,status,record", "ON CONFLICT(id) DO UPDATE SET conversation_id=excluded.conversation_id,request_id=excluded.request_id,status=excluded.status,record=excluded.record"},
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
		if err := s.execUpstream("DELETE FROM "+s.upstreamTable("document_revisions")+" WHERE document_id IN (SELECT value FROM json_each(?))", raw); err != nil {
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

// Source-layout statements use JS string lengths, unlike the byte limits of
// the legacy frame transport.
const (
	sqliteRowTextUnits    = 8 * 1024
	sqliteArrayUnits      = 512 * 1024
	sqliteBoundParameters = 100
)

func sqliteTextUnits(s string) int {
	if len(s) == 0 {
		return 0
	}
	n := 0
	for _, r := range s {
		n++
		if r > 0xffff {
			n++
		}
	}
	return n
}

// Compact rows are pooled by table before large rows bind individual columns.
func (s *SQLiteStorage) insertUpstreamRows(prefix, conflict string, rows [][]any) error {
	width := len(rows[0])
	compact := make([]string, width)
	for i := range width {
		compact[i] = "json_extract(item.value, '$[" + strconv.Itoa(i) + "]')"
	}
	compactSQL := prefix + "SELECT " + strings.Join(compact, ", ") + " FROM json_each(?) AS item WHERE true " + conflict
	var buf strings.Builder
	used := 0
	flush := func() error {
		if used == 0 {
			return nil
		}
		buf.WriteByte(']')
		if err := s.execUpstream(compactSQL, buf.String()); err != nil {
			return err
		}
		buf.Reset()
		used = 0
		return nil
	}
	var large [][]any
	for _, row := range rows {
		text := 0
		for _, val := range row {
			if str, ok := val.(string); ok {
				text += sqliteTextUnits(str)
			}
		}
		if text > sqliteRowTextUnits {
			large = append(large, row)
			continue
		}
		raw, err := upstreamJSON(row)
		if err != nil {
			return err
		}
		size := sqliteTextUnits(raw)
		if used > 0 && used+1+size > sqliteArrayUnits {
			if err := flush(); err != nil {
				return err
			}
		}
		if used == 0 {
			buf.WriteByte('[')
			used = 2
		} else {
			buf.WriteByte(',')
			used++
		}
		buf.WriteString(raw)
		used += size
	}
	if err := flush(); err != nil {
		return err
	}
	values := "(?" + strings.Repeat(", ?", width-1) + ")"
	for len(large) > 0 {
		maxRows := min(len(large), sqliteBoundParameters/width)
		n := 1 << (bits.Len(uint(maxRows)) - 1)
		args := make([]any, 0, n*width)
		for _, row := range large[:n] {
			args = append(args, row...)
		}
		if err := s.execUpstream(prefix+"VALUES "+values+strings.Repeat(", "+values, n-1)+" "+conflict, args...); err != nil {
			return err
		}
		large = large[n:]
	}
	return nil
}
