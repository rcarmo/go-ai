package durable

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
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
		t.Run("global-and-visible-entry", func(t *testing.T) {
			for _, lookup := range [][]ID{{ids[0]}, {1, ids[0]}} {
				if record, found, err := s.Entry(bg, lookup...); err != nil || found || record.CommitSeq != 0 {
					t.Fatalf("unpublished lookup %v: %+v %v %v", lookup, record, found, err)
				}
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
		task := foundationTask(ids[1], nil)
		task.Kind, task.Conversation = "custom", ids[0]
		task.Execution.Native.State.Checkpoint = JSON{"x": 1}
		batch := Batch{Writes: []Write{
			{Op: "create-conversation", Conversation: &Conversation{ID: ids[0]}},
			{Op: "put-task", Task: &task},
			{Op: "put-submission", Submission: &Submission{ID: ids[2], Conversation: ids[0], RequestIDPresent: true, Type: "write", Status: "failed", Value: JSON{"errorCode": "host", "detail": JSON{"x": 1}}}},
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
			conversation, found, err := s.Conversation(bg, ids[0])
			if err != nil || found != published || (found && conversation.ID != ids[0]) {
				t.Fatalf("conversation lookup=%+v found=%v published=%v err=%v", conversation, found, published, err)
			}
			tasks, err := s.ScanTasks(bg, TaskQuery{Conversation: ids[0]}, 10, nil)
			if err != nil || len(tasks.Items) != want {
				t.Fatalf("tasks: %+v %v", tasks, err)
			}
			record, found, err := s.Task(bg, ids[1])
			if err != nil || found != published {
				t.Fatalf("canonical task lookup found=%v published=%v err=%v", found, published, err)
			}
			if found {
				if record.Version != 1 || record.Input == nil {
					t.Fatal("lookup leaked private envelope", record)
				}
				taskCheckpointMap(record.State.Checkpoint)["x"] = "lookup mutation"
			}
			sub, found, err := s.Submission(bg, ids[2])
			if err != nil || found != published {
				t.Fatal("submission publication", sub, found, err)
			}
			byRequest, found, err := s.SubmissionByRequest(bg, ids[0], "")
			if err != nil || found != published {
				t.Fatal("request publication", byRequest, found, err)
			}
			if published {
				if sub.Status != "unanswered" || sub.Detail == nil {
					t.Fatal("private submission envelope", sub, byRequest)
				}
				sub.Detail.Value.(map[string]any)["x"] = "lookup mutation"
				byRequest.Detail.Value.(map[string]any)["x"] = "request mutation"
			}
			submissions, err := s.ScanSubmissions(bg, SubmissionQuery{Conversation: ids[0]}, 10, nil)
			if err != nil || len(submissions.Items) != want {
				t.Fatalf("submissions: %+v %v", submissions, err)
			}
			if published {
				taskCheckpointMap(tasks.Items[0].State.Checkpoint)["x"] = "mutated"
				submissions.Items[0].Detail.Value.(map[string]any)["x"] = "mutated"
				image, err := s.Snapshot(bg)
				if err != nil || !equalJSONValue(taskCheckpointMap(image.Tasks[ids[1]].Execution.Native.State.Checkpoint), JSON{"x": 1}) || image.Submissions[ids[2]].Value["detail"].(map[string]any)["x"] == "mutated" {
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
	snapshots           int
	entries             int
	recordEntries       int
	recordConversations int
	recordError         error
	session             *Session
	lineOwned           bool
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
func (a *mainPublicImageAdapter) Conversation(ctx context.Context, id ID) (Conversation, bool, error) {
	a.recordConversations++
	a.lineOwned = a.session.line.held()
	if a.recordError != nil {
		return Conversation{}, false, a.recordError
	}
	record, found, err := a.Storage.Conversation(ctx, id)
	if found {
		record.ID = 77
	}
	return record, found, err
}
func (a *mainPublicImageAdapter) Entry(ctx context.Context, ids ...ID) (StoredEntry, bool, error) {
	a.recordEntries++
	a.lineOwned = a.session.line.held()
	if a.recordError != nil {
		return StoredEntry{}, false, a.recordError
	}
	record, found, err := a.Storage.Entry(ctx, ids...)
	if found {
		record.CommitSeq = 99
	}
	return record, found, err
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
		adapter := &mainPublicImageAdapter{Storage: b.store, session: s}
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
		conversation, found, err := s.Conversation(bg, 1)
		if err != nil || !found || conversation.ID != 77 || adapter.recordConversations != 1 || !adapter.lineOwned {
			t.Fatal(conversation, found, err)
		}
		for _, ids := range [][]ID{{page.Items[0].ID}, {1, page.Items[0].ID}} {
			record, found, err := s.Entry(bg, ids...)
			if err != nil || !found || record.CommitSeq != 99 || !adapter.lineOwned {
				t.Fatal(record, found, err)
			}
		}
		if adapter.recordEntries != 2 {
			t.Fatal(adapter.recordEntries)
		}
		adapter.recordError = storageRequest("adapter record request")
		if _, _, err := s.Conversation(bg, 1); err != adapter.recordError {
			t.Fatal("conversation request identity", err)
		}
		if _, _, err := s.Entry(bg, page.Items[0].ID); err != adapter.recordError {
			t.Fatal("entry request identity", err)
		}
		canceled, cancel := context.WithCancel(bg)
		cancel()
		before := adapter.recordEntries + adapter.recordConversations
		if _, _, err := s.Entry(canceled, page.Items[0].ID); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, _, err := s.Conversation(canceled, 1); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if adapter.recordEntries+adapter.recordConversations != before {
			t.Fatal("canceled read reached adapter")
		}
		adapter.recordError = errors.New("adapter record failure")
		if _, _, err := s.Entry(bg, page.Items[0].ID); err != adapter.recordError {
			t.Fatal("original backend failure", err)
		}
		before = adapter.recordConversations
		_, _, err = s.Conversation(canceled, 1)
		var failed *SessionFailed
		if !errors.As(err, &failed) || !errors.Is(err, adapter.recordError) || adapter.recordConversations != before {
			t.Fatal("first failure precedence", err)
		}
	})
}

// Source keys.ts:156-185: global entries touch only their record; visible
// entries additionally touch the conversation, but never its append key.
func TestSessionPrimaryRecordReadsUseExactKeys(t *testing.T) {
	for _, tc := range []struct {
		name, key string
		wait      bool
	}{
		{"conversation", conversationKey(1), true},
		{"global", entryKey(2), true},
		{"visible-conversation", conversationKey(1), true},
		{"visible-entry", entryKey(2), true},
		{"global-disjoint-conversation", conversationKey(1), false},
		{"conversation-disjoint-entry", entryKey(2), false},
		{"visible-disjoint-append", appendKey(1), false},
		{"submission", submissionKey(3), true},
		{"submission-disjoint-request", requestKey(1, ""), false},
		{"request", requestKey(1, ""), true},
		{"request-disjoint-record", submissionKey(3), false},
		{"request-disjoint-conversation", conversationKey(1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := gatedSession(t)
			if _, err := s.Commit(bg, func(tx *Tx) (any, error) {
				id, err := tx.mintID()
				if err != nil {
					return nil, err
				}
				if id != 2 {
					return nil, errors.New("unexpected test entry ID")
				}
				if err := tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: "note", HasData: true, Data: JSON{"value": "owned"}}); err != nil {
					return nil, err
				}
				submission, err := tx.mintID()
				if err != nil {
					return nil, err
				}
				if submission != 3 {
					return nil, errors.New("unexpected submission ID")
				}
				return nil, tx.putSubmissionRecord(Submission{ID: submission, Conversation: 1, RequestIDPresent: true, Type: "write", Status: "pending", Value: JSON{}})
			}); err != nil {
				t.Fatal(err)
			}
			release := holdingQueue(t, s, tc.key)
			writer := s.writer
			released := false
			defer func() {
				if !released {
					close(release)
				}
				if drained := writer.drained(); drained != nil {
					<-drained
				}
			}()
			parent, cancel := context.WithTimeout(bg, 5*time.Second)
			defer cancel()
			queued := make(chan struct{})
			ctx := &documentGateContext{Context: parent, queued: queued}
			result := make(chan error, 1)
			go func() {
				if strings.HasPrefix(tc.name, "submission") || strings.HasPrefix(tc.name, "request") {
					var record SubmissionRecord
					var found bool
					var err error
					if strings.HasPrefix(tc.name, "request") {
						record, found, err = s.SubmissionByRequest(ctx, 1, "")
					} else {
						record, found, err = s.Submission(ctx, 3)
					}
					if err == nil && (!found || record.ID != 3 || record.Status != "queued") {
						err = errors.New("submission missing or noncanonical")
					}
					result <- err
					return
				}
				if tc.name == "conversation" || tc.name == "conversation-disjoint-entry" {
					record, found, err := s.Conversation(ctx, 1)
					if err == nil && (!found || record.ID != 1) {
						err = errors.New("conversation missing")
					}
					result <- err
					return
				}
				ids := []ID{1, 2}
				if tc.name == "global" || tc.name == "global-disjoint-conversation" {
					ids = []ID{2}
				}
				stored, found, err := s.Entry(ctx, ids...)
				if err == nil && (!found || stored.Entry.ID != 2 || stored.CommitSeq != 2) {
					err = errors.New("entry missing or original commit sequence lost")
				}
				result <- err
			}()
			if tc.wait {
				awaitTaskSignal(t, queued)
				// Confirm an actual hit and a released memory line; a backend admission's
				// Done consultation alone cannot prove that a read waited at its key.
				if err := s.line.acquire(parent); err != nil {
					t.Fatal("read did not release memory across gate", err)
				}
				hit := s.currentJobHit
				s.line.release()
				if !hit {
					t.Error("record read did not hit its dependency gate")
				}
				select {
				case err := <-result:
					t.Fatalf("read escaped unpublished dependency: %v", err)
				default:
				}
				close(release)
				released = true
			}
			if err := <-result; err != nil {
				t.Fatal(err)
			}
		})
	}
}
