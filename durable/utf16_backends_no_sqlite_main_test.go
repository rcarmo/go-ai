//go:build !cgo

package durable

import "testing"

// Memory/journal coverage remains in the shared helper; SQLite is not built.
func sqliteUTF16Backends(*testing.T, func(*testing.T, backend))  {}
func sqliteUTF16ReopenStore(*testing.T, Storage) (Storage, bool) { return nil, false }
