package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func sourcePublicCommitObservation(t *testing.T, s *Session, observer CommitObserver) *CommitObservation {
	t.Helper()
	guard, err := s.ObserveCommits(observer)
	if err != nil {
		t.Fatal(err)
	}
	return guard
}
func sourceDetachCommitObservation(t *testing.T, guard *CommitObservation) {
	t.Helper()
	guard.Detach()
}
func TestMainPublicCommitObserverDispatchMembershipAndCanonicalValue(t *testing.T) {
	s := gatedSession(t)
	def := mustDefinition(t, DefinitionOptions{Kind: "oracle.commit.public", Scope: "session", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"n": 0}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
	var order []string
	var seen []CommitPublication
	var late *CommitObservation
	first := sourcePublicCommitObservation(t, s, CommitObserver{Advance: func(context.Context, CommitPublication) error { order = append(order, "first"); return nil }, End: func(WatchEnd) error { return nil }})
	unfollow, err := s.followSessionCommits(&sessionCommitListener{callback: func(context.Context, sessionObservedCommit) error {
		order = append(order, "own")
		sourceDetachCommitObservation(t, first)
		if late == nil {
			late = sourcePublicCommitObservation(t, s, CommitObserver{Advance: func(_ context.Context, p CommitPublication) error {
				order = append(order, "late")
				seen = append(seen, p)
				return nil
			}, End: func(WatchEnd) error { order = append(order, "end"); return nil }})
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer unfollow()
	if _, err := s.Commit(bg, func(tx *Tx) error { _, err := tx.AcquireDocument(def, 0, nil, nil); return err }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"own", "late"}) || len(seen) != 1 {
		t.Fatal("dispatch order/membership", order, len(seen))
	}
	p := seen[0]
	if p.Changes.Len() != 1 {
		t.Fatal("changes", p.Changes.Len())
	}
	change, _ := p.Changes.Index(0)
	if immutableViewMember(t, change, "type").String() != "document" || immutableViewMember(t, change, "kind").String() != "change" {
		t.Fatal("noncanonical change")
	}
	record := immutableViewMember(t, change, "record")
	if record.Len() != 4 || immutableViewMember(t, record, "kind").String() != "oracle.commit.public" || immutableViewMember(t, record, "createdAt").Number() != json.Number(fmt.Sprint(p.Seq)) {
		t.Fatal("noncanonical metadata", record.Copy())
	}
	value, found, err := s.SnapshotDefinition(bg, def, 0, nil)
	if err != nil || !found || immutableViewMember(t, change, "value") != value {
		t.Fatal("publication lost adopted document identity", found, err)
	}
	sourceDetachCommitObservation(t, late)
	if err := s.Close(bg); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"own", "late"}) {
		t.Fatal("detached guard ended", order)
	}
}

// Source session-observers:389/414/427 lifecycle and guarded peer delivery.
func TestMainPublicCommitObserverLifecycleAndGuards(t *testing.T) {
	for _, mode := range []string{"close", "detach", "listener-error"} {
		t.Run(mode, func(t *testing.T) {
			s := gatedSession(t)
			var calls, peer int
			var ends []WatchEnd
			var reports []error
			marker := errors.New("commit observer bug")
			s.timerReport = func(err error) { reports = append(reports, err) }
			guard := sourcePublicCommitObservation(t, s, CommitObserver{Advance: func(context.Context, CommitPublication) error {
				calls++
				if mode == "listener-error" {
					return marker
				}
				return nil
			}, End: func(end WatchEnd) error { ends = append(ends, end); return nil }})
			healthy := sourcePublicCommitObservation(t, s, CommitObserver{Advance: func(context.Context, CommitPublication) error { peer++; return nil }, End: func(WatchEnd) error { return nil }})
			defer healthy.Detach()
			bump := func() {
				_, err := s.Commit(bg, func(tx *Tx) error {
					id, err := tx.MintID()
					if err != nil {
						return err
					}
					return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{}})
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			bump()
			if mode == "detach" {
				guard.Detach()
				guard.Detach()
			}
			bump()
			if peer != 2 {
				t.Fatal("healthy peer skipped", peer)
			}
			if err := s.Close(bg); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "close":
				if calls != 2 || len(ends) != 1 || ends[0].Reason != "failed" || ends[0].Error == nil || ends[0].Error.Error() != "session closed" {
					t.Fatal(calls, ends)
				}
			case "detach":
				if calls != 1 || len(ends) != 0 {
					t.Fatal(calls, ends)
				}
			case "listener-error":
				if calls != 1 || len(ends) != 1 || ends[0].Reason != "listener_error" || ends[0].Error != marker || len(reports) != 1 || reports[0] != marker {
					t.Fatal(calls, ends, reports)
				}
			}
		})
	}
}
func TestMainPublicCommitObserverStorageScopeDispatchMembership(t *testing.T) {
	for _, interested := range []bool{false, true} {
		t.Run(fmt.Sprint(interested), func(t *testing.T) {
			s := gatedSession(t)
			var order []string
			var first, late *CommitObservation
			if interested {
				first = sourcePublicCommitObservation(t, s, CommitObserver{Advance: func(context.Context, CommitPublication) error { order = append(order, "first"); return nil }, End: func(WatchEnd) error { return nil }})
			}
			s.store = &v150PublicationScopeStorage{Storage: s.store, core: s.core, scope: func(_ context.Context, publish func()) {
				order = append(order, "scope-open")
				publish()
				order = append(order, "scope-close")
				if first != nil {
					first.Detach()
				}
				late = sourcePublicCommitObservation(t, s, CommitObserver{Advance: func(context.Context, CommitPublication) error { order = append(order, "late"); return nil }, End: func(WatchEnd) error { return nil }})
			}}
			_, err := s.Commit(bg, func(tx *Tx) error {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{}})
			})
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"scope-open", "scope-close"}
			if interested {
				want = append(want, "late")
			}
			if !reflect.DeepEqual(order, want) {
				t.Fatal(order, want)
			}
			late.Detach()
		})
	}
}

type endedPublicationScopeStorage struct {
	Storage
	core  *storeCore
	scope func(context.Context, func()) <-chan struct{}
}

func (w *endedPublicationScopeStorage) nativeCore() *storeCore { return w.core }
func (w *endedPublicationScopeStorage) Publishing(ctx context.Context, publish func()) <-chan struct{} {
	return w.scope(ctx, publish)
}

// A completion channel represents the end of a Go async host's synchronous
// scope turn, an intrinsic microtask boundary in the reference JS runtime.
func TestMainPublicCommitObserverDeferredStorageTurn(t *testing.T) {
	s := gatedSession(t)
	var order []string
	var late *CommitObservation
	first := sourcePublicCommitObservation(t, s, CommitObserver{Advance: func(context.Context, CommitPublication) error { order = append(order, "first"); return nil }, End: func(WatchEnd) error { return nil }})
	entered := make(chan func(), 1)
	finished := make(chan struct{})
	published := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	s.store = &endedPublicationScopeStorage{Storage: s.store, core: s.core, scope: func(_ context.Context, publish func()) <-chan struct{} {
		entered <- func() {
			order = append(order, "open")
			publish()
			close(published)
			<-release
			order = append(order, "close")
			first.Detach()
			late = sourcePublicCommitObservation(t, s, CommitObserver{Advance: func(context.Context, CommitPublication) error { order = append(order, "late"); return nil }, End: func(WatchEnd) error { return nil }})
			close(finished)
		}
		return finished
	}}
	result := make(chan error, 1)
	go func() {
		_, err := s.Commit(bg, func(tx *Tx) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{}})
		})
		result <- err
	}()
	select {
	case run := <-entered:
		go run()
	case err := <-result:
		t.Fatal("deferred scope not used", err)
	}
	watchReceive(t, published)
	select {
	case err := <-result:
		t.Error("outcome escaped unfinished host turn", err)
	default:
	}
	unblock()
	if err := watchReceive(t, result); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"open", "close", "late"}) {
		t.Fatal("deferred scope membership/order", order)
	}
	late.Detach()
}

func TestMainPublicCommitObserverCanonicalTableRecords(t *testing.T) {
	s := gatedSession(t)
	var publications []CommitPublication
	guard := sourcePublicCommitObservation(t, s, CommitObserver{Advance: func(_ context.Context, p CommitPublication) error { publications = append(publications, p); return nil }, End: func(end WatchEnd) error { t.Error("unexpected public projection end", end); return nil }})
	defer guard.Detach()
	phase := func(context.Context, TaskRecord, *TaskRuntime) error { return nil }
	work, err := DefineTask(TaskDefinitionOptions{Kind: "oracle.public.task", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "run"}, nil }, Phases: map[string]TaskPhase{"run": phase}, Abort: phase})
	if err != nil {
		t.Fatal(err)
	}
	var conversation, entry, task, submission ID
	_, err = s.Commit(bg, func(tx *Tx) error {
		var err error
		conversation, err = tx.MintID()
		if err != nil {
			return err
		}
		if err = tx.CreateConversation(Conversation{ID: conversation}); err != nil {
			return err
		}
		entry, err = tx.MintID()
		if err != nil {
			return err
		}
		if err = tx.AppendEntry(Entry{ID: entry, Conversation: conversation, Kind: "note", Data: JSON{"n": 1}, HasData: true, Value: JSON{}}); err != nil {
			return err
		}
		task, err = tx.CreateTask(work, nil, TaskOptions{Conversation: conversation, Ownership: TaskOwnership{Kind: "conversation"}})
		if err != nil {
			return err
		}
		createdSubmission, err := tx.CreateSubmission(SubmissionCreate{Conversation: conversation, Type: "write", Status: "queued", RequestIDPresent: true})
		submission = createdSubmission.ID
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(publications) != 1 {
		t.Fatal("stored table publications", len(publications))
	}
	changes := publications[0].Changes
	if changes.Len() != 4 {
		t.Fatal("table changes", changes.Copy())
	}
	byType := map[string]any{}
	for i := range changes.Len() {
		change, _ := changes.Index(i)
		if change.Len() != 2 {
			t.Fatal("noncanonical table envelope", change.Copy())
		}
		byType[immutableViewMember(t, change, "type").String()] = immutableViewMember(t, change, "value").Copy()
	}
	expected := JSON{"conversation": JSON{"id": conversation}, "entry": JSON{"id": entry, "conversationId": conversation, "kind": "note", "data": JSON{"n": 1}}, "task": JSON{"id": task, "conversationId": conversation, "kind": "oracle.public.task", "version": 1, "input": nil, "background": false, "abortRequested": false, "state": JSON{"status": "pending", "checkpoint": JSON{"phase": "run"}}}, "submission": JSON{"id": submission, "conversationId": conversation, "type": "write", "status": "queued", "requestId": ""}}
	// expected IDs are typed Go IDs; normalise only this fixture's JSON scalars.
	owned, err := ownJSONValue(expected, s.limits)
	if err != nil {
		t.Fatal(err)
	}
	if !equalDeltaJSON(byType, owned) {
		t.Fatal("canonical table records", byType, owned)
	}
}

func TestMainPublicCommitObserverCopyAndRetirement(t *testing.T) {
	s := gatedSession(t)
	def := mustDefinition(t, DefinitionOptions{Kind: "oracle.public.copy", Scope: "conversation", History: "rewindable", Fork: "asOf", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"n": 0}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
	var entry ID
	_, err := s.Commit(bg, func(tx *Tx) error {
		_, err := tx.AcquireDocument(def, 1, nil, nil)
		if err != nil {
			return err
		}
		entry, err = tx.MintID()
		if err != nil {
			return err
		}
		return tx.AppendEntry(Entry{ID: entry, Conversation: 1, Kind: "note", Value: JSON{}})
	})
	if err != nil {
		t.Fatal(err)
	}
	var seen []CommitPublication
	guard := sourcePublicCommitObservation(t, s, CommitObserver{Advance: func(_ context.Context, p CommitPublication) error { seen = append(seen, p); return nil }, End: func(end WatchEnd) error { t.Error("copy projection failed", end); return nil }})
	defer guard.Detach()
	var child Conversation
	_, err = s.Commit(bg, func(tx *Tx) error { var err error; child, err = tx.ForkConversation(1, entry, 0); return err })
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 {
		t.Fatal("copy publication", len(seen))
	}
	var copied ReadonlyValue
	for i := range seen[0].Changes.Len() {
		change, _ := seen[0].Changes.Index(i)
		if immutableViewMember(t, change, "type").String() == "document.copy" {
			copied = change
		}
	}
	if copied == nil || copied.Len() != 4 || immutableViewMember(t, copied, "conversationId").Number() != json.Number(fmt.Sprint(child.ID)) {
		t.Fatal("noncanonical copy", copied)
	}
	if _, exists := copied.Get("value"); exists {
		t.Fatal("copy exposed materialised value")
	}
	if point := immutableViewMember(t, immutableViewMember(t, copied, "source"), "at"); point.Kind() != "number" || point.Number() != json.Number(fmt.Sprint(seen[0].Seq-1)) {
		t.Fatal("copy point is not the canonical sequence scalar", point.Copy())
	}
	_, err = s.Commit(bg, func(tx *Tx) error { return tx.RetireDefinition(def, 1, nil) })
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatal("retirement publication", len(seen))
	}
	retired, _ := seen[1].Changes.Index(0)
	if retired.Len() != 4 || immutableViewMember(t, retired, "kind").String() != "retire" || immutableViewMember(t, immutableViewMember(t, retired, "record"), "retiredAt").Number() != json.Number(fmt.Sprint(seen[1].Seq)) {
		t.Fatal("retirement metadata", retired.Copy())
	}
	guard.Detach()
}

func TestMainPublicCommitObserverHeldStoresAndClose(t *testing.T) {
	for _, finish := range []string{"read", "unload", "close"} {
		t.Run(finish, func(t *testing.T) {
			s := gatedSession(t)
			def := mustDefinition(t, DefinitionOptions{Kind: "oracle.public.held", Scope: "conversation", History: "latest", Fork: "initial", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"n": 0}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
			if _, err := s.Commit(bg, func(tx *Tx) error { _, err := tx.AcquireDocument(def, 1, nil, nil); return err }); err != nil {
				t.Fatal(err)
			}
			var seen []CommitPublication
			var order []string
			guard := sourcePublicCommitObservation(t, s, CommitObserver{Advance: func(_ context.Context, p CommitPublication) error {
				seen = append(seen, p)
				order = append(order, "stored")
				return nil
			}, End: func(WatchEnd) error { order = append(order, "end"); return nil }})
			defer guard.Detach()
			if _, err := s.commitPersistence(bg, func(tx *Tx) error {
				doc, err := tx.AcquireDocument(def, 1, nil, nil)
				if err != nil {
					return err
				}
				return doc.ApplyOperations([]Operation{{"s", []any{"n"}, 1}})
			}, true, 0, true, math.Inf(1)); err != nil {
				t.Fatal(err)
			}
			value, found, err := s.SnapshotDefinition(bg, def, 1, nil)
			if err != nil || !found || len(seen) != 0 {
				t.Fatal("memory commit escaped stored lane", found, err, len(seen))
			}
			switch finish {
			case "read":
				_, err = s.Commit(bg, func(tx *Tx) error {
					_, err := tx.AcquireDocument(def, 1, nil, nil)
					if err != nil {
						return err
					}
					id, err := tx.MintID()
					if err != nil {
						return err
					}
					return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{}})
				})
			case "unload":
				err = s.UnloadDocuments(bg)
			case "close":
				err = s.Close(bg)
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(seen) != 1 {
				t.Fatal("stored count", len(seen))
			}
			var change ReadonlyValue
			for i := range seen[0].Changes.Len() {
				candidate, _ := seen[0].Changes.Index(i)
				if immutableViewMember(t, candidate, "type").String() == "document" {
					change = candidate
				}
			}
			if change == nil {
				t.Fatal("stored document change missing")
			}
			if immutableViewMember(t, change, "value") != value {
				t.Fatal("held storage manufactured a new revision")
			}
			ops := immutableViewMember(t, change, "ops")
			want, err := ownJSONValue([]Operation{{"s", []any{"n"}, 1}}, s.limits)
			if err != nil || !equalDeltaJSON(ops.Copy(), want) {
				t.Fatal("held operations", ops.Copy(), err)
			}
			expected := []string{"stored"}
			if finish == "close" {
				expected = append(expected, "end")
			}
			if !reflect.DeepEqual(order, expected) {
				t.Fatal("stored/close order", order)
			}
			guard.Detach()
		})
	}
}
func TestMainPublicCommitObserverFirstFailureCause(t *testing.T) {
	s := gatedSession(t)
	marker := errors.New("disk gone")
	var ends []WatchEnd
	sourcePublicCommitObservation(t, s, CommitObserver{Advance: func(context.Context, CommitPublication) error { t.Error("uncommitted failure published"); return nil }, End: func(end WatchEnd) error { ends = append(ends, end); return nil }})
	s.writer.host.commit = func(context.Context, []Batch, func(commitReceipt)) (commitGroupResult, error) {
		return commitGroupResult{}, marker
	}
	_, err := s.Commit(bg, func(tx *Tx) error {
		id, err := tx.MintID()
		if err != nil {
			return err
		}
		return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{}})
	})
	if err != marker {
		t.Fatal("own storage failure", err)
	}
	if _, err := s.Closed(bg); err != nil {
		t.Fatal(err)
	}
	if len(ends) != 1 || ends[0].Reason != "failed" || ends[0].Error != marker {
		t.Fatal("close lost failure cause", ends)
	}
}

func TestMainPublicCommitObserverBeforeTaskWaiter(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	defer releaseTaskGate(release)
	work := taskDefinition(t, "oracle.public.wait", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		close(entered)
		<-release
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	h := taskTestHarness(t, store, work)
	id := createPublicTask(t, h, work, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	var delivered, stored atomic.Bool
	guard, err := h.ObserveCommits(CommitObserver{Advance: func(_ context.Context, p CommitPublication) error {
		for i := range p.Changes.Len() {
			change, _ := p.Changes.Index(i)
			if immutableViewMember(t, change, "type").String() != "task" {
				continue
			}
			record := immutableViewMember(t, change, "value")
			if immutableViewMember(t, record, "id").Number() != json.Number(fmt.Sprint(id)) {
				continue
			}
			if immutableViewMember(t, immutableViewMember(t, record, "state"), "status").String() == "terminal" {
				if delivered.Load() {
					t.Error("waiter continuation overtook stored dispatch")
				}
				stored.Store(true)
			}
		}
		return nil
	}, End: func(WatchEnd) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Detach()
	result := make(chan error, 1)
	go func() { _, err := h.WaitForTask(bg, id); delivered.Store(true); result <- err }()
	watchReceive(t, entered)
	close(release)
	if err := watchReceive(t, result); err != nil || !stored.Load() {
		t.Fatal("missing stored terminal before waiter", err)
	}
}

func TestMainPublicCommitObserverBeforeAbortSignal(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	entered, stopped := make(chan struct{}), make(chan struct{})
	var requested atomic.Bool
	work := taskDefinition(t, "oracle.public.abort", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
		close(entered)
		<-ctx.Done()
		if !requested.Load() {
			t.Error("invocation abort signal overtook stored abort publication")
		}
		close(stopped)
		return nil
	})
	h := taskTestHarness(t, store, work)
	id := createPublicTask(t, h, work, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	guard, err := h.ObserveCommits(CommitObserver{Advance: func(_ context.Context, p CommitPublication) error {
		for i := range p.Changes.Len() {
			change, _ := p.Changes.Index(i)
			if immutableViewMember(t, change, "type").String() != "task" {
				continue
			}
			record := immutableViewMember(t, change, "value")
			if immutableViewMember(t, record, "id").Number() == json.Number(fmt.Sprint(id)) && immutableViewMember(t, record, "abortRequested").Bool() {
				requested.Store(true)
			}
		}
		return nil
	}, End: func(WatchEnd) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Detach()
	if err := h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	watchReceive(t, entered)
	if _, err := h.AbortTask(bg, id); err != nil {
		t.Fatal(err)
	}
	watchReceive(t, stopped)
	if !requested.Load() {
		t.Fatal("stored abort was not observed")
	}
}

func TestMainPublicCommitObserverArbitraryPanicValue(t *testing.T) {
	for _, value := range []any{false, 0, "", nil} {
		t.Run(fmt.Sprintf("%T:%v", value, value), func(t *testing.T) {
			s := gatedSession(t)
			var reports []error
			var ends []WatchEnd
			called := 0
			s.timerReport = func(err error) { reports = append(reports, err) }
			guard, err := s.ObserveCommits(CommitObserver{Advance: func(context.Context, CommitPublication) error {
				called++
				panic(value)
			}, End: func(end WatchEnd) error { ends = append(ends, end); return nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Detach()
			for range 2 {
				if _, err := s.Commit(bg, func(tx *Tx) error {
					id, err := tx.MintID()
					if err != nil {
						return err
					}
					return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{}})
				}); err != nil {
					t.Fatal(err)
				}
			}
			if called != 1 || len(reports) != 1 || len(ends) != 1 || ends[0].Reason != "listener_error" {
				t.Fatal(called, reports, ends)
			}
			var failure *Failure
			if !errors.As(reports[0], &failure) || !reflect.DeepEqual(failure.Value(), value) {
				t.Fatal("report replaced original throw value", reports[0], value)
			}
			expected := fmt.Sprint(value)
			if value == nil {
				expected = "null"
			}
			if ends[0].Error == nil || ends[0].Error.Error() != expected {
				t.Fatal("listener ending did not normalise as source toError", ends[0].Error, expected)
			}

		})
	}
}

func TestMainPublicCommitObserverEndPanicValue(t *testing.T) {
	for _, value := range []any{false, 0, "", nil} {
		t.Run(fmt.Sprintf("%T:%v", value, value), func(t *testing.T) {
			s := gatedSession(t)
			var reports []error
			var ends []WatchEnd
			s.timerReport = func(err error) { reports = append(reports, err) }
			_, err := s.ObserveCommits(CommitObserver{Advance: func(context.Context, CommitPublication) error { return nil }, End: func(end WatchEnd) error {
				ends = append(ends, end)
				panic(value)
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Close(bg); err != nil {
				t.Fatal(err)
			}
			if len(ends) != 1 || ends[0].Reason != "failed" || ends[0].Error == nil || ends[0].Error.Error() != "session closed" || len(reports) != 1 {
				t.Fatal(ends, reports)
			}
			var failure *Failure
			if !errors.As(reports[0], &failure) || !reflect.DeepEqual(failure.Value(), value) {
				t.Fatal("ending report replaced original throw value", reports[0], value)
			}
		})
	}
}

// Source guardObserver releases once after reporting; notifyHosts snapshots its
// registry, then rechecks each guard. Observer attachment/detachment is reentrant.
func TestMainPublicCommitObserverReentrantDetachAndDispatchSnapshot(t *testing.T) {
	for _, mode := range []string{"self-detach", "report-detach", "replace-peer"} {
		t.Run(mode, func(t *testing.T) {
			s := gatedSession(t)
			var calls, ends []string
			var reports []error
			bug := errors.New("observer fault")
			var first, peer, late *CommitObservation
			s.timerReport = func(err error) {
				reports = append(reports, err)
				if mode == "report-detach" {
					first.Detach()
				}
			}
			first = sourcePublicCommitObservation(t, s, CommitObserver{Advance: func(context.Context, CommitPublication) error {
				calls = append(calls, "first")
				if mode == "replace-peer" {
					if late == nil {
						peer.Detach()
						late = sourcePublicCommitObservation(t, s, CommitObserver{Advance: func(context.Context, CommitPublication) error { calls = append(calls, "late"); return nil }, End: func(WatchEnd) error { ends = append(ends, "late"); return nil }})
					}
					return nil
				}
				if mode == "self-detach" {
					first.Detach()
				}
				return bug
			}, End: func(WatchEnd) error { ends = append(ends, "first"); return nil }})
			peer = sourcePublicCommitObservation(t, s, CommitObserver{Advance: func(context.Context, CommitPublication) error { calls = append(calls, "peer"); return nil }, End: func(WatchEnd) error { ends = append(ends, "peer"); return nil }})
			for range 2 {
				if _, err := s.Commit(bg, func(tx *Tx) error {
					id, err := tx.MintID()
					if err != nil {
						return err
					}
					return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{}})
				}); err != nil {
					t.Fatal(err)
				}
			}
			wantCalls := []string{"first", "peer", "peer"}
			wantEnds := []string{"peer"}
			if mode == "replace-peer" {
				wantCalls = []string{"first", "first", "late"}
				wantEnds = []string{"first", "late"}
				if len(reports) != 0 {
					t.Fatal(reports)
				}
			} else if len(reports) != 1 || reports[0] != bug {
				t.Fatal(reports)
			}
			if !reflect.DeepEqual(calls, wantCalls) || len(ends) != 0 {
				t.Fatal(calls, ends)
			}
			if err := s.Close(bg); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(ends, wantEnds) {
				t.Fatal(ends, wantEnds)
			}
		})
	}
}

// A Go Stringer is the host-side counterpart of a source object with toString.
// The throwable itself must remain the report value even if conversion fails.
type publicObserverThrowStringer struct {
	text  string
	fail  bool
	calls int
}

func (s *publicObserverThrowStringer) String() string {
	s.calls++
	if s.fail {
		panic(nil)
	}
	return s.text
}

func TestMainPublicCommitObserverObjectAndNumberPanicValue(t *testing.T) {
	cycle := make([]any, 1)
	cycle[0] = cycle
	a, b := make([]any, 2), make([]any, 2)
	a[0], a[1] = b, 1
	b[0], b[1] = a, 2
	for _, tc := range []struct {
		name    string
		value   any
		message string
	}{
		{"object", JSON{"n": 1}, "[object Object]"}, {"empty-array", []any{}, ""}, {"array", []any{nil, false, JSON{"n": 1}, []any{1, 2}}, ",false,[object Object],1,2"},
		{"negative-zero", math.Copysign(0, -1), "0"}, {"small", 1e-7, "1e-7"}, {"large", 1e20, "100000000000000000000"}, {"exponent", 1e21, "1e+21"}, {"nan", math.NaN(), "NaN"}, {"infinity", math.Inf(1), "Infinity"}, {"negative-infinity", math.Inf(-1), "-Infinity"},
		{"cycle", cycle, ""}, {"indirect-cycle", a, ",2,1"}, {"custom", &publicObserverThrowStringer{text: "custom"}, "custom"}, {"failed-conversion", &publicObserverThrowStringer{fail: true}, "[object Object]"}, {"array-failed-conversion", []any{&publicObserverThrowStringer{fail: true}}, "[object Array]"},
		{"json-number", json.Number("1e20"), "100000000000000000000"},
		{"shared-array", []any{cycle[:0], []any{1, 2}, []any{1, 2}}, ",1,2,1,2"},
		{"bigint", new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil), "1000000000000000000000000000000"},
		{"error-in-array", []any{errors.New("nested error")}, "Error: nested error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := gatedSession(t)
			var reports []error
			var ends []WatchEnd
			s.timerReport = func(err error) { reports = append(reports, err) }
			guard := sourcePublicCommitObservation(t, s, CommitObserver{Advance: func(context.Context, CommitPublication) error { panic(tc.value) }, End: func(end WatchEnd) error { ends = append(ends, end); return nil }})
			defer guard.Detach()
			if _, err := s.Commit(bg, func(tx *Tx) error {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{}})
			}); err != nil {
				t.Fatal(err)
			}
			if len(reports) != 1 || len(ends) != 1 || ends[0].Reason != "listener_error" {
				t.Fatal(len(reports), len(ends))
			}
			var failure *Failure
			if !errors.As(reports[0], &failure) {
				t.Fatal("report lost the original value carrier")
			}
			original, reported := reflect.ValueOf(tc.value), reflect.ValueOf(failure.Value())
			switch original.Kind() {
			case reflect.Map, reflect.Slice, reflect.Pointer:
				if original.Type() != reported.Type() || original.UnsafePointer() != reported.UnsafePointer() {
					t.Fatal("report lost throwable identity")
				}
			default:
				if original.Kind() == reflect.Float64 && math.IsNaN(original.Float()) {
					if reported.Kind() != reflect.Float64 || !math.IsNaN(reported.Float()) {
						t.Fatal("report lost NaN")
					}
				} else if !reflect.DeepEqual(tc.value, failure.Value()) {
					t.Fatal("report replaced original scalar")
				}
			}
			if original.Kind() == reflect.Float64 && original.Float() == 0 && math.Signbit(original.Float()) != math.Signbit(reported.Float()) {
				t.Fatal("report replaced signed zero")
			}
			var endedFailure *Failure
			if errors.As(ends[0].Error, &endedFailure) {
				t.Fatal("ending retained the throw carrier instead of a normalized Error")
			}
			if custom, ok := tc.value.(*publicObserverThrowStringer); ok && custom.calls != 1 {
				t.Fatal("stringification call count", custom.calls)
			}
			if ends[0].Error == nil || ends[0].Error.Error() != tc.message {
				t.Fatalf("listener_error message %q, want %q", ends[0].Error, tc.message)
			}
			if err := s.Close(bg); err != nil {
				t.Fatal(err)
			}
			if len(ends) != 1 {
				t.Fatal("faulted guard ended again")
			}
		})
	}
}

func TestMainPublicCommitObserverCanonicalCompactionKind(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			t.Error("empty compaction called a model")
			ch := make(chan goai.Event)
			close(ch)
			return ch
		})
		h := openHarness(t, b.store, options)
		c := root(t, h, ref)
		var records []ReadonlyValue
		var ends []WatchEnd
		guard, err := h.ObserveCommits(CommitObserver{Advance: func(_ context.Context, p CommitPublication) error {
			for i := range p.Changes.Len() {
				change, _ := p.Changes.Index(i)
				if immutableViewMember(t, change, "type").String() == "task" {
					records = append(records, immutableViewMember(t, change, "value"))
				}
			}
			return nil
		}, End: func(end WatchEnd) error { ends = append(ends, end); return nil }})
		if err != nil {
			t.Fatal(err)
		}
		defer guard.Detach()
		id, err := c.Compact(bg, CompactionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		waitPublicTask(t, h, id)
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		var first ReadonlyValue
		for _, record := range records {
			if immutableViewMember(t, record, "id").Number() == json.Number(fmt.Sprint(id)) {
				first = record
				break
			}
		}
		if first == nil {
			t.Fatal("compaction had no public task publication")
		}
		if got := immutableViewMember(t, first, "kind").String(); got != "pi.compaction" {
			t.Fatal("noncanonical compaction task kind", got)
		}
		if immutableViewMember(t, first, "version").Number() != json.Number("1") || immutableViewMember(t, immutableViewMember(t, first, "input"), "reason").String() != "manual" {
			t.Fatal(first.Copy())
		}
		state := immutableViewMember(t, first, "state")
		if !equalJSONValue(state.Copy(), JSON{"status": "pending", "checkpoint": JSON{"phase": "select"}}) {
			t.Fatal(state.Copy())
		}
		if len(ends) != 1 || ends[0].Reason != "failed" {
			t.Fatal(ends)
		}
	})
}

func TestMainTaskApplicationPiNamespace(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		work := taskDefinition(t, "pi.example", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("done"), nil })
		})
		h := taskTestHarness(t, b.store, work)
		id := createPublicTask(t, h, work, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		record := waitPublicTask(t, h, id)
		if record.Kind != "pi.example" || record.State.Outcome.Status != "completed" || record.State.Outcome.Result.Value != "done" {
			t.Fatal(record)
		}
	})
}
