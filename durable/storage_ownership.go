package durable

import "sync"

// File-backed SQLite stores retain process ownership until durable close joins.
var storageOwners = struct {
	sync.Mutex
	paths map[string]bool
}{paths: map[string]bool{}}

func ownPath(path string) error {
	storageOwners.Lock()
	defer storageOwners.Unlock()
	if storageOwners.paths[path] {
		return ErrOwned
	}
	storageOwners.paths[path] = true
	return nil
}
func releasePath(path string) {
	storageOwners.Lock()
	delete(storageOwners.paths, path)
	storageOwners.Unlock()
}

// Keep the persisted SQLite configuration shape readable without retaining
// the removed file-journal implementation. Sync is always true for SQLite.
// sqliteConfigVersion is the format written by this build. Version 1 files
// predate the raw/fresh root split and persisted no root conversation write, so
// the reserved root stays implicit for them; version 2 stores start raw and
// empty, with the reserved root bootstrapped by its owner.
const sqliteConfigVersion = 2

type sqliteConfig struct {
	Version uint64 `json:"version"`
	Sync    bool   `json:"sync"`
	Limits  Limits `json:"limits"`
}
