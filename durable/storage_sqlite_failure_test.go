//go:build cgo && unix

package durable

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"testing"
)

func TestSQLiteRealInsertFailureRollsBackAndKeepsReservation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	id := mint(t, store)
	before := snap(t, store)
	if err := store.exec("CREATE TRIGGER fail_commit BEFORE INSERT ON frames WHEN NEW.kind=2 BEGIN SELECT RAISE(ABORT,'injected insert failure'); END;"); err != nil {
		t.Fatal(err)
	}
	_, err = store.Apply(bg, Batch{Writes: []Write{entry(id, JSON{"value": "not committed"})}})
	rejected(t, err)
	after := snap(t, store)
	if after.Seq != before.Seq || after.HighWater != before.HighWater || len(after.Entries) != 0 {
		t.Fatal("SQLite rejection partially adopted", after)
	}
	if err := store.exec("DROP TRIGGER fail_commit"); err != nil {
		t.Fatal(err)
	}
	apply(t, store, entry(id, JSON{"value": "committed"}))
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	next, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close(bg)
	if value := snap(t, next); value.HighWater != uint64(id) || len(value.Entries) != 1 || value.Entries[id].Value["value"] != "committed" {
		t.Fatal("rollback/reopen", value)
	}
}
func TestSQLiteReclaimDeleteFailureRollsBackCheckpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	id := mint(t, store)
	apply(t, store, entry(id, JSON{"persisted": true}))
	if err := store.exec("CREATE TRIGGER fail_delete BEFORE DELETE ON frames BEGIN SELECT RAISE(ABORT,'injected reclaim failure'); END;"); err != nil {
		t.Fatal(err)
	}
	rejected(t, store.Reclaim(bg))
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	next, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal("reclaim rollback left duplicate checkpoint+frames", err)
	}
	defer next.Close(bg)
	if len(snap(t, next).Entries) != 1 {
		t.Fatal("failed reclamation lost entry")
	}
	if err := next.exec("DROP TRIGGER fail_delete"); err != nil {
		t.Fatal(err)
	}
	if err := next.Reclaim(bg); err != nil {
		t.Fatal(err)
	}
}
func TestSQLiteLostCommitAcknowledgementPoisonsThenReopenAdopts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	id := mint(t, store)
	// Per-instance transport seam: the actual SQLite COMMIT succeeds, then its
	// acknowledgement is lost. This is uncertainty injection, not an OS fault.
	store.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
		if err := store.append(kind, ordinal, high, payload); err != nil {
			return err
		}
		return errors.New("lost acknowledgement")
	}
	if _, err := store.Apply(bg, Batch{Writes: []Write{entry(id, JSON{"adopted": true})}}); !errors.Is(err, ErrPoisoned) {
		t.Fatal(err)
	}
	if _, err := store.Snapshot(bg); !errors.Is(err, ErrPoisoned) {
		t.Fatal("uncertain SQLite usable", err)
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	next, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close(bg)
	if state := snap(t, next); state.Seq != 1 || state.Entries[id].Value["adopted"] != true {
		t.Fatal("confirmed SQLite prefix not adopted", state)
	}
}
func TestSQLiteProcessExitWithoutClosePreservesCommittedPrefix(t *testing.T) {
	if path := os.Getenv("DURABLE_SQLITE_CRASH_PATH"); path != "" {
		profiles := os.Getenv("DURABLE_SQLITE_CRASH_PROFILES")
		cpu, err := os.Create(filepath.Join(profiles, "cpu.pprof"))
		if err != nil {
			t.Fatal(err)
		}
		if err := pprof.StartCPUProfile(cpu); err != nil {
			t.Fatal(err)
		}
		store, err := OpenSQLite(path, SQLiteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		id := mint(t, store)
		apply(t, store, entry(id, JSON{"before": "checkpoint"}))
		if err := store.Reclaim(bg); err != nil {
			t.Fatal(err)
		}
		id = mint(t, store)
		apply(t, store, entry(id, JSON{"after": "checkpoint"}))
		reserved := mint(t, store) // reservation survives even with no later record.
		if reserved != 4 {
			t.Fatal("fixture allocator", reserved)
		}
		pprof.StopCPUProfile()
		cpu.Close()
		heap, err := os.Create(filepath.Join(profiles, "heap.pprof"))
		if err != nil {
			t.Fatal(err)
		}
		runtime.GC()
		if err := pprof.WriteHeapProfile(heap); err != nil {
			t.Fatal(err)
		}
		heap.Close()
		// Immediate process exit deliberately omits Storage.Close and lock release.
		// This proves process-crash recovery only, not universal power-loss safety.
		os.Exit(23)
	}
	path := filepath.Join(t.TempDir(), "session.sqlite")
	profiles := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=^TestSQLiteProcessExitWithoutClosePreservesCommittedPrefix$")
	command.Env = append(os.Environ(), "DURABLE_SQLITE_CRASH_PATH="+path, "DURABLE_SQLITE_CRASH_PROFILES="+profiles)
	output, err := command.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("crash subprocess: %v\n%s", err, output)
	}
	for _, kind := range []string{"alloc_space", "alloc_objects", "cpu"} {
		profile := filepath.Join(profiles, "heap.pprof")
		flag := "-" + kind
		if kind == "cpu" {
			profile = filepath.Join(profiles, "cpu.pprof")
			flag = "-cum"
		}
		output, err := exec.Command("go", "tool", "pprof", "-top", flag, os.Args[0], profile).CombinedOutput()
		if err != nil {
			t.Fatalf("crash profile analysis: %v\n%s", err, output)
		}
		t.Logf("crash helper %s:\n%s", kind, output)
	}
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	state := snap(t, store)
	if state.Seq != 2 || state.HighWater != 4 || len(state.Entries) != 2 || state.Entries[2].Value["before"] != "checkpoint" || state.Entries[3].Value["after"] != "checkpoint" {
		t.Fatal("crash prefix mismatch", state)
	}
	if id := mint(t, store); id != 5 {
		t.Fatal("crash allocator reused reservation", id)
	}
}
