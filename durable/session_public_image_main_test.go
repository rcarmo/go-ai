package durable

import (
	"context"
	"testing"
)

func TestMainSessionPublicReadsExcludeUnpublishedGroup(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		batches, ids := groupEntryBatches(t, b.store, 2)
		seedRootConversation(t, b.store)
		s, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		result, err := s.core.commitBatches(bg, batches, nil, true)
		if err != nil || result.stored != 2 {
			t.Fatalf("group: %#v %v", result, err)
		}
		raw, err := b.store.Snapshot(bg)
		if err != nil || len(raw.Entries) != 2 {
			t.Fatalf("raw: %#v %v", raw, err)
		}
		t.Run("snapshot", func(t *testing.T) {
			image, err := s.Snapshot(bg)
			if err != nil || image.Seq != 1 || len(image.Entries) != 0 || image.HighWater < uint64(ids[1]) {
				t.Fatalf("published: %#v %v", image, err)
			}
			delete(image.Conversations, 1)
			again, err := s.Snapshot(bg)
			if err != nil || len(again.Conversations) != 1 {
				t.Fatalf("snapshot alias: %#v %v", again, err)
			}
		})
		t.Run("entries", func(t *testing.T) {
			page, err := s.ScanEntries(bg, EntryQuery{Conversation: 1}, 10, nil)
			if err != nil || len(page.Items) != 0 {
				t.Fatalf("published entries: %#v %v", page, err)
			}
		})
	})
}

func TestMainSessionPublicRecordScansUsePublishedImage(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		ids := []ID{mint(t, b.store), mint(t, b.store), mint(t, b.store)}
		seedRootConversation(t, b.store)
		s, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		batch := Batch{Writes: []Write{
			{Op: "create-conversation", Conversation: &Conversation{ID: ids[0]}},
			{Op: "put-task", Task: &Task{ID: ids[1], Conversation: ids[0], Kind: "custom", Status: "pending", Checkpoint: JSON{"x": 1}}},
			{Op: "put-submission", Submission: &Submission{ID: ids[2], Conversation: ids[0], Type: "write", Status: "pending", Value: JSON{"x": 1}}},
		}}
		if _, err := s.core.apply(bg, batch, true); err != nil {
			t.Fatal(err)
		}
		check := func(published bool) {
			t.Helper()
			want := 0
			if published {
				want = 1
			}
			conversations, err := s.ScanConversations(bg, ConversationQuery{}, 10, nil)
			if err != nil || len(conversations.Items) != 1+want {
				t.Fatalf("conversations: %+v %v", conversations, err)
			}
			tasks, err := s.ScanTasks(bg, TaskQuery{Conversation: ids[0]}, 10, nil)
			if err != nil || len(tasks.Items) != want {
				t.Fatalf("tasks: %+v %v", tasks, err)
			}
			submissions, err := s.ScanSubmissions(bg, SubmissionQuery{Conversation: ids[0]}, 10, nil)
			if err != nil || len(submissions.Items) != want {
				t.Fatalf("submissions: %+v %v", submissions, err)
			}
			if published {
				tasks.Items[0].Checkpoint["x"] = "mutated"
				submissions.Items[0].Value["x"] = "mutated"
				image, err := s.Snapshot(bg)
				if err != nil || image.Tasks[ids[1]].Checkpoint["x"] == "mutated" || image.Submissions[ids[2]].Value["x"] == "mutated" {
					t.Fatal("scan aliases publication image", err)
				}
			}
		}
		check(false)
		s.taskBookkeeping(func() {
			if err := s.adoptReadImage(s.core.state, 2); err != nil {
				t.Fatal(err)
			}
		})
		check(true)
	})
}

// A dynamically installed adapter remains authoritative. This intentionally does
// not establish wrapper publication projection: overlap stays disabled.
type mainPublicImageAdapter struct {
	Storage
	snapshots int
	entries   int
}

func (a *mainPublicImageAdapter) Snapshot(ctx context.Context) (Snapshot, error) {
	a.snapshots++
	image, err := a.Storage.Snapshot(ctx)
	if err == nil {
		image.Conversations[1] = Conversation{ID: 1, Name: "adapter"}
	}
	return image, err
}
func (a *mainPublicImageAdapter) ScanEntries(ctx context.Context, q EntryQuery, n int, c Cursor) (Page[Entry], error) {
	a.entries++
	return a.Storage.ScanEntries(ctx, q, n, c)
}
func TestMainSessionPublicImagePreservesInstalledAdapter(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		batches, _ := groupEntryBatches(t, b.store, 2)
		seedRootConversation(t, b.store)
		s, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		adapter := &mainPublicImageAdapter{Storage: b.store}
		s.taskBookkeeping(func() { s.store = adapter })
		result, err := s.core.commitBatches(bg, batches, nil, true)
		if err != nil || result.stored != 2 {
			t.Fatal(result, err)
		}
		image, err := s.Snapshot(bg)
		if err != nil || image.Seq != 3 || image.Conversations[1].Name != "adapter" || adapter.snapshots != 1 {
			t.Fatal(image, err, adapter.snapshots)
		}
		page, err := s.ScanEntries(bg, EntryQuery{Conversation: 1}, 10, nil)
		if err != nil || len(page.Items) != 2 || adapter.entries != 1 {
			t.Fatal(page, err, adapter.entries)
		}
	})
}
