package durable

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Reclaim atomically replaces redundant physical frames with a full native
// checkpoint. Logical history, spent IDs and sequence/ordinal values survive.
// This adapts pi-durable's sidecar replacement maintenance to the native journal;
// it does not read or write the upstream JSONL format.
func (j *JournalStorage) Reclaim(ctx context.Context) error {
	if err := j.enter(ctx); err != nil {
		return err
	}
	defer j.leave()
	if j.ordinal < 2 {
		return nil
	} // Config-only journals have nothing to reclaim.
	value := checkpointOf(j.state, j.ordinal)
	payload, err := encodeBounded(value, j.limits, j.limits.MaxRetainedBytesAsInt())
	if err != nil {
		return err
	}
	config, err := encodeBounded(journalConfig{Version: 2, Sync: j.syncWrites, Limits: j.limits}, DefaultLimits(), 16<<10)
	if err != nil {
		return err
	}
	first, checkpoint := frame(0, 1, 1, config), frame(3, j.ordinal, j.state.HighWater, payload)
	size := int64(len(first)) + int64(len(checkpoint))
	if size > j.limits.MaxJournalBytes {
		return reject("journal checkpoint/file limit")
	}
	temp, err := os.CreateTemp(filepath.Dir(j.path), ".journal-reclaim-*")
	if err != nil {
		return err
	}
	adopted := false
	defer func() {
		if !adopted {
			_ = temp.Close()
		}
		_ = os.Remove(temp.Name())
	}()
	for _, data := range [][]byte{first, checkpoint} {
		n, err := temp.Write(data)
		if err != nil {
			return err
		}
		if n != len(data) {
			return io.ErrShortWrite
		}
	}
	if j.syncWrites {
		if err := temp.Sync(); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Until rename the authoritative journal and in-memory state are untouched.
	if err := os.Rename(temp.Name(), j.path); err != nil {
		return err
	}
	old := j.file
	j.file = temp
	j.size = size
	adopted = true
	// After rename, any acknowledgement failure is uncertain. Keep ownership and
	// seal further operations until Close/reopen validates the adopted image.
	if err := old.Close(); err != nil {
		j.poisoned = true
		return fmt.Errorf("%w: reclaimed journal close failed", ErrPoisoned)
	}
	if j.syncWrites {
		if err := syncDirectory(filepath.Dir(j.path)); err != nil {
			j.poisoned = true
			return fmt.Errorf("%w: reclaimed journal directory sync failed", ErrPoisoned)
		}
	}
	return nil
}
