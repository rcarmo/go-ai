package durable

import (
	"context"
	"fmt"
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

// A record the native store cannot represent in its own envelope is refused
// before it becomes durable, never written in a different shape.
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
// the native ownership rules: an unreserved ID is refused, not stored.
func TestPiDurableV150PublicStorageCommitEnforcesIDOwnership(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(context.Background())
	ctx := context.Background()
	result, err := store.Commit(ctx, [][]StorageWrite{{
		{Type: "conversation", Value: Conversation{ID: 99}},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stored != 0 || result.Refused == nil {
		t.Fatalf("stored=%d refused=%v", result.Stored, result.Refused)
	}
}
