package durable

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSQLiteNativeConformance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.sqlite")
	var current *SQLiteStorage
	open := func() *SQLiteStorage {
		t.Helper()
		store, err := OpenSQLite(path, SQLiteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return store
	}
	current = open()
	t.Cleanup(func() { current.Close(bg) })
	runStorageConformance(t, sqliteFixture{store: current, reopen: func() Storage {
		if err := current.Close(bg); err != nil {
			t.Fatal(err)
		}
		current = open()
		return current
	}})
}
func TestSQLiteCrossProcessOwnership(t *testing.T) {
	if path := os.Getenv("DURABLE_SQLITE_LOCK_TEST"); path != "" {
		store, err := OpenSQLite(path, SQLiteOptions{})
		if store != nil {
			store.Close(bg)
		}
		if !errors.Is(err, ErrOwned) {
			t.Fatalf("cross-process opener was not excluded: %v", err)
		}
		return
	}
	path := filepath.Join(t.TempDir(), "session.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	profiles := sqliteChildProfileDirectory(t)
	command := exec.Command(os.Args[0], "-test.run=^TestSQLiteCrossProcessOwnership$")
	if profiles != "" {
		command.Args = append(command.Args, "-test.cpuprofile="+filepath.Join(profiles, "cpu.pprof"), "-test.memprofile="+filepath.Join(profiles, "heap.pprof"))
	}
	command.Env = append(os.Environ(), "DURABLE_SQLITE_LOCK_TEST="+path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("ownership subprocess: %v\n%s", err, output)
	}
	analyseSQLiteChildProfiles(t, profiles)
}
func TestSQLiteDirectoryAliasAndFileSymlink(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "session.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(directory, alias); err != nil {
		t.Fatal(err)
	}
	if duplicate, err := OpenSQLite(filepath.Join(alias, "session.sqlite"), SQLiteOptions{}); !errors.Is(err, ErrOwned) {
		if duplicate != nil {
			duplicate.Close(bg)
		}
		t.Fatal("alias bypassed ownership", err)
	}
	link := filepath.Join(directory, "link.sqlite")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if duplicate, err := OpenSQLite(link, SQLiteOptions{}); err == nil {
		duplicate.Close(bg)
		t.Fatal("file symlink accepted")
	}
}
