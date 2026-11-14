package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The public grouped commit boundary is the original Storage.commit contract:
// ordered batches, one durability receipt per stored batch, and a refusal that
// resolves instead of rejecting. It is not Session publication.
func TestPiDurableV150PublicStorageGroupedCommit(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(context.Background())
	ctx := context.Background()
	reserve := func() ID {
		id, err := store.MintID(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	conversation, rewindDoc, entry, task := reserve(), reserve(), reserve(), reserve()
	submission, taskDoc, child, copyDoc := reserve(), reserve(), reserve(), reserve()

	batches := [][]StorageWrite{
		{
			{Type: "conversation", Value: Conversation{ID: conversation}},
			{Type: "document.create", Record: &DocumentCreate{ID: rewindDoc, Kind: "t.rewind", Scope: DocumentCreateScope{Kind: "conversation", ConversationID: conversation}, History: "rewindable", Fork: "asOf"}, Content: &DocumentContent{Version: 1, Kind: "base", Value: JSON{"value": 1}}},
		},
		{
			{Type: "entry", Value: Entry{ID: entry, Conversation: conversation, Kind: "note", Data: map[string]any{"n": 1}, HasData: true}},
			{Type: "task", Value: TaskRecord{ID: task, Conversation: conversation, Kind: "t.work", Version: 1, Input: &TaskValue{Present: true, Value: JSON{"path": "a"}}, State: TaskState{Status: "pending", Checkpoint: map[string]any{"phase": "start"}}}},
		},
		{
			{Type: "submission", Value: SubmissionRecord{ID: submission, Conversation: conversation, Type: "write", Status: "queued", RequestID: "r1"}},
			{Type: "document.create", Record: &DocumentCreate{ID: taskDoc, Kind: "t.task", Scope: DocumentCreateScope{Kind: "task", TaskID: task}}, Content: &DocumentContent{Version: 1, Kind: "base", Value: JSON{"value": 1}}},
		},
	}
	var receipts []struct {
		index int
		seq   uint64
	}
	result, err := store.Commit(ctx, batches, func(index int, seq uint64) {
		receipts = append(receipts, struct {
			index int
			seq   uint64
		}{index, seq})
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stored != 3 || result.Refused != nil {
		t.Fatalf("stored=%d refused=%v", result.Stored, result.Refused)
	}
	want := []struct {
		index int
		seq   uint64
	}{{0, 1}, {1, 2}, {2, 3}}
	if len(receipts) != len(want) {
		t.Fatalf("receipts=%v", receipts)
	}
	for i, receipt := range receipts {
		if receipt != want[i] {
			t.Fatalf("receipt %d = %v, want %v", i, receipt, want[i])
		}
	}

	// A later call may mutate what the earlier calls stored; the copy follows it
	// in a separate call, as a batch may not change the source it copies.
	result, err = store.Commit(ctx, [][]StorageWrite{
		{
			{Type: "document.change", ID: rewindDoc, Content: &DocumentContent{Version: 1, Kind: "delta", Ops: []Operation{{"s", []any{"value"}, 2}}}},
			{Type: "document.change", ID: taskDoc, Content: &DocumentContent{Version: 1, Kind: "base", Value: JSON{"value": 2}}},
		},
		{{Type: "conversation", Value: Conversation{ID: child, Parent: conversation, ParentAt: entry}}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stored != 2 || result.Refused != nil {
		t.Fatalf("stored=%d refused=%v", result.Stored, result.Refused)
	}
	result, err = store.Commit(ctx, [][]StorageWrite{{
		{Type: "document.copy", Record: &DocumentCreate{ID: copyDoc, Kind: "t.rewind", Scope: DocumentCreateScope{Kind: "conversation", ConversationID: child}, History: "rewindable", Fork: "asOf"}, Source: &DocumentCopySource{ID: rewindDoc, At: DocumentPoint{Seq: 4}}},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stored != 1 || result.Refused != nil {
		t.Fatalf("stored=%d refused=%v", result.Stored, result.Refused)
	}

	state, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Conversations[child].Parent != conversation || state.Conversations[child].ParentAt != entry {
		t.Fatalf("conversation ancestry %+v", state.Conversations[child])
	}
	stored := state.Entries[entry]
	if stored.Kind != "note" || len(stored.Value) != 0 {
		t.Fatalf("entry %+v", stored)
	}
	if data, ok := stored.Data.(map[string]any); !ok {
		t.Fatalf("entry data type %T", stored.Data)
	} else if fmt.Sprint(data["n"]) != "1" {
		t.Fatalf("entry %+v", stored)
	}
	if stored.Seq == 0 || stored.Position == 0 {
		t.Fatalf("entry placement %+v", stored)
	}
	storedTask := state.Tasks[task]
	if storedTask.Kind != "t.work" || storedTask.Execution == nil || storedTask.Execution.Native == nil || storedTask.Execution.Native.Version != 1 {
		t.Fatalf("task %+v", storedTask)
	}
	if storedTask.Status != "pending" {
		t.Fatalf("task status %q", storedTask.Status)
	}
	if sub := state.Submissions[submission]; sub.Type != "write" || sub.RequestID != "r1" {
		t.Fatalf("submission %+v", sub)
	}
	if doc := state.Documents[rewindDoc]; doc.Version != 1 || doc.DeltasSinceBase != 1 || fmt.Sprint(doc.Value["value"]) != "2" {
		t.Fatalf("rewindable document %+v", doc)
	}
	if doc := state.Documents[taskDoc]; fmt.Sprint(doc.Value["value"]) != "2" {
		t.Fatalf("task document %+v", doc)
	}
	if doc := state.Documents[copyDoc]; fmt.Sprint(doc.Value["value"]) != "2" || doc.Scope != "conversation" || doc.Owner != child {
		t.Fatalf("copied document %+v", doc)
	}
	if state.Documents[rewindDoc].Retired {
		t.Fatal("retired before its batch")
	}

	// A refused batch leaves the batches before it durable, stops the call and
	// changes nothing itself. The address it names comes from the stored image,
	// so a command for a document that is not there is refused, not invented.
	refusedEntry := reserve()
	result, err = store.Commit(ctx, [][]StorageWrite{
		{{Type: "entry", Value: Entry{ID: refusedEntry, Conversation: conversation, Kind: "note"}}},
		{{Type: "document.retire", ID: reserve()}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stored != 1 || result.Refused == nil {
		t.Fatalf("stored=%d refused=%v", result.Stored, result.Refused)
	}
	state, err = store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := state.Entries[refusedEntry]; !ok {
		t.Fatal("batch before the refusal was not stored")
	}
	if state.Documents[rewindDoc].Retired {
		t.Fatal("refused call retired the focus document")
	}
	if _, err := store.Commit(ctx, [][]StorageWrite{{{Type: "document.retire", ID: rewindDoc}}}, nil); err != nil {
		t.Fatal(err)
	}
	state, err = store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Documents[rewindDoc].Retired {
		t.Fatal("retirement was not stored")
	}
}

// Native structural refusal control: this task names a conversation that was
// never created. Its refusal is independent of builtin-name admission; full
// source raw reference/replacement policy acceptance remains D12.
func TestPiDurableV150PublicStorageCommitRefusesUnrepresentableCommand(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(context.Background())
	ctx := context.Background()
	result, err := store.Commit(ctx, [][]StorageWrite{{
		{Type: "task", Value: TaskRecord{ID: 2, Conversation: 1, Kind: "pi.generation", Version: 1, Input: &TaskValue{Present: true, Value: JSON{}}, State: TaskState{Status: "pending", Checkpoint: map[string]any{}}}},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stored != 0 || result.Refused == nil {
		t.Fatalf("stored=%d refused=%v", result.Stored, result.Refused)
	}
	state, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Tasks) != 0 {
		t.Fatalf("refused task became durable: %+v", state.Tasks)
	}
}

// The command union is translated without letting a caller's Go value bypass
// global identity rules. Explicit unminted IDs are accepted (pinned capture,
// storage_explicit_id_v150_test.go); recreating an existing record is refused.
// TestPiDurableV150PublicStorageCommitEnforcesIDOwnership replays the original
// conformance expectation: the reserved root may be created once by its owner,
// and a second claim of the same identity is refused.
// Source: storage-conformance.test.ts root creation and duplicate rejection.
func TestPiDurableV150PublicStorageCommitEnforcesIDOwnership(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(context.Background())
	ctx := context.Background()
	result, err := store.Commit(ctx, [][]StorageWrite{{
		{Type: "conversation", Value: Conversation{ID: 1}},
	}}, nil)
	if err != nil || result.Stored != 1 || result.Refused != nil {
		t.Fatalf("root creation: %+v %v", result, err)
	}
	result, err = store.Commit(ctx, [][]StorageWrite{{
		{Type: "conversation", Value: Conversation{ID: 1}},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stored != 0 || result.Refused == nil {
		t.Fatalf("stored=%d refused=%v", result.Stored, result.Refused)
	}
}

func TestPiDurableV150PublicStoragePiNamespaceRoundTrip(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close(bg) }()
	var receipts [][2]uint64
	commands := []StorageWrite{}
	for index, kind := range []string{"pi.example", "pi.compaction"} {
		commands = append(commands, StorageWrite{Type: "task", Value: TaskRecord{ID: ID(index + 2), Conversation: 1, Kind: kind, Version: 1, Input: &TaskValue{Present: true, Value: JSON{"reason": "manual"}}, State: TaskState{Status: "pending", Checkpoint: JSON{"phase": "select"}}}})
	}
	result, err := store.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: 1}}}, commands}, func(index int, seq uint64) { receipts = append(receipts, [2]uint64{uint64(index), seq}) })
	if err != nil || result.Stored != 2 || result.Refused != nil {
		t.Fatal("source-accepted pi namespace records refused", result, err)
	}
	if !reflect.DeepEqual(receipts, [][2]uint64{{0, 1}, {1, 2}}) {
		t.Fatal(receipts)
	}
	for phase := range 2 {
		state, err := store.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for index, kind := range []string{"pi.example", "pi.compaction"} {
			record, err := CanonicalTask(state.Tasks[ID(index+2)], DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := record.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			var got, want JSON
			if err := decodeStrict(encoded, DefaultLimits(), DefaultLimits().MaxRecordBytes, &got); err != nil {
				t.Fatal(err)
			}
			literal := fmt.Sprintf(`{"id":%d,"conversationId":1,"kind":%q,"version":1,"input":{"reason":"manual"},"background":false,"abortRequested":false,"state":{"status":"pending","checkpoint":{"phase":"select"}}}`, index+2, kind)
			if err := decodeStrict([]byte(literal), DefaultLimits(), DefaultLimits().MaxRecordBytes, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatal("public record changed", phase, got, want)
			}
		}
		if phase == 0 {
			if err := store.Close(bg); err != nil {
				t.Fatal(err)
			}
			store, err = OpenSQLite(store.path, SQLiteOptions{Prefix: store.prefix})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

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

// Source-derived from the original output in storage-boundaries.json: a fresh
// raw Storage holds no conversation at all, including no reserved root 1. The
// reserved root is bootstrapped by its owner (Harness.Root), never fabricated by
// Storage.
func TestV150StorageFreshRootAbsent(t *testing.T) {
	raw, err := os.ReadFile("testdata/pi-durable-v150/storage-boundaries.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		FreshRootPresent bool
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.FreshRootPresent {
		t.Fatal("fixture recapture disagrees with the original fresh root")
	}
	sqliteStoreFresh(t, func(t *testing.T, b sqliteFixture) {
		state := snap(t, b.store)
		if _, ok := state.Conversations[1]; ok != fixture.FreshRootPresent {
			t.Fatalf("fresh root present %v want %v", ok, fixture.FreshRootPresent)
		}
		if len(state.Conversations) != 0 || state.HighWater != 1 {
			t.Fatalf("fresh raw state %+v", state)
		}
		rows, err := b.store.ScanConversations(bg, ConversationQuery{}, 10, nil)
		if err != nil || len(rows.Items) != 0 {
			t.Fatalf("fresh raw conversations %v %v", rows, err)
		}
		// The original allocator starts above the reserved root (nextId = 2), so the
		// first mint is 2 and never reproduces the reserved root ID.
		if got := mint(t, b.store); got != 2 {
			t.Fatalf("first mint %d want 2", got)
		}
		// The reserved root can still be created explicitly by its owner, matching
		// tx.createRootConversation() staging conversation 1.
		apply(t, b.store, Write{Op: "create-conversation", Conversation: &Conversation{ID: 1}})
		if _, ok := snap(t, b.store).Conversations[1]; !ok {
			t.Fatal("root bootstrap not stored")
		}
		rejected(t, secondCreateRoot(b.store))
		store := b.reopen()
		if _, ok := snap(t, store).Conversations[1]; !ok {
			t.Fatal("root bootstrap lost on reopen")
		}
		if got := mint(t, store); got != 3 {
			t.Fatalf("reopened mint %d want 3", got)
		}
	})
}

func secondCreateRoot(s Storage) error {
	_, err := s.Apply(bg, Batch{[]Write{{Op: "create-conversation", Conversation: &Conversation{ID: 1}}}})
	return err
}

// Source-derived from the actual original output in storage-boundaries.json.
// This packet isolates empty batches and empty continue reasons; explicit IDs
// are covered in storage_explicit_id_v150_test.go. No fixture payload is
// rewritten.
func TestV150StorageEmptyBatch(t *testing.T) {
	raw, err := os.ReadFile("testdata/pi-durable-v150/storage-boundaries.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		EmptyBatch struct{ Stored int }
		Receipts   []struct {
			Index int
			Seq   uint64
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "store.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close(bg) }()
	before := snap(t, store)
	var receipts [][2]uint64
	result, err := store.Commit(bg, [][]StorageWrite{{}}, func(i int, seq uint64) { receipts = append(receipts, [2]uint64{uint64(i), seq}) })
	if err != nil || result.Refused != nil || result.Stored != fixture.EmptyBatch.Stored {
		t.Fatalf("empty batch: %+v %v", result, err)
	}
	want := [][2]uint64{{uint64(fixture.Receipts[0].Index), fixture.Receipts[0].Seq}}
	if !reflect.DeepEqual(receipts, want) {
		t.Fatalf("receipts %v want %v", receipts, want)
	}
	after := snap(t, store)
	if after.Seq != before.Seq+1 || after.HighWater != before.HighWater || !reflect.DeepEqual(after.Conversations, before.Conversations) || len(after.Entries) != 0 {
		t.Fatal("empty commit changed records/allocator", after)
	}
	// Empty outer calls consume no sequence; each inner empty batch consumes one.
	result, err = store.Commit(bg, nil, nil)
	if err != nil || result.Stored != 0 || result.Refused != nil || snap(t, store).Seq != after.Seq {
		t.Fatalf("empty call: %+v %v", result, err)
	}
	result, err = store.Commit(bg, [][]StorageWrite{nil, {}}, nil)
	if err != nil || result.Stored != 2 || result.Refused != nil {
		t.Fatalf("two empty batches: %+v %v", result, err)
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	store, err = OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if snap(t, store).Seq != 3 {
		t.Fatal("empty commits lost on replay")
	}
	if err := store.Reclaim(bg); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	store, err = OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if snap(t, store).Seq != 3 {
		t.Fatal("empty commits lost on checkpoint replay")
	}
}

func TestV150SubmissionEmptyContinueReason(t *testing.T) {
	raw, err := os.ReadFile("testdata/pi-durable-v150/storage-boundaries.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Conversation     Conversation
		Submission       SubmissionRecord
		StoredSubmission json.RawMessage
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		for mint(t, b.store) < fixture.Submission.ID {
		}
		result, err := b.store.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: fixture.Conversation}}, {{Type: "submission", Value: fixture.Submission}}}, nil)
		if err != nil || result.Refused != nil || result.Stored != 2 {
			t.Fatalf("continue empty reason: %+v %v", result, err)
		}
		check := func(store Storage) {
			state := snap(t, store)
			got := publicSubmission(state.Submissions[fixture.Submission.ID])
			var expected any
			if err := json.Unmarshal(fixture.StoredSubmission, &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(canonicalJSON(t, got), expected) {
				t.Fatalf("stored %s want %s", marshalOrFail(t, got), fixture.StoredSubmission)
			}
		}
		check(b.store)
		check(b.reopen())
	})
}

func TestV150SubmissionEmptyReasonSettlement(t *testing.T) {
	for _, kind := range []string{"input", "write", "continue"} {
		t.Run(kind, func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			seedRootConversation(t, store)
			session, err := OpenSession(store)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close(bg)
			var id ID
			_, err = session.Commit(bg, func(tx *Tx) (any, error) {
				var err error
				status := "queued"
				if kind == "continue" {
					status = "placed"
				}
				createdSubmission, err := tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: kind, Status: status})
				id = createdSubmission.ID
				return nil, err
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = session.Commit(bg, func(tx *Tx) (any, error) {
				return nil, tx.SettleSubmission(id, SubmissionSettlement{Status: "unanswered", Reason: ""})
			})
			if err != nil {
				t.Fatal("empty reason settlement", err)
			}
			_, err = session.Commit(bg, func(tx *Tx) (any, error) {
				record, ok, err := tx.submissionRecord(id)
				if err != nil {
					return nil, err
				}
				if !ok || record.Status != "unanswered" || record.Reason != "" {
					t.Fatalf("settled %+v", record)
				}
				return nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestV150StorageEmptyBatchRollback(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	failure := reject("empty group backend failure")
	store.appendFrames = func([]storageFrame) error { return failure }
	receipts := 0
	result, err := store.Commit(bg, [][]StorageWrite{{}, {}}, func(int, uint64) { receipts++ })
	if !errors.Is(err, failure) || result.Stored != 0 || receipts != 0 || snap(t, store).Seq != 0 {
		t.Fatalf("failed empty group: %+v %v receipts=%d", result, err, receipts)
	}
}

func TestV150StorageEmptyBatchRealRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close(bg) }()
	if err := store.exec("CREATE TRIGGER reject_second_empty BEFORE INSERT ON frames WHEN NEW.kind=2 AND NEW.ordinal=2 BEGIN SELECT RAISE(ABORT,'second empty frame rejected'); END;"); err != nil {
		t.Fatal(err)
	}
	receipts := 0
	result, err := store.Commit(bg, [][]StorageWrite{{}, {}}, func(int, uint64) { receipts++ })
	if err == nil || result.Stored != 0 || receipts != 0 || snap(t, store).Seq != 0 {
		t.Fatalf("SQL rollback: %+v %v receipts=%d", result, err, receipts)
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	store, err = OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if snap(t, store).Seq != 0 {
		t.Fatal("first empty frame escaped rollback")
	}
	if err := store.exec("DROP TRIGGER reject_second_empty"); err != nil {
		t.Fatal(err)
	}
	result, err = store.Commit(bg, [][]StorageWrite{{}, {}}, nil)
	if err != nil || result.Refused != nil || result.Stored != 2 {
		t.Fatalf("after rollback: %+v %v", result, err)
	}
}

func TestV150StorageEmptyBatchRefusalKeepsPrefix(t *testing.T) {
	sqliteStoreFresh(t, func(t *testing.T, b sqliteFixture) {
		var receipts []uint64
		result, err := b.store.Commit(bg, [][]StorageWrite{{}, {{Type: "unknown"}}, {}}, func(_ int, seq uint64) { receipts = append(receipts, seq) })
		if err != nil || result.Stored != 1 || result.Refused == nil || !reflect.DeepEqual(receipts, []uint64{1}) {
			t.Fatalf("refusal prefix: %+v %v %v", result, err, receipts)
		}
		if state := snap(t, b.reopen()); state.Seq != 1 || len(state.Entries) != 0 {
			t.Fatal("refusal prefix lost", state)
		}
	})
}

func TestV150ContinueReasonStillRequired(t *testing.T) {
	var record SubmissionRecord
	if err := json.Unmarshal([]byte(`{"id":2,"conversationId":1,"type":"continue","status":"unanswered"}`), &record); err == nil {
		t.Fatal("missing reason accepted on the public wire")
	}
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	id := mint(t, store)
	for _, value := range []JSON{{}, {"errorCode": nil}, {"errorCode": 42}, {"errorCode": "", "answerEntry": ID(99)}} {
		_, err := store.Apply(bg, Batch{Writes: []Write{{Op: "put-submission", Submission: &Submission{ID: id, Conversation: 1, Type: "continue", Status: "failed", Value: value}}}})
		if err == nil || snap(t, store).Seq != 0 {
			t.Fatalf("invalid reason/union accepted: %v %v", value, err)
		}
	}
}

// Source-derived from storage-boundaries.json: the original raw Storage accepts
// an explicit, never-minted conversation ID and raises later minting above it.
// Root preinstallation is still a separate open D28 gap, so this packet does
// not assert freshRootPresent.
func TestV150StorageExplicitIDRaisesAllocator(t *testing.T) {
	raw, err := os.ReadFile("testdata/pi-durable-v150/storage-boundaries.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Conversation       Conversation
		ExplicitID         struct{ Stored int }
		StoredConversation struct{ ID ID }
		NextMint           ID
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		result, err := b.store.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: fixture.Conversation}}}, nil)
		if err != nil || result.Refused != nil || result.Stored != fixture.ExplicitID.Stored {
			t.Fatalf("explicit ID: %+v %v", result, err)
		}
		if _, ok := snap(t, b.store).Conversations[fixture.StoredConversation.ID]; !ok {
			t.Fatal("explicit conversation not stored")
		}
		if got := mint(t, b.store); got != fixture.NextMint {
			t.Fatalf("next mint %d want %d", got, fixture.NextMint)
		}
		reopened := b.reopen()
		if _, ok := snap(t, reopened).Conversations[fixture.StoredConversation.ID]; !ok {
			t.Fatal("explicit conversation lost on reopen")
		}
		if got := mint(t, reopened); got != fixture.NextMint+1 {
			t.Fatalf("reopened mint %d", got)
		}
	})
}

func TestV150StorageExplicitIDGroupAndCollisions(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		// A later batch may reference an ID raised by an earlier batch; an
		// unminted ID below the high-water mark is also accepted when unused.
		result, err := b.store.Commit(bg, [][]StorageWrite{
			{{Type: "conversation", Value: Conversation{ID: 50}}},
			{{Type: "conversation", Value: Conversation{ID: 20}}},
		}, nil)
		if err != nil || result.Refused != nil || result.Stored != 2 {
			t.Fatalf("group: %+v %v", result, err)
		}
		// Global type collision refuses the batch and leaves the allocator.
		result, err = b.store.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: 80}}, {Type: "conversation", Value: Conversation{ID: 50}}}}, nil)
		if err != nil || result.Refused == nil || result.Stored != 0 {
			t.Fatalf("collision: %+v %v", result, err)
		}
		if _, ok := snap(t, b.store).Conversations[80]; ok {
			t.Fatal("refused batch partly stored")
		}
		if got := mint(t, b.store); got != 51 {
			t.Fatalf("refused explicit ID raised allocator: %d", got)
		}
		result, err = b.store.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: ID(MaxID + 1)}}}}, nil)
		if err != nil || result.Refused == nil {
			t.Fatalf("out-of-domain ID: %+v %v", result, err)
		}
		if err := b.reopen().(*SQLiteStorage).Reclaim(bg); err != nil {
			t.Fatal(err)
		}
		if got := mint(t, b.reopen()); got != 52 {
			t.Fatalf("checkpoint mint %d", got)
		}
	})
}

func TestV150StorageExplicitIDRealRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close(bg) }()
	if err := store.exec("CREATE TRIGGER reject_explicit BEFORE INSERT ON frames WHEN NEW.kind=2 BEGIN SELECT RAISE(ABORT,'explicit rejected'); END;"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: 99}}}}, nil); err == nil {
		t.Fatal("trigger did not fail")
	}
	if err := store.exec("DROP TRIGGER reject_explicit"); err != nil {
		t.Fatal(err)
	}
	if got := mint(t, store); got != 2 {
		t.Fatalf("rolled-back explicit ID raised allocator: %d", got)
	}
}

// Session ownership keeps native reservation: an unreserved ID inside a
// Session transaction is still rejected.
func TestV150SessionStillRequiresReservedIDs(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	seedRootConversation(t, store)
	s, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	if _, err = s.Commit(bg, func(tx *Tx) (any, error) {
		return nil, tx.appendEntryRecord(Entry{ID: 99, Conversation: 1, Kind: "test", Value: JSON{}})
	}); err == nil {
		t.Fatal("unreserved Session ID accepted")
	}
}

func TestV150StorageExplicitIDReplayRejectsForgedHighWater(t *testing.T) {
	for _, forged := range []int{98, 150} {
		path := filepath.Join(t.TempDir(), "store.sqlite")
		store, err := OpenSQLite(path, SQLiteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if result, err := store.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: 99}}}}, nil); err != nil || result.Stored != 1 {
			t.Fatal(result, err)
		}
		if err := store.exec("UPDATE frames SET highwater=? WHERE kind=2", forged); err != nil {
			t.Fatal(err)
		}
		_ = store.Close(bg)
		if reopened, err := OpenSQLite(path, SQLiteOptions{}); err == nil {
			_ = reopened.Close(bg)
			t.Fatalf("forged high water %d accepted", forged)
		}
	}
}

// TestSQLiteRootConfigVersionCompatibility pins the persisted-format policy that
// comes with removing the fabricated reserved root:
//
//   - a newly created store records config version 2 and holds no conversation,
//     so the reserved root only exists once its owner writes it;
//   - a store persisted before this split (version 1) never wrote a root, so the
//     implicit reserved root is restored for it and its existing data keeps the
//     semantics its writer intended;
//   - an unknown config version is corrupt rather than being guessed at.
func TestSQLiteRootConfigVersionCompatibility(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var data []byte
	if err := store.db.QueryRowContext(bg, store.schemaSQL("SELECT value FROM config WHERE id=1")).Scan(&data); err != nil {
		t.Fatal(err)
	}
	var config sqliteConfig
	if err := decodeStrict(data, DefaultLimits(), 16<<10, &config); err != nil {
		t.Fatal(err)
	}
	if config.Version != sqliteConfigVersion || sqliteConfigVersion != 2 {
		t.Fatalf("new store config version %d want 2", config.Version)
	}
	if _, ok := store.state.Conversations[1]; ok {
		t.Fatal("version 2 store fabricated the reserved root")
	}
	_, err = store.Apply(bg, Batch{Writes: []Write{{Op: "append-entry", Entry: &Entry{ID: mint(t, store), Conversation: 1, Kind: "message", Value: JSON{}}}}})
	rejected(t, err)
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}

	// A version 1 file predates the split: it must still read, keep its implicit
	// root and accept new conversation-scoped data.
	if err := downgradeSQLiteConfigFile(t, path, 1); err != nil {
		t.Fatal(err)
	}
	legacy, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	state, err := legacy.Snapshot(bg)
	if err != nil || state.Seq != 0 || len(state.Conversations) != 1 {
		t.Fatalf("version 1 store %+v err=%v", state, err)
	}
	root, ok := state.Conversations[1]
	if !ok || root.ID != 1 {
		t.Fatalf("version 1 store lost its implicit root: %+v", state.Conversations)
	}
	_, err = legacy.Apply(bg, Batch{Writes: []Write{{Op: "create-conversation", Conversation: &Conversation{ID: 1}}}})
	rejected(t, err)
	id := mint(t, legacy)
	if _, err := legacy.Apply(bg, Batch{Writes: []Write{{Op: "append-entry", Entry: &Entry{ID: id, Conversation: 1, Kind: "message", Value: JSON{}}}}}); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(bg); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(bg)
	if state, err := reopened.Snapshot(bg); err != nil || state.Seq != 1 || state.Entries[id].Conversation != 1 {
		t.Fatalf("version 1 data lost: %+v err=%v", state, err)
	}
}

// TestSQLiteUnknownConfigVersionIsCorrupt keeps the persisted-format gate closed:
// a config the native reader cannot interpret is never upgraded implicitly.
func TestSQLiteUnknownConfigVersionIsCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := downgradeSQLiteConfig(store, 3); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSQLite(path, SQLiteOptions{}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("unknown config version: %v", err)
	}
}

// downgradeSQLiteConfigFile rewrites the config row of a closed store file so a
// reopen can be observed through the production read path.
func downgradeSQLiteConfigFile(t *testing.T, path string, version uint64) error {
	t.Helper()
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		return err
	}
	defer store.Close(bg)
	return downgradeSQLiteConfig(store, version)
}

// TestV150PublicCommitRefusalCarriesOriginalValue ports the original boundary
// semantics: CommitResult.refused is `unknown`, so the value a batch was refused
// with survives the public boundary verbatim, including present null/undefined
// and falsy values, while a Go error keeps its identity.
// Source: packages/durable/src/types.ts:1314 (CommitResult.refused?: unknown).
func TestV150PublicCommitRefusalCarriesOriginalValue(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	ctx := bg

	// A real refusal: an unrepresentable command type.
	result, err := store.Commit(ctx, [][]StorageWrite{{}, {{Type: "unknown"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stored != 1 || result.Refused == nil {
		t.Fatalf("refusal presence lost: %+v", result)
	}
	if _, ok := result.Refused.Value().(error); !ok {
		t.Fatalf("native cause lost: %#v", result.Refused.Value())
	}
	if !errors.Is(result.Refused.AsError(), result.Refused.Value().(error)) {
		t.Fatal("AsError lost the original cause identity")
	}

	// Present non-error values must survive verbatim rather than collapsing to a
	// generic error string.
	for _, value := range []any{commitUndefinedValue{}, nil, false, 0, ""} {
		refused := newCommitRefusal(value)
		if refused.Value() == nil && value == nil && refused.Value() != nil {
			t.Fatal("nil value lost")
		}
		if !reflect.DeepEqual(refused.Value(), value) {
			t.Fatalf("value %v became %#v", value, refused.Value())
		}
		if refused.AsError() == nil {
			t.Fatalf("present refusal %v projected as absent", value)
		}
	}

	// Absent refusal stays absent.
	if result, err := store.Commit(ctx, [][]StorageWrite{{}}, nil); err != nil || result.Stored != 1 || result.Refused != nil {
		t.Fatalf("empty batch: %+v %v", result, err)
	}
}

// Captured unchanged from original v1.5.0 Session -> MemoryStorage.commit.
func TestPiDurableV150StorageWriteWire(t *testing.T) {
	data, err := os.ReadFile("testdata/pi-durable-v150/storage-writes.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Target  string              `json:"target"`
		Package string              `json:"package"`
		Version string              `json:"version"`
		Types   []string            `json:"types"`
		Batches [][]json.RawMessage `json:"batches"`
	}
	if err = json.Unmarshal(data, &fixture); err != nil || fixture.Target != "f9a26365568127461c47a07a37dcb42c140a3ac5" || fixture.Package != "@earendil-works/pi-durable" || fixture.Version != "1.5.0" || len(fixture.Batches) != 12 || len(fixture.Types) != 8 {
		t.Fatal("fixture provenance", err)
	}
	for batch, writes := range fixture.Batches {
		for index, raw := range writes {
			t.Run(fmt.Sprintf("batch%d/write%d", batch+1, index+1), func(t *testing.T) {
				var write StorageWrite
				if err := json.Unmarshal(raw, &write); err != nil {
					t.Fatalf("valid original command rejected: %v", err)
				}
				got, err := json.Marshal(write)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(dropinJSON(t, got), dropinJSON(t, raw)) {
					t.Fatalf("got %s\noriginal %s", got, raw)
				}
			})
		}
	}
}

func TestPiDurableV150StorageWriteRejectsMalformedUnion(t *testing.T) {
	for _, raw := range []string{
		`null`, `{}`, `{"type":"unknown"}`,
		`{"type":"conversation","value":null}`,
		`{"type":"conversation","value":{"id":2},"id":2}`,
		`{"type":"entry","value":{"id":2,"conversationId":1,"kind":"note"},"op":"append-entry"}`,
		`{"type":"document.retire","id":2,"content":null}`,
		`{"type":"document.retire","id":0}`,
		`{"type":"document.change","id":2,"content":{"version":1,"kind":"delta","ops":null}}`,
		`{"type":"document.change","id":2,"content":{"version":1,"kind":"delta","ops":[],"value":null}}`,
		`{"type":"document.change","id":2,"content":{"version":1,"kind":"base","value":null}}`,
		`{"type":"document.change","id":2,"content":{"version":0,"kind":"base","value":{}}}`,
		`{"type":"document.copy","record":{"id":3,"kind":"x","scope":{"kind":"session"}},"source":{"id":2}}`,
		`{"type":"document.copy","record":{"id":3,"kind":"x","scope":{"kind":"session"}},"source":{"id":2,"at":null}}`,
		`{"type":"document.create","record":{"id":2,"kind":"x","scope":{"kind":"session","taskId":3}},"content":{"version":1,"kind":"base","value":{}}}`,
		`{"type":"document.create","record":{"id":2,"kind":"x","createdAt":1,"scope":{"kind":"session"}},"content":{"version":1,"kind":"base","value":{}}}`,
		`{"type":"document.create","record":{"id":2,"kind":"x","scope":{"kind":"session"}},"content":{"version":1,"kind":"delta","ops":[]}}`,
		`{"type":"submission","value":{"id":2,"conversationId":1,"type":"continue","status":"queued"}}`,
		`{"type":"submission","value":{"id":2,"conversationId":1,"type":"write","status":"unanswered"}}`,
		`{"type":"submission","value":{"id":2,"conversationId":1,"type":"write","status":"queued","detail":null}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			original := StorageWrite{Type: "document.retire", ID: 99}
			got := original
			if err := json.Unmarshal([]byte(raw), &got); err == nil {
				t.Fatalf("accepted %s", raw)
			}
			if !reflect.DeepEqual(got, original) {
				t.Fatal("failed decode mutated destination")
			}
		})
	}
}

func TestPiDurableV150StorageWritePointsAndEmptyContainers(t *testing.T) {
	for _, raw := range []string{
		`{"type":"document.change","id":2,"content":{"version":1,"kind":"delta","ops":[]}}`,
		`{"type":"document.change","id":2,"content":{"version":1,"kind":"base","value":{}}}`,
		`{"type":"document.create","record":{"id":2,"kind":"x","key":"","scope":{"kind":"session"}},"content":{"version":1,"kind":"base","value":{}}}`,
		`{"type":"document.copy","record":{"id":3,"kind":"x","scope":{"kind":"conversation","conversationId":1},"history":"latest","fork":"current"},"source":{"id":2,"at":"current"}}`,
		`{"type":"document.copy","record":{"id":3,"kind":"x","scope":{"kind":"conversation","conversationId":1},"history":"rewindable","fork":"asOf"},"source":{"id":2,"at":0}}`,
		`{"type":"document.change","id":2,"content":{"version":1,"kind":"base","value":{"n":900719925474099312345,"negative":-0,"exp":1e100}}}`,
	} {
		var write StorageWrite
		if err := json.Unmarshal([]byte(raw), &write); err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(write)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(dropinJSON(t, got), dropinJSON(t, []byte(raw))) {
			t.Fatalf("got %s want %s", got, raw)
		}
	}
}

type storageWireMarshalProbe struct{ called *bool }

func (p storageWireMarshalProbe) MarshalJSON() ([]byte, error) {
	*p.called = true
	return []byte(`{}`), nil
}

func TestPiDurableV150StorageWriteRejectsMarshalAuthorityAndBounds(t *testing.T) {
	called := false
	var typedNil *Entry
	var deep any = nil
	for i := 0; i <= maximumDurableLimits().MaxDepth; i++ {
		deep = []any{deep}
	}
	for _, write := range []StorageWrite{
		{Type: "entry", Value: typedNil},
		{Type: "entry", Value: storageWireMarshalProbe{&called}},
		{Type: "conversation", Value: &Entry{}},
		{Type: "entry", Value: Entry{ID: 2, Conversation: 1, Kind: "x", HasData: true, Data: storageWireMarshalProbe{&called}}},
		{Type: "document.change", ID: 2, Content: &DocumentContent{Version: 1, Kind: "base", Value: JSON{"n": storageWireMarshalProbe{&called}}}},
		{Type: "document.change", ID: 2, Content: &DocumentContent{Version: 1, Kind: "base", Value: JSON{"n": deep}}},
	} {
		if _, err := json.Marshal(write); err == nil {
			t.Fatalf("accepted invalid command %#v", write)
		}
	}
	if called {
		t.Fatal("invoked caller marshal authority")
	}
}

// Public codecs must not rewrite persisted native op/owner/value/point fields.
func TestPiDurableV150StorageWriteKeepsPrivateEnvelope(t *testing.T) {
	const raw = `{"writes":[{"op":"create-conversation","conversation":{"id":2,"parent":1,"parentAt":4,"owner":3}},{"op":"append-entry","entry":{"id":4,"conversation":1,"kind":"message","value":{"role":"user","text":"hi"}}},{"op":"put-submission","submission":{"id":5,"conversation":1,"type":"write","status":"pending","value":{"detail":null}}},{"op":"copy-document","document":{"id":7,"scope":"conversation","owner":2,"kind":"x","key":"","version":0,"value":null},"source":{"id":6,"at":{"seq":1}}}]}`
	var batch Batch
	if err := decodeStrict([]byte(raw), maximumDurableLimits(), maximumDurableLimits().MaxRecordBytes, &batch); err != nil {
		t.Fatal(err)
	}
	got, err := encodeBoundedOrder(batch, maximumDurableLimits(), maximumDurableLimits().MaxRecordBytes, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dropinJSON(t, got), dropinJSON(t, []byte(raw))) {
		t.Fatalf("private storage envelope changed: %s", got)
	}
}

// Source Storage.conversation/entry overloads return records and original commit
// sequence separately; neither lookup depends on a caller-side native adapter.
func TestStorageCanonicalConversationAndEntryLookupSurface(t *testing.T) {
	s := taskConformanceStoreV150(t)
	root, found, err := s.Conversation(bg, 1)
	if err != nil || !found || root.ID != 1 {
		t.Fatal(root, found, err)
	}
	first, cut, excluded, child, own := mint(t, s), mint(t, s), mint(t, s), mint(t, s), mint(t, s)
	input := JSON{"nested": []any{1}}
	result, err := s.Commit(bg, [][]StorageWrite{
		{{Type: "entry", Value: Entry{ID: first, Conversation: 1, Kind: "note", HasData: true, Data: input}}, {Type: "entry", Value: Entry{ID: cut, Conversation: 1, Kind: "note"}}, {Type: "entry", Value: Entry{ID: excluded, Conversation: 1, Kind: "note"}}},
		{{Type: "conversation", Value: Conversation{ID: child, Parent: 1, ParentAt: cut}}},
		{{Type: "entry", Value: Entry{ID: own, Conversation: child, Kind: "note"}}},
	}, nil)
	if err != nil || result.Refused != nil {
		t.Fatal(result, err)
	}
	input["nested"].([]any)[0] = "input mutation"
	for _, check := range []struct {
		ids                 []ID
		entry, conversation ID
		seq                 uint64
		found               bool
	}{
		{[]ID{first}, first, 1, 2, true}, {[]ID{child, first}, first, 1, 2, true}, {[]ID{child, cut}, cut, 1, 2, true}, {[]ID{child, own}, own, child, 4, true},
		{[]ID{child, excluded}, 0, 0, 0, false}, {[]ID{1, own}, 0, 0, 0, false}, {[]ID{0}, 0, 0, 0, false},
	} {
		stored, found, err := s.Entry(bg, check.ids...)
		if err != nil {
			t.Fatal(err)
		}
		if found != check.found {
			t.Fatal("visibility", check.ids, found)
		}
		if !found {
			continue
		}
		wire, err := json.Marshal(stored)
		if err != nil {
			t.Fatal(err)
		}
		var actual struct {
			Entry     Entry  `json:"entry"`
			CommitSeq uint64 `json:"commitSeq"`
		}
		if err = json.Unmarshal(wire, &actual); err != nil {
			t.Fatal(err)
		}
		if actual.Entry.ID != check.entry || actual.Entry.Conversation != check.conversation || actual.CommitSeq != check.seq {
			t.Fatalf("lookup %v got=%s", check.ids, wire)
		}
		if stored.Entry.Seq != 0 || stored.Entry.Position != 0 {
			t.Fatal("private placement leaked through entry record", stored)
		}
		if check.entry == first {
			if !equalJSONValue(stored.Entry.Data, JSON{"nested": []any{1}}) {
				t.Fatal("entry input/read alias", stored.Entry.Data)
			}
			stored.Entry.Data.(map[string]any)["nested"].([]any)[0] = "read mutation"
		}
	}
	stored, found, err := s.Entry(bg, first)
	if err != nil || !found || !equalJSONValue(stored.Entry.Data, JSON{"nested": []any{1}}) {
		t.Fatal("retained entry alias", stored, found, err)
	}
}

// Literal src/testing/storage-conformance.ts:378-398, pinned f9a26365.
func TestStorageConversationAscendingCursorPagination(t *testing.T) {
	s := taskConformanceStoreV150(t)
	second, third := mint(t, s), mint(t, s)
	result, err := s.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: third}}, {Type: "conversation", Value: Conversation{ID: second}}}}, nil)
	if err != nil || result.Refused != nil {
		t.Fatal(result, err)
	}
	first, err := s.ScanConversations(bg, ConversationQuery{}, 2, nil)
	if err != nil || len(first.Items) != 2 || first.Items[0].ID != 1 || first.Items[1].ID != second || first.Next == nil {
		t.Fatal(first, err)
	}
	wire, err := json.Marshal(first.Next)
	if err != nil {
		t.Fatal(err)
	}
	var cursor Cursor
	if err = json.Unmarshal(wire, &cursor); err != nil {
		t.Fatal(err)
	}
	next, err := s.ScanConversations(bg, ConversationQuery{}, 2, cursor)
	if err != nil || len(next.Items) != 1 || next.Items[0].ID != third || next.Next != nil {
		t.Fatal(next, err)
	}
}

// Literal src/testing/storage-conformance.ts:451-616. Assertions exercise the
// public global/visible entry overloads, commitSeq, deep caps, both scan orders,
// cursor continuation, markers, inclusive bounds and missing values/errors.
func TestStorageDeepForkHistoryOriginalEntryReadContract(t *testing.T) {
	s := taskConformanceStoreV150(t)
	commit := func(writes ...StorageWrite) uint64 {
		t.Helper()
		var seq uint64
		result, err := s.Commit(bg, [][]StorageWrite{writes}, func(_ int, value uint64) { seq = value })
		if err != nil || result.Refused != nil || result.Stored != 1 {
			t.Fatal(result, err)
		}
		return seq
	}
	record := func(id, conversation ID, kind string, head ID) Entry {
		return Entry{ID: id, Conversation: conversation, Kind: kind, Head: head}
	}
	write := func(entry Entry) StorageWrite { return StorageWrite{Type: "entry", Value: entry} }
	rootFirst, rootFork, rootExcluded := mint(t, s), mint(t, s), mint(t, s)
	rootSeq := commit(write(record(rootFirst, 1, "message", 0)), write(record(rootFork, 1, "marker", rootFirst)), write(record(rootExcluded, 1, "message", 0)))
	child := mint(t, s)
	commit(StorageWrite{Type: "conversation", Value: Conversation{ID: child, Parent: 1, ParentAt: rootFork}})
	childFork, childExcluded := mint(t, s), mint(t, s)
	commit(write(record(childFork, child, "note", 0)), write(record(childExcluded, child, "message", 0)))
	rootLater := mint(t, s)
	commit(write(record(rootLater, 1, "message", 0)))
	grand := mint(t, s)
	commit(StorageWrite{Type: "conversation", Value: Conversation{ID: grand, Parent: child, ParentAt: childFork}})
	grandHead, grandTail := mint(t, s), mint(t, s)
	grandSeq := commit(write(record(grandHead, grand, "marker", grandHead)), write(record(grandTail, grand, "message", 0)))
	childLater := mint(t, s)
	commit(write(record(childLater, child, "message", 0)))
	scan := func(q EntryQuery, limit int, cursor Cursor, want ...ID) Page[Entry] {
		t.Helper()
		page, err := s.ScanEntries(bg, q, limit, cursor)
		if err != nil || !reflect.DeepEqual(historyIDs(page), want) {
			t.Fatalf("scan got=%v want=%v err=%v", historyIDs(page), want, err)
		}
		return page
	}
	first := scan(EntryQuery{Conversation: grand}, 2, nil, grandTail, grandHead)
	second := scan(EntryQuery{Conversation: grand}, 2, first.Next, childFork, rootFork)
	third := scan(EntryQuery{Conversation: grand}, 2, second.Next, rootFirst)
	if third.Next != nil {
		t.Fatal(third.Next)
	}
	up := EntryQuery{Conversation: grand, Order: Ascending}
	firstUp := scan(up, 2, nil, rootFirst, rootFork)
	secondUp := scan(up, 2, firstUp.Next, childFork, grandHead)
	thirdUp := scan(EntryQuery{Conversation: grand}, 2, secondUp.Next, grandTail)
	if thirdUp.Next != nil {
		t.Fatal(thirdUp.Next)
	}
	scan(EntryQuery{Conversation: grand, Order: Ascending, MinEntryID: rootFork, MaxEntryID: childFork}, 10, nil, rootFork, childFork)
	if _, err := s.ScanEntries(bg, EntryQuery{Conversation: grand, Order: Descending}, 2, firstUp.Next); err == nil || !strings.Contains(err.Error(), "cursor") {
		t.Fatal("opposite cursor order", err)
	}
	current, found, err := s.FindLatestHeadMarker(bg, grand, 0)
	if err != nil || !found || current.ID != grandHead || current.Head != grandHead {
		t.Fatal(current, found, err)
	}
	historical, found, err := s.FindLatestHeadMarker(bg, grand, childFork)
	if err != nil || !found || historical.ID != rootFork || historical.Head != rootFirst {
		t.Fatal(historical, found, err)
	}
	if _, found, err := s.FindLatestHeadMarker(bg, grand, rootFirst); err != nil || found {
		t.Fatal(found, err)
	}
	active := scan(EntryQuery{Conversation: grand, MinEntryID: current.Head}, 1, nil, grandTail)
	if active.Next == nil {
		t.Fatal("active continuation missing")
	}
	active = scan(EntryQuery{Conversation: grand, MinEntryID: current.Head}, 1, active.Next, grandHead)
	if active.Next != nil {
		t.Fatal(active.Next)
	}
	scan(EntryQuery{Conversation: grand, MinEntryID: historical.Head, MaxEntryID: childFork}, 10, nil, childFork, rootFork, rootFirst)
	for _, check := range []struct {
		id, owner ID
		seq       uint64
		kind      string
		head      ID
	}{
		{rootFirst, 1, rootSeq, "message", 0}, {rootFork, 1, rootSeq, "marker", rootFirst}, {grandHead, grand, grandSeq, "marker", grandHead}, {grandTail, grand, grandSeq, "message", 0},
	} {
		actual, found, err := s.Entry(bg, check.id)
		if err != nil || !found || actual.CommitSeq != check.seq {
			t.Fatal(actual, found, err)
		}
		want := StoredEntry{Entry: record(check.id, check.owner, check.kind, check.head), CommitSeq: check.seq}
		gotWire, err := json.Marshal(actual)
		if err != nil {
			t.Fatal(err)
		}
		wantWire, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		if string(gotWire) != string(wantWire) {
			t.Fatalf("global entry got=%s want=%s", gotWire, wantWire)
		}
	}
	if _, found, err := s.Entry(bg, 999999); err != nil || found {
		t.Fatal(found, err)
	}
	ancestor, found, err := s.Entry(bg, grand, rootFirst)
	if err != nil || !found || ancestor.Entry.ID != rootFirst || ancestor.CommitSeq != rootSeq {
		t.Fatal(ancestor, found, err)
	}
	inherited, found, err := s.Entry(bg, grand, childFork)
	if err != nil || !found || inherited.Entry.Conversation != child {
		t.Fatal(inherited, found, err)
	}
	own, found, err := s.Entry(bg, grand, grandTail)
	if err != nil || !found || own.CommitSeq != grandSeq {
		t.Fatal(own, found, err)
	}
	for _, id := range []ID{rootExcluded, rootLater, childExcluded, childLater, 999999} {
		if _, found, err := s.Entry(bg, grand, id); err != nil || found {
			t.Fatal("cut/missing entry visible", id, found, err)
		}
	}
	if _, found, err := s.Entry(bg, 1, grandHead); err != nil || found {
		t.Fatal("child visible from parent", found, err)
	}
	if _, _, err := s.Entry(bg, 999999, rootFirst); err == nil || !strings.Contains(err.Error(), "Unknown conversation") {
		t.Fatal("unknown conversation", err)
	}
	if _, err := s.ScanEntries(bg, EntryQuery{Conversation: 999999}, 10, nil); err == nil || !strings.Contains(err.Error(), "Unknown conversation") {
		t.Fatal("unknown scan conversation", err)
	}
}
