package durable

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed storage_sqlite_schema_v152.json
var upstreamSQLiteSchema string

const upstreamSQLiteSchemaVersion = 2

// migrateUpstreamSQLite applies the extracted statements inside the writer's
// opening transaction before reading the migrated records. Viewers never call it.
func (s *SQLiteStorage) migrateUpstreamSQLite() error {
	ctx := context.Background()
	table := s.upstreamTable("durable_schema")
	if err := s.exec("INSERT OR IGNORE INTO " + table + " (singleton, version) VALUES (1, 0)"); err != nil {
		return err
	}
	var version int
	if err := s.db.QueryRowContext(ctx, "SELECT version FROM "+table+" WHERE singleton=1").Scan(&version); err != nil {
		return err
	}
	if version == upstreamSQLiteSchemaVersion {
		return nil
	}
	if version > upstreamSQLiteSchemaVersion {
		return fmt.Errorf("SQLite schema version %d is newer than supported version %d", version, upstreamSQLiteSchemaVersion)
	}
	if version < 0 {
		return ErrCorrupt
	}
	var migrations []struct {
		Version    int      `json:"version"`
		Statements []string `json:"statements"`
	}
	if err := json.Unmarshal([]byte(upstreamSQLiteSchema), &migrations); err != nil || len(migrations) != upstreamSQLiteSchemaVersion {
		return ErrCorrupt
	}
	for i, migration := range migrations {
		if migration.Version != i+1 {
			return ErrCorrupt
		}
		if migration.Version <= version {
			continue
		}
		for _, statement := range migration.Statements {
			if err := s.exec(strings.ReplaceAll(statement, "pi_schema_", s.prefix)); err != nil {
				return err
			}
		}
		if err := s.exec("UPDATE "+table+" SET version=? WHERE singleton=1", migration.Version); err != nil {
			return err
		}
	}
	return nil
}
