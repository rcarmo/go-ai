package durable

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const v160FiveStoreValues = `for (let i=0;i<5;i++) store("k"+i, String(i).repeat(200*1024)); return "stored-five"`

// Supplemental to sandbox.test.ts:454–470. Five individually legal writes
// fit one default SQLite transaction but not a single Durable document.
func TestV160CodemodePublishedLargeStorePatch(t *testing.T) {
	for _, mode := range []struct{ name, refusal string }{
		{"default-frame", ""},
		{"tight-frame", "encoded JSON byte limit"},
		{"tight-writes", "invalid write count"},
	} {
		t.Run(mode.name, func(t *testing.T) {
			sandbox, worker := newJournalWorkerSandboxV160(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			limits := DefaultLimits()
			switch mode.name {
			case "tight-frame":
				limits.MaxFramePayloadBytes = 1536 << 10
			case "tight-writes":
				limits.MaxWrites = 10
			}
			if mode.refusal == "" {
				worker.guestStore = func(run int32, store JSON) JSON {
					if run == 2 {
						// Simulate a peer proposing a patch against an empty rather
						// than the saved nonempty start: host owns the merged cap.
						return JSON{}
					}
					return store
				}
			}
			path := filepath.Join(t.TempDir(), "sqlite")
			openStore := func() *SQLiteStorage {
				store, err := openSQLiteTestDirectory(path, SQLiteOptions{Limits: &limits})
				if err != nil {
					t.Fatal(err)
				}
				return store
			}
			registry := NewRegistry()
			writer := taskDefinition(t, "store-five-v160", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				first, err := r.RunCodemode(ctx, "write", v160FiveStoreValues, sandbox)
				if mode.refusal != "" {
					if err == nil || !strings.Contains(err.Error(), mode.refusal) || first.HasValue || len(first.StoreWrites) != 0 {
						return fmt.Errorf("%s: want atomic %q refusal, got value=%v writes=%d error=%v", mode.name, mode.refusal, first.HasValue, len(first.StoreWrites), err)
					}
				} else {
					if err != nil {
						return fmt.Errorf("public RunCodemode of five individually legal stores: %w", err)
					}
					second, err := r.RunCodemode(ctx, "write", v160FiveStoreValues, sandbox)
					if err != nil {
						return fmt.Errorf("settled-run readback: %w", err)
					}
					for _, result := range []CodemodeResult{first, second} {
						if !result.HasValue || result.Value != "stored-five" || result.Error != "" || len(result.StoreWrites) != 5 || len(result.StoreDeletes) != 0 {
							return fmt.Errorf("wrong result: value=%v error=%q writes=%d deletes=%d", result.Value, result.Error, len(result.StoreWrites), len(result.StoreDeletes))
						}
						for i := 0; i < 5; i++ {
							if result.StoreWrites[fmt.Sprintf("k%d", i)] != strings.Repeat(fmt.Sprint(i), 200<<10) {
								return fmt.Errorf("wrong full value for k%d", i)
							}
						}
					}
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			})
			rejectMerge := taskDefinition(t, "store-reject-merge-v160", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				// The JSON values use exactly 1 Mi UTF-16 units. Their keys put the merged store over.
				got, err := r.RunCodemode(ctx, "excess", `store("excess", "z".repeat(24564)); return "locally valid"`, sandbox)
				if err == nil || err.Error() != "durable: rejected: codemode logical store exceeds retained value limit" || got.HasValue || len(got.StoreWrites) != 0 {
					return fmt.Errorf("merged store cap: value=%v writes=%d error=%v", got.HasValue, len(got.StoreWrites), err)
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			})
			reader := func(kind, comparison string) *TaskDefinition {
				return taskDefinition(t, kind, func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					code := `return load("excess") === undefined && [0,1,2,3,4].every(i => load("k"+i) === ` + comparison + `)`
					got, err := r.RunCodemode(ctx, "read", code, sandbox)
					if err != nil || !got.HasValue || got.Value != true || got.Error != "" || len(got.StoreWrites) != 0 {
						return fmt.Errorf("read %s: value=%v script error=%q writes=%d err=%v", kind, got.Value, got.Error, len(got.StoreWrites), err)
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
			}
			full := reader("read-five-v160", `String(i).repeat(200*1024)`)
			empty := reader("read-empty-v160", "undefined")
			for _, def := range []*TaskDefinition{writer, rejectMerge, full, empty} {
				if _, err := registry.RegisterTask(def); err != nil {
					t.Fatal(err)
				}
			}
			h := openHarness(t, openStore(), Options{Registry: registry})
			parent, err := h.Conversation(ctx, 1)
			if err != nil {
				t.Fatal(err)
			}
			if err := h.Resume(ctx); err != nil {
				t.Fatal(err)
			}
			run := func(c *ConversationHandle, def *TaskDefinition) {
				t.Helper()
				value, err := c.Commit(ctx, func(tx *Tx) (any, error) {
					return tx.CreateTask(def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				})
				if err != nil {
					t.Fatal(err)
				}
				row, err := h.WaitForTask(ctx, value.(ID))
				if row.State.Outcome != nil && row.State.Outcome.Error != nil {
					t.Fatal(row.State.Outcome.Error.Message)
				}
				if err != nil || row.State.Outcome == nil || row.State.Outcome.Status != "completed" {
					t.Fatalf("store witness task did not complete: %v; %+v", err, row.State.Outcome)
				}
			}
			boundary := func() ID {
				t.Helper()
				entry, err := parent.Commit(ctx, func(tx *Tx) (any, error) {
					return tx.AppendEntry(parent.ID(), EntryDraft{"kind": "app.store-boundary"})
				})
				if err != nil {
					t.Fatal(err)
				}
				return entry.(Entry).ID
			}
			earlier := boundary()
			run(parent, writer)
			later := boundary()
			if n := worker.starts.Load(); n != 1 {
				t.Fatalf("writer starts including settled retry = %d, want one", n)
			}
			wantStarts := int32(4)
			if mode.refusal == "" {
				run(parent, rejectMerge)
				wantStarts++
			}
			if err := h.Close(ctx); err != nil {
				t.Fatal(err)
			}
			h = openHarness(t, openStore(), Options{Registry: registry})
			parent, err = h.Conversation(ctx, parent.ID())
			if err != nil {
				t.Fatal(err)
			}
			if err := h.Resume(ctx); err != nil {
				t.Fatal(err)
			}
			forkAt := func(at ID) *ConversationHandle {
				t.Helper()
				fork, err := parent.Fork(ctx, at)
				if err != nil {
					t.Fatal(err)
				}
				return fork
			}
			wantRead := full
			if mode.refusal != "" {
				wantRead = empty
			}
			run(parent, wantRead)
			run(forkAt(earlier), empty)
			run(forkAt(later), wantRead)
			if n := worker.starts.Load(); n != wantStarts {
				t.Fatalf("worker starts = %d, want %d", n, wantStarts)
			}
		})
	}
}

// Exercise the same bounded storage path without a remote transport. The
// second native VM must load the large start snapshot without truncating it.
func TestV160CodemodeInlineLargeStorePatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	sandbox, err := NewQuickJSSandbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(context.Background())
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	def := taskDefinition(t, "inline-store-five-v160", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		result, err := r.RunCodemode(ctx, "write", v160FiveStoreValues, sandbox)
		if err != nil || result.Error != "" || !result.HasValue || result.Value != "stored-five" || len(result.StoreWrites) != 5 {
			return fmt.Errorf("inline write: value=%v error=%q writes=%d; %v", result.Value, result.Error, len(result.StoreWrites), err)
		}
		for i := 0; i < 5; i++ {
			if result.StoreWrites[fmt.Sprintf("k%d", i)] != strings.Repeat(fmt.Sprint(i), 200<<10) {
				return fmt.Errorf("inline result changed k%d", i)
			}
		}
		fresh, err := r.RunCodemode(ctx, "read", `return [0,1,2,3,4].every(i => load("k"+i) === String(i).repeat(200*1024))`, sandbox)
		if err != nil || fresh.Error != "" || !fresh.HasValue || fresh.Value != true || len(fresh.StoreWrites) != 0 {
			return fmt.Errorf("inline read: value=%v error=%q writes=%d; %v", fresh.Value, fresh.Error, len(fresh.StoreWrites), err)
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	if _, err := registry.RegisterTask(def); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, Options{Registry: registry})
	id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	row, err := h.WaitForTask(ctx, id)
	if row.State.Outcome != nil && row.State.Outcome.Error != nil {
		t.Fatal(row.State.Outcome.Error.Message)
	}
	if err != nil || row.State.Outcome == nil || row.State.Outcome.Status != "completed" {
		t.Fatalf("inline store task outcome=%+v err=%v", row.State.Outcome, err)
	}
}

func TestV160CodemodeLogicalStoreBudgetCountsKeys(t *testing.T) {
	// The quotes bring each value to exactly the guest's per-value cap.
	v := strings.Repeat("x", (256<<10)-2)
	for _, tc := range []struct {
		name, key string
		trim      int
		wantError bool
	}{
		{"ascii-exact", "a", 4, false},
		{"ascii-one-over", "a", 3, true},
		{"surrogate-pair-key-exact", "😀", 5, false},
		{"surrogate-pair-key-one-over", "😀", 4, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := JSON{tc.key: v, "b": v, "c": v, "d": v[:len(v)-tc.trim]}
			err := validateCodemodeLogicalStore(values, DefaultLimits())
			if tc.wantError {
				if err == nil || err.Error() != "durable: rejected: codemode logical store exceeds retained value limit" {
					t.Fatalf("logical store error=%v; want retained value limit", err)
				}
			} else if err != nil {
				t.Fatalf("logical store at exact limit was rejected: %v", err)
			}
		})
	}
}
