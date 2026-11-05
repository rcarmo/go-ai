package durable

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

func v150WatchKeyFrames(t *testing.T, counts ...int) []WatchFrame {
	t.Helper()
	limits := DefaultLimits()
	keys := map[string]any{}
	frames := []WatchFrame{}
	index := 0
	for _, count := range counts {
		ops := make([]Operation, 0, count)
		for range count {
			key := fmt.Sprintf("k%d", index)
			keys[key] = index
			ops = append(ops, Operation{"s", []any{"keys", key}, index})
			index++
		}
		value, err := copyObject(JSON{"keys": keys}, limits)
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, WatchFrame{Kind: "value", Value: value, ownedValue: value, Ops: ops})
	}
	return frames
}

// Source pending.ts MAX_PENDING_OPS=10000: the 8,000 control rules out an
// earlier native threshold, byte quota or intermediate-frame root fallback.
func TestV150WatchFoldThresholdAndReplay(t *testing.T) {
	limits := DefaultLimits()
	quota := limits.MaxRetainedBytes / 64
	frames := v150WatchKeyFrames(t, 4000, 4000)
	folded, err := coalesceWatchFrames(frames, limits, quota)
	if err != nil || len(folded.Ops) != 8000 {
		t.Fatalf("8000 source ops: count=%d error=%v", len(folded.Ops), err)
	}
	replay, err := ApplyOperations(JSON{"keys": map[string]any{}}, folded.Ops, limits)
	if err != nil || !equalJSONValue(replay, folded.Value) {
		t.Fatalf("8000 replay error=%v", err)
	}
	frames = v150WatchKeyFrames(t, 4000, 4000, 2001)
	folded, err = coalesceWatchFrames(frames, limits, quota)
	if err != nil || len(folded.Ops) != 1 || folded.Ops[0][0] != "r" {
		t.Fatalf("10001 source root: count=%d error=%v", len(folded.Ops), err)
	}
	if len(folded.Value["keys"].(map[string]any)) != 10001 {
		t.Fatal("source root lost keys")
	}
	if _, err := coalesceWatchFrames(frames, limits, 1); err == nil {
		t.Fatal("byte quota bypassed")
	}
}

func TestV150WatchSingleBatchAndRootRecovery(t *testing.T) {
	limits := DefaultLimits()
	frames := v150WatchKeyFrames(t, 10001)
	single, err := coalesceWatchFrames(frames, limits, limits.MaxRetainedBytes/64)
	if err != nil || len(single.Ops) != 10001 {
		t.Fatalf("single batch changed: %d %v", len(single.Ops), err)
	}
	// The first single batch must also be a valid public replay input.
	if _, err := ownOperations(single.Ops, limits); err != nil {
		t.Fatalf("single batch is not replayable: %v", err)
	}
	root := WatchFrame{Kind: "value", Value: JSON{"keys": map[string]any{}}, ownedValue: JSON{"keys": map[string]any{}}, Ops: []Operation{{"r", map[string]any{"keys": map[string]any{}}}}}
	next := WatchFrame{Kind: "value", Value: JSON{"keys": map[string]any{"final": 1}}, ownedValue: JSON{"keys": map[string]any{"final": 1}}, Ops: []Operation{{"s", []any{"keys", "final"}, 1}}}
	frames = append(frames, root, next)
	folded, err := coalesceWatchFrames(frames, limits, limits.MaxRetainedBytes/64)
	if err != nil || len(folded.Ops) != 2 || folded.Ops[0][0] != "r" {
		t.Fatalf("root recovery=%v error=%v", folded.Ops, err)
	}
}

func TestV150WatchNewBoundsPreservePersistedPolicy(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxMembers = 4096
	limits.MaxNodes = 65536
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{Limits: &limits})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(bg)
	got, err := reopened.Limits()
	if err != nil || got != limits {
		t.Fatalf("persisted policy changed: %+v %v", got, err)
	}
	keys := map[string]any{}
	for i := 0; i < 5000; i++ {
		keys[fmt.Sprint(i)] = json.Number("1")
	}
	if _, err := ownJSONValue(keys, got); err == nil {
		t.Fatal("legacy member policy was silently widened")
	}
}
