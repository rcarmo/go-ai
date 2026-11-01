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

// This process deliberately omits Close. The parent joins it before opening the
// journal under normal native ownership rules; no lock is disabled for recovery.
func TestGroupJournalCrashChild(t *testing.T) {
	if os.Getenv("GO_AI_JOURNAL_GROUP_CRASH_CHILD") != "1" {
		return
	}
	dir := os.Getenv("GO_AI_JOURNAL_GROUP_CRASH_PATH")
	if !filepath.IsAbs(dir) {
		t.Fatal("child journal path must be absolute")
	}
	phase := os.Getenv("GO_AI_JOURNAL_GROUP_CRASH_PHASE")
	switch phase {
	case "stored-first-before-adopt", "stored-second-before-adopt", "first-receipt", "returned", "refused-prefix":
	default:
		t.Fatal("invalid crash phase")
	}
	s, err := OpenJournal(dir, JournalOptions{}) // default sync policy, not a buffered journal
	if err != nil {
		t.Fatal(err)
	}
	batches, _ := groupEntryBatches(t, s, 3)
	if phase == "refused-prefix" {
		batches[2].Writes = nil
	}
	appendFrame := s.appendFrame
	stored := 0
	s.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
		if err := appendFrame(kind, ordinal, high, payload); err != nil {
			return err
		}
		if kind == 2 {
			stored++
			if (phase == "stored-first-before-adopt" && stored == 1) || (phase == "stored-second-before-adopt" && stored == 2) {
				os.Exit(0)
			}
		}
		return nil
	}
	received := 0
	result, err := s.commitBatches(bg, batches, func(index int, seq uint64) error {
		if index != received || seq != uint64(index+1) {
			return fmt.Errorf("unexpected receipt index=%d seq=%d", index, seq)
		}
		received++
		if err := os.WriteFile(filepath.Join(dir, "receipts"), []byte(strconv.Itoa(received)), 0600); err != nil {
			return err
		}
		if phase == "first-receipt" {
			os.Exit(0)
		}
		return nil
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if phase == "refused-prefix" {
		if result.stored != 2 || result.refused == nil || received != 2 {
			t.Fatal(result, received)
		}
	} else {
		if phase != "returned" || result.stored != 3 || result.refused != nil || received != 3 {
			t.Fatal(result, received)
		}
		if id, err := s.MintID(bg); err != nil || id != 5 {
			t.Fatal("unused reservation", id, err)
		}
	}
	os.Exit(0)
}

func TestInternalJournalGroupCrashRecovery(t *testing.T) {
	for _, tc := range []struct {
		phase            string
		stored, receipts int
		high             uint64
	}{
		{"stored-first-before-adopt", 1, 0, 4},
		{"stored-second-before-adopt", 2, 1, 4},
		{"first-receipt", 1, 1, 4},
		{"returned", 3, 3, 5},
		{"refused-prefix", 2, 2, 4},
	} {
		t.Run(tc.phase, func(t *testing.T) {
			owned := t.TempDir()
			dir := filepath.Join(owned, "journal")
			ctx, cancel := context.WithTimeout(bg, 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGroupJournalCrashChild$", "-test.timeout=12s")
			cmd.Dir = owned
			cmd.Env = []string{
				"GO_AI_JOURNAL_GROUP_CRASH_CHILD=1", "GO_AI_JOURNAL_GROUP_CRASH_PATH=" + dir,
				"GO_AI_JOURNAL_GROUP_CRASH_PHASE=" + tc.phase, "TMPDIR=" + owned, "TMP=" + owned, "TEMP=" + owned,
				"HOME=" + owned, "APPDATA=" + owned, "GOMAXPROCS=4",
			}
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("crash helper: %v\n%s", err, output)
			}
			marker, err := os.ReadFile(filepath.Join(dir, "receipts"))
			if tc.receipts == 0 {
				if !os.IsNotExist(err) {
					t.Fatal("pre-receipt crash left a marker", err)
				}
			} else {
				count, parseErr := strconv.Atoi(string(marker))
				if err != nil || parseErr != nil || count != tc.receipts {
					t.Fatal("receipt evidence", string(marker), err, parseErr)
				}
			}
			s, err := OpenJournal(dir, JournalOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close(bg)
			state, err := s.Snapshot(bg)
			if err != nil || state.Seq != uint64(tc.stored) || len(state.Entries) != tc.stored || state.HighWater != tc.high {
				t.Fatal("recovered prefix", state, err)
			}
			for index := 0; index < tc.stored; index++ {
				entry := state.Entries[ID(index+2)]
				if entry.Seq != uint64(index+1) || entry.Position != 1 || entry.Value["text"] != "original" {
					t.Fatal("lost recovered entry", entry)
				}
			}
			for index := tc.stored; index < 3; index++ {
				if _, ok := state.Entries[ID(index+2)]; ok {
					t.Fatal("invented unconfirmed suffix")
				}
			}
			id, err := s.MintID(bg)
			if err != nil || uint64(id) <= tc.high {
				t.Fatal("reservation reused", id, err)
			}
			seq, err := s.Apply(bg, Batch{Writes: []Write{{Op: "append-entry", Entry: &Entry{ID: id, Conversation: 1, Kind: "after-crash", Value: JSON{}}}}})
			if err != nil || seq != uint64(tc.stored+1) {
				t.Fatal("post-crash append", seq, err)
			}
		})
	}
}
