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
type sqliteConfig struct {
	Version uint64 `json:"version"`
	Sync    bool   `json:"sync"`
	Limits  Limits `json:"limits"`
}
