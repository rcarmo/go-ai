package durable

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// downgradeSQLiteConfig rewrites a freshly created store's config row to an older
// persisted version so the legacy compatibility path can be exercised end to end.
func downgradeSQLiteConfig(store *SQLiteStorage, version uint64) error {
	var data []byte
	if err := store.db.QueryRowContext(context.Background(), store.schemaSQL("SELECT value FROM config WHERE id=1")).Scan(&data); err != nil {
		return err
	}
	var config sqliteConfig
	if err := decodeStrict(data, DefaultLimits(), 16<<10, &config); err != nil {
		return err
	}
	config.Version = version
	encoded, err := encodeBounded(config, DefaultLimits(), 16<<10)
	if err != nil {
		return err
	}
	return store.exec("UPDATE config SET value=? WHERE id=1", encoded)
}

// TestPiDurableV150PublicStorageCommitReplaysCapturedOriginalBatches replays the
// exact command batches the original pi-durable 1.5.0 Session handed to
// Storage.commit (testdata/pi-durable-v150/storage-writes.json, captured from the
// pinned reference). Every batch must be accepted as one atomic commit with its
// own sequence, nothing may be refused, and the stored public records must
// re-encode to the records the original asked for.
func TestPiDurableV150PublicStorageCommitReplaysCapturedOriginalBatches(t *testing.T) {
	raw, err := os.ReadFile("testdata/pi-durable-v150/storage-writes.json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Batches json.RawMessage `json:"batches"`
		Types   []string        `json:"types"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Batches [][]StorageWrite
		Types   []string
	}
	fixture.Types = envelope.Types
	if err := decodePublicJSON(envelope.Batches, &fixture.Batches); err != nil {
		t.Fatal(err)
	}
	wantTypes := []string{"conversation", "document.change", "document.copy", "document.create", "document.retire", "entry", "submission", "task"}
	if !reflect.DeepEqual(fixture.Types, wantTypes) {
		t.Fatalf("types %v", fixture.Types)
	}
	if len(fixture.Batches) != 12 {
		t.Fatalf("batches %d", len(fixture.Batches))
	}

	// The captured stream never writes the reserved root: it predates the raw/fresh
	// root split, exactly like a persisted version 1 store. Downgrade the config row
	// so the production legacy compatibility path supplies the implicit root, then
	// reopen. This keeps every captured sequence number and document point intact.
	path := filepath.Join(t.TempDir(), "store.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := downgradeSQLiteConfig(store, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	store, err = OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(context.Background())
	ctx := context.Background()
	if _, ok := store.state.Conversations[1]; !ok {
		t.Fatal("version 1 store did not adopt the implicit reserved root")
	}

	// The original assigned every identity before its Session handed the batches
	// over; reserve the same range here so the caller-supplied IDs are owned.
	var highest ID
	for _, batch := range fixture.Batches {
		for _, command := range batch {
			if id := storageWriteID(command); id > highest {
				highest = id
			}
		}
	}
	for {
		id, err := store.MintID(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if id > highest {
			t.Fatalf("minted past the captured identity range: %d > %d", id, highest)
		}
		if id == highest {
			break
		}
	}

	var receipts []uint64
	result, err := store.Commit(ctx, fixture.Batches, func(index int, seq uint64) {
		if uint64(index) != seq-1 {
			t.Errorf("receipt %d out of order at seq %d", index, seq)
		}
		receipts = append(receipts, seq)
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stored != 12 || result.Refused != nil {
		t.Fatalf("stored=%d refused=%v", result.Stored, result.Refused)
	}
	if len(receipts) != 12 {
		t.Fatalf("receipts %v", receipts)
	}
	for i, seq := range receipts {
		if seq != uint64(i+1) {
			t.Fatalf("receipt seq %d = %d, want %d", i, seq, i+1)
		}
	}

	state, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Conversations: the root, the conversation the first entry went to, the
	// task-owned one, and the fork of the first at its entry.
	for _, id := range []ID{1, 3, 8, 10} {
		if _, ok := state.Conversations[id]; !ok {
			t.Fatalf("conversation %d missing", id)
		}
	}
	if fork := state.Conversations[10]; fork.Parent != 3 || fork.ParentAt != 5 {
		t.Fatalf("fork ancestry %+v", fork)
	}
	if len(state.Entries) != 2 {
		t.Fatalf("entries %+v", state.Entries)
	}
	if note := state.Entries[5]; note.Kind != "note" || !note.HasData || note.Model != nil {
		t.Fatalf("first entry %+v", note)
	}
	if head := state.Entries[6]; head.Head != 6 || len(head.Model) != 1 || head.HasData {
		t.Fatalf("head entry %+v", head)
	}
	task := state.Tasks[7]
	if task.Kind != "t.work" || task.Conversation != 3 || task.Status != "pending" || task.Execution == nil || task.Execution.Native == nil {
		t.Fatalf("task %+v", task)
	}
	if version := task.Execution.Native.Version; version != 1 {
		t.Fatalf("task version %d", version)
	}
	if checkpoint, ok := task.Execution.Native.State.Checkpoint.(JSON); !ok || checkpoint["phase"] != "start" {
		t.Fatalf("task checkpoint %#v", task.Execution.Native.State.Checkpoint)
	}
	if len(state.Submissions) != 11 {
		t.Fatalf("submissions %d", len(state.Submissions))
	}
	if sub := state.Submissions[9]; sub.Type != "write" || sub.RequestID != "r1" {
		t.Fatalf("submission 9 %+v", sub)
	}
	// The original wrote an explicitly empty reason and a present null detail;
	// both must read back exactly as written.
	if sub := publicSubmission(state.Submissions[15]); sub.Status != "unanswered" || sub.Reason != "" || sub.Detail == nil || !sub.Detail.Present || sub.Detail.Value != nil {
		t.Fatalf("empty-reason submission %+v", sub)
	}
	if sub := publicSubmission(state.Submissions[16]); sub.Status != "unanswered" || sub.Reason != "failed" || sub.Detail == nil || !sub.Detail.Present {
		t.Fatalf("failed submission %+v", sub)
	}
	sessionDoc := state.Documents[2]
	if !sessionDoc.Retired || sessionDoc.RetiredAt == 0 {
		t.Fatalf("session document %+v", sessionDoc)
	}
	if rewind := state.Documents[4]; rewind.History != "rewindable" || rewind.Fork != "asOf" || rewind.DeltasSinceBase != 1 {
		t.Fatalf("rewindable document %+v", rewind)
	}
	// The copy names seq 3, the base revision, so it must materialise value 1 and
	// not the delta that landed one commit later.
	if copy := state.Documents[11]; copy.Owner != 10 || jsonNumber(copy.Value["value"]) != 1 {
		t.Fatalf("copied document %+v", copy)
	}
	if taskDoc := state.Documents[22]; taskDoc.Scope != "task" || jsonNumber(taskDoc.Value["value"]) != 2 {
		t.Fatalf("task document %+v", taskDoc)
	}

	// Every record the original wrote must read back as the record it wrote.
	for _, batch := range fixture.Batches {
		for _, command := range batch {
			var stored any
			var expected any
			switch value := command.Value.(type) {
			case *Entry:
				stored = state.Entries[value.ID]
			case *TaskRecord:
				task, ok := state.Tasks[value.ID]
				if !ok {
					t.Fatalf("task %d missing", value.ID)
				}
				record, err := CanonicalTask(task, maximumDurableLimits())
				if err != nil {
					t.Fatal(err)
				}
				stored = record
			case *SubmissionRecord:
				submission, ok := state.Submissions[value.ID]
				if !ok {
					t.Fatalf("submission %d missing", value.ID)
				}
				stored = publicSubmission(submission)
			default:
				continue
			}
			expected = command.Value
			// Every original field, including present-empty requestId, must match.
			if !reflect.DeepEqual(canonicalJSON(t, stored), canonicalJSON(t, expected)) {
				t.Fatalf("%s %v re-encoded differently:\n stored: %s\n wrote:  %s", command.Type, storageWriteID(command), marshalOrFail(t, stored), marshalOrFail(t, expected))
			}
		}
	}
}

func storageWriteID(command StorageWrite) ID {
	switch value := command.Value.(type) {
	case *Conversation:
		return value.ID
	case *Entry:
		return value.ID
	case *TaskRecord:
		return value.ID
	case *SubmissionRecord:
		return value.ID
	}
	return command.ID
}

func jsonNumber(value any) float64 {
	number, _ := value.(json.Number)
	result, _ := number.Float64()
	return result
}

func marshalOrFail(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func canonicalJSON(t *testing.T, value any) any {
	t.Helper()
	var decoded any
	if err := json.Unmarshal(marshalOrFail(t, value), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}
