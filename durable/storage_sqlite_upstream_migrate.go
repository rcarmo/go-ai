package durable

import "context"

// migrateUpstreamSQLite applies the upstream sleeping-status rebuild inside
// the writer's opening transaction. A viewer never calls this function.
func (s *SQLiteStorage) migrateUpstreamSQLite() error {
	schema := s.upstreamTable("durable_schema")
	var version int
	if err := s.db.QueryRowContext(context.Background(), "SELECT version FROM "+schema+" WHERE singleton=1").Scan(&version); err != nil {
		return err
	}
	if version == 2 {
		return nil
	}
	if version != 1 {
		return ErrCorrupt
	}
	tasks, rebuilt := s.upstreamTable("tasks"), s.upstreamTable("tasks_v2")
	columns := "id, conversation_id, kind, status, abort_requested, background, record"
	statements := []string{
		`CREATE TABLE ` + rebuilt + ` (
			id INTEGER PRIMARY KEY,
			conversation_id INTEGER NOT NULL,
			kind TEXT NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('pending','running','waiting','sleeping','completing','terminal')),
			abort_requested INTEGER NOT NULL CHECK (abort_requested IN (0,1)),
			background INTEGER NOT NULL CHECK (background IN (0,1)),
			record TEXT NOT NULL CHECK (json_valid(record))
		) STRICT`,
		"INSERT INTO " + rebuilt + " (" + columns + ") SELECT " + columns + " FROM " + tasks,
		"DROP TABLE " + tasks,
		"ALTER TABLE " + rebuilt + " RENAME TO " + tasks,
	}
	for _, index := range []struct{ name, column string }{
		{"status", "status"}, {"conversation", "conversation_id"}, {"kind", "kind"},
		{"abort_requested", "abort_requested"}, {"background", "background"},
	} {
		statements = append(statements, "CREATE INDEX "+s.upstreamTable("tasks_by_"+index.name)+" ON "+tasks+" ("+index.column+", id)")
	}
	statements = append(statements, "UPDATE "+schema+" SET version=2 WHERE singleton=1")
	for _, statement := range statements {
		if err := s.exec(statement); err != nil {
			return err
		}
	}
	return nil
}
