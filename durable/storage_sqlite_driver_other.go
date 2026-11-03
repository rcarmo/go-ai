//go:build !((darwin && (amd64 || arm64)) || (freebsd && (386 || amd64 || arm || arm64)) || (linux && (386 || amd64 || arm || arm64 || loong64 || ppc64le || riscv64 || s390x)) || (netbsd && amd64) || (openbsd && (amd64 || arm64)) || (windows && (386 || amd64 || arm64)))

package durable

import "database/sql/driver"

// Keep non-SQLite consumers buildable where the engine has no generated code.
// SQLite is unavailable there; no tests or acceptance requirements are skipped.
const sqliteEngineAvailable = false

func newSQLiteConnector(string) (driver.Connector, error) {
	return nil, ErrUnsupported
}
