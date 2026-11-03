//go:build (darwin && (amd64 || arm64)) || (freebsd && (386 || amd64 || arm || arm64)) || (linux && (386 || amd64 || arm || arm64 || loong64 || ppc64le || riscv64 || s390x)) || (netbsd && amd64) || (openbsd && (amd64 || arm64)) || (windows && (386 || amd64 || arm64))

package durable

import (
	"database/sql/driver"

	"modernc.org/sqlite"
)

// Match the engine's generated platform sources. Secure lifetime file locking
// imposes additional restrictions; this constant does not promise native-store
// support on every driver target (or Go's Android/iOS platform-tag aliases).
const sqliteEngineAvailable = true

func newSQLiteConnector(dsn string) (driver.Connector, error) {
	return sqlite.NewConnector(dsn)
}
