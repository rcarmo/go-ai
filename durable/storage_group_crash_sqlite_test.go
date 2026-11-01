//go:build cgo

package durable

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// The helper exits without Close or deferred cleanup. Its parent owns every
// path, supplies a credential-free environment, waits for process termination,
// and only then acquires the normal native file/ownership locks for recovery.
func TestGroupSQLiteCrashChild(t *testing.T) {
	if os.Getenv("GO_AI_GROUP_CRASH_CHILD") != "1" {
		return
	}
	path := os.Getenv("GO_AI_GROUP_CRASH_PATH")
	if !filepath.IsAbs(path) {
		t.Fatal("child database path must be absolute")
	}
	s, err := OpenSQLite(path, SQLiteOptions{Prefix: os.Getenv("GO_AI_GROUP_CRASH_PREFIX")})
	if err != nil {
		t.Fatal(err)
	}
	batches, _ := groupEntryBatches(t, s, 3)
	phase := os.Getenv("GO_AI_GROUP_CRASH_PHASE")
	if phase == "committed-before-receipts" {
		appendSeries := s.appendFrames
		s.appendFrames = func(frames []storageFrame) error {
			if err := appendSeries(frames); err != nil {
				return err
			}
			os.Exit(0) // durable, not yet adopted by the core, no receipt
			return nil
		}
	}
	received := 0
	result, err := s.commitBatches(bg, batches, func(index int, seq uint64) error {
		if index != received || seq != uint64(index+1) {
			return fmt.Errorf("unexpected receipt index=%d seq=%d", index, seq)
		}
		received++
		if phase == "first-receipt" {
			if err := os.WriteFile(path+".receipts", []byte(strconv.Itoa(received)), 0600); err != nil {
				return err
			}
			os.Exit(0)
		}
		return nil
	}, false)
	if err != nil || result.refused != nil || result.stored != 3 || received != 3 || phase != "returned" {
		t.Fatalf("child result=%+v receipts=%d phase=%s err=%v", result, received, phase, err)
	}
	// The unused reservation must survive too, so reopen cannot reuse it.
	if id, err := s.MintID(bg); err != nil || id != 5 {
		t.Fatalf("unused reservation=%d err=%v", id, err)
	}
	if err := os.WriteFile(path+".receipts", []byte(strconv.Itoa(received)), 0600); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestInternalSQLiteGroupCrashRecovery(t *testing.T) {
	for _, prefix := range []string{"", "worker_"} {
		for _, phase := range []string{"committed-before-receipts", "first-receipt", "returned"} {
			t.Run(prefix+phase, func(t *testing.T) {
				owned := t.TempDir()
				path := filepath.Join(owned, "state.db")
				ctx, cancel := context.WithTimeout(bg, 15*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGroupSQLiteCrashChild$", "-test.timeout=12s")
				cmd.Dir = owned
				cmd.Env = []string{
					"GO_AI_GROUP_CRASH_CHILD=1", "GO_AI_GROUP_CRASH_PATH=" + path,
					"GO_AI_GROUP_CRASH_PREFIX=" + prefix, "GO_AI_GROUP_CRASH_PHASE=" + phase,
					"TMPDIR=" + owned, "TMP=" + owned, "TEMP=" + owned,
					"HOME=" + owned, "APPDATA=" + owned, "GOMAXPROCS=4",
				}
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("crash helper: %v\n%s", err, output)
				}
				marker, err := os.ReadFile(path + ".receipts")
				wantReceipts := 0
				if phase == "committed-before-receipts" {
					if !os.IsNotExist(err) {
						t.Fatal("pre-receipt crash left unexpected evidence", err)
					}
				} else {
					wantReceipts = 1
					if phase == "returned" {
						wantReceipts = 3
					}
					count, parseErr := strconv.Atoi(string(marker))
					if err != nil || parseErr != nil || count != wantReceipts {
						t.Fatalf("receipt evidence=%q read=%v parse=%v", marker, err, parseErr)
					}
				}
				s, err := OpenSQLite(path, SQLiteOptions{Prefix: prefix})
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close(bg)
				state, err := s.Snapshot(bg)
				high := uint64(4)
				if phase == "returned" {
					high = 5
				}
				if err != nil || state.Seq != 3 || state.HighWater != high || len(state.Entries) != 3 {
					t.Fatalf("recovery=%+v receipts=%d err=%v", state, wantReceipts, err)
				}
				for index := 0; index < 3; index++ {
					entry := state.Entries[ID(index+2)]
					if entry.Seq != uint64(index+1) || entry.Position != 1 || entry.Value["text"] != "original" {
						t.Fatal("recovery lost a batch or its sequence", entry)
					}
				}
				id, err := s.MintID(bg)
				if err != nil || uint64(id) <= high {
					t.Fatal("recovery reused a reservation", id, err)
				}
				seq, err := s.Apply(bg, Batch{Writes: []Write{{Op: "append-entry", Entry: &Entry{ID: id, Conversation: 1, Kind: "after-crash", Value: JSON{}}}}})
				if err != nil || seq != 4 {
					t.Fatalf("post-crash apply seq=%d err=%v", seq, err)
				}
			})
		}
	}
}
