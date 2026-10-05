//go:build cgo

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
	runStorageConformance(t, backend{store: current, reopen: func() Storage {
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
	// The subprocess runs the same test binary. Profiles are still captured by
	// the parent test run; child profiling is enabled explicitly as well.
	profiles := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=^TestSQLiteCrossProcessOwnership$", "-test.cpuprofile="+filepath.Join(profiles, "child.cpu"), "-test.memprofile="+filepath.Join(profiles, "child.heap"))
	command.Env = append(os.Environ(), "DURABLE_SQLITE_LOCK_TEST="+path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("ownership subprocess: %v\n%s", err, output)
	}
	for _, kind := range []string{"alloc_space", "alloc_objects", "cpu"} {
		profile := filepath.Join(profiles, "child.heap")
		flag := "-" + kind
		if kind == "cpu" {
			profile = filepath.Join(profiles, "child.cpu")
			flag = "-cum"
		}
		output, err := exec.Command("go", "tool", "pprof", "-top", flag, os.Args[0], profile).CombinedOutput()
		if err != nil {
			t.Fatalf("child profile analysis: %v\n%s", err, output)
		}
		t.Logf("child %s profile:\n%s", kind, output)
	}
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
