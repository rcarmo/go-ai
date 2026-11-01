package durable

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type transcriptDelivery struct {
	value ConversationView
	ops   []Operation
}

// Source view.ts advances each window once per publication, before Watch
// delivery. Exercise that invariant across the native overflow boundary, using
// the same append/trim rule as harness-view.test.ts rather than rehydrating.
func TestMainTranscriptPublicationWindowPendingFold(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(fmt.Sprintf("blocked=%t", blocked), func(t *testing.T) {
			utf16Backends(t, func(t *testing.T, b backend) {
				h, c := transcriptFixture(t, b)
				future := ID(MaxID)
				window := transcriptBounds(3, 6)
				window.From = &future
				watch, err := c.Watch(bg, window)
				if err != nil {
					t.Fatal(err)
				}
				defer watch.Stop()
				baseline, err := watch.Value()
				if err != nil {
					t.Fatal(err)
				}
				replay, err := dtoObject(baseline, h.session.limits)
				if err != nil {
					t.Fatal(err)
				}
				frames := make(chan transcriptDelivery, 128)
				entered := make(chan struct{})
				release := make(chan struct{})
				defer func() {
					select {
					case <-release:
					default:
						close(release)
					}
				}()
				first := true
				listen := func(_ context.Context, v ConversationView, ops []Operation) error {
					if blocked && first {
						first = false
						close(entered)
						<-release
					}
					frames <- transcriptDelivery{v, ops}
					return nil
				}
				if blocked {
					if err = watch.Start(listen); err != nil {
						t.Fatal(err)
					}
				}
				expected := map[ID]ConversationView{}
				ids := []ID{}
				var before ID
				for index := 0; index < 105; index++ {
					id := transcriptNotes(t, h, 1)[0]
					ids = append(ids, id)
					if len(ids) > 6 {
						drop := len(ids) - 3
						before = ids[drop-1]
						ids = append([]ID{}, ids[drop:]...)
					}
					want := ConversationView{Before: before, Entries: make([]Entry, len(ids))}
					for j, id := range ids {
						want.Entries[j].ID = id
					}
					expected[id] = want
					if blocked && index == 0 {
						select {
						case <-entered:
						case <-time.After(3 * time.Second):
							t.Fatal("first callback never entered")
						}
					}
				}
				last := ids[len(ids)-1]
				if blocked {
					close(release)
				} else if err = watch.Start(listen); err != nil {
					t.Fatal(err)
				}
				root, calls := false, 0
				for {
					var frame transcriptDelivery
					select {
					case frame = <-frames:
					case <-time.After(3 * time.Second):
						end, _ := watch.End()
						t.Fatal("missing window delivery", end)
					}
					calls++
					v := frame.value
					if len(v.Entries) == 0 {
						t.Fatal("future from was reapplied during overflow")
					}
					tail := v.Entries[len(v.Entries)-1].ID
					want, ok := expected[tail]
					if !ok {
						t.Fatal("unexpected last entry", tail)
					}
					wanted := make([]ID, len(want.Entries))
					for i, e := range want.Entries {
						wanted[i] = e.ID
					}
					transcriptIDs(t, v, wanted, want.Before)
					if len(frame.ops) > 0 && frame.ops[0][0] == "r" {
						root = true
					}
					next, err := ApplyOperations(replay, frame.ops, h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					wire, err := dtoObject(v, h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					if !equalJSONValue(next, wire) {
						t.Fatal("overflow operations lost publication-time window")
					}
					replay = wire
					if tail == last {
						wantCalls := 1
						if blocked {
							wantCalls++
						}
						if root || calls != wantCalls {
							t.Fatal("pending transcript did not fold without a count reset", root, calls)
						}
						break
					}
				}
			})
		})
	}
}

func TestMainTranscriptPreparationDoesNotPublishWindow(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		h, c := transcriptFixture(t, b)
		ids := transcriptNotes(t, h, 2)
		watch, err := c.Watch(bg, transcriptBounds(3, 6))
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		frames := transcriptListener(t, watch, h.session.limits)
		abandoned, err := h.session.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		ready := make(chan struct{})
		defer close(ready)
		h.session.taskBookkeeping(func() {
			_ = mainPrepareOnlyPublication(t, h.session, bg, abandoned, ready)
		})
		// No storage settlement/adoption follows the prepared, sealed candidate.
		current, err := watch.Value()
		if err != nil {
			t.Fatal(err)
		}
		transcriptIDs(t, current, ids, 0)
		ids = append(ids, transcriptNotes(t, h, 1)...)
		select {
		case value := <-frames:
			transcriptIDs(t, value, ids, 0)
		case <-watch.Closed():
			end, _ := watch.End()
			t.Fatal("abandoned preparation corrupted replay", end)
		case <-time.After(3 * time.Second):
			t.Fatal("missing post-preparation frame")
		}
	})
}

func TestMainTranscriptProjectionFailureIsLocalAndOffLine(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		reports := make(chan error, 4)
		var h *Harness
		h = openHarness(t, b.store, Options{OnReport: func(err error) {
			// Reporting is allowed to re-enter; the failed projection never poisons
			// storage or calls this hook while owning the Session mutation line.
			if _, readErr := h.Snapshot(bg); readErr != nil {
				reports <- readErr
				return
			}
			reports <- err
		}})
		c, err := h.Conversation(bg, 1)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = h.session.Commit(bg, func(tx *Tx) error { return initializeBuiltins(tx, 1) }); err != nil {
			t.Fatal(err)
		}
		bad, err := c.Watch(bg, transcriptBounds(1, 20))
		if err != nil {
			t.Fatal(err)
		}
		defer bad.Stop()
		good, err := c.Watch(bg, transcriptBounds(1, 1))
		if err != nil {
			t.Fatal(err)
		}
		defer good.Stop()
		goodFrames := transcriptListener(t, good, h.session.limits)
		var last ID
		if _, err = h.session.Commit(bg, func(tx *Tx) error {
			for i := 0; i < 9; i++ {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				if err = tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "app.note", Value: JSON{"blob": strings.Repeat("x", 64<<10)}}); err != nil {
					return err
				}
				last = id
			}
			return nil
		}); err != nil {
			t.Fatal("watch projection rejected durable commit", err)
		}
		value := transcriptFrame(t, goodFrames)
		if len(value.Entries) != 1 || value.Entries[0].ID != last {
			t.Fatal("healthy window lost commit", value)
		}
		// A projection error exists before Start but remains a listener diagnostic;
		// no user callback or reporter runs during preparation/publication.
		select {
		case err := <-reports:
			t.Fatal("projection reported before Start", err)
		default:
		}
		if err = bad.Start(func(context.Context, ConversationView, []Operation) error {
			t.Error("invalid projection reached callback")
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		select {
		case <-bad.Closed():
		case <-time.After(3 * time.Second):
			t.Fatal("projection error did not end watch")
		}
		end, _ := bad.End()
		if end.Reason != "listener_error" || end.Error == nil {
			t.Fatal("wrong projection failure", end)
		}
		select {
		case err := <-reports:
			if err.Error() != end.Error.Error() {
				t.Fatal("wrong report", err, end)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("missing reentrant listener report")
		}
		if h.session.failure() != nil {
			t.Fatal("local projection failed Session", h.session.failure())
		}
		added := transcriptNotes(t, h, 1)
		value = transcriptFrame(t, goodFrames)
		if len(value.Entries) != 1 || value.Entries[0].ID != added[0] {
			t.Fatal("healthy watch stopped after peer error", value)
		}
		state, err := h.Snapshot(bg)
		if err != nil || len(state.Entries) != 10 {
			t.Fatal("stored entries lost", state, err)
		}
	})
}

func TestMainTranscriptProjectionRetainsByteQuota(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		h, c := transcriptFixture(t, b)
		watch, err := c.Watch(bg, transcriptBounds(1, 3))
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		ids := []ID{}
		// Each DTO is legal. Three owned values plus their operations cannot all
		// fit this fixed share; replace the backlog without rejecting any commit.
		for i := 0; i < 3; i++ {
			if _, err = h.session.Commit(bg, func(tx *Tx) error {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				ids = append(ids, id)
				return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "app.note", Value: JSON{"blob": strings.Repeat("x", 80<<10)}})
			}); err != nil {
				t.Fatal("observer byte quota rejected commit", err)
			}
		}
		watch.sub.mu.Lock()
		count, bytes, quota := len(watch.sub.pending), watch.sub.bytes, watch.sub.quota
		root := count == 1 && watch.sub.pending[0].conversation != nil && len(watch.sub.pending[0].conversation.ops) == 1 && watch.sub.pending[0].conversation.ops[0][0] == "r"
		watch.sub.mu.Unlock()
		if !root || bytes > quota {
			t.Fatalf("owned views not charged/reset: count=%d bytes=%d quota=%d", count, bytes, quota)
		}
		frames := transcriptListener(t, watch, h.session.limits)
		transcriptIDs(t, transcriptFrame(t, frames), ids, 0)
	})
}

func TestMainTranscriptConsumedProjectionSlotReleased(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		h, c := transcriptFixture(t, b)
		watch, err := c.Watch(bg, transcriptBounds(1, 3))
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		transcriptNotes(t, h, 1)
		watch.sub.mu.Lock()
		backing := watch.sub.pending
		watch.sub.mu.Unlock()
		if len(backing) != 1 {
			t.Fatal("missing queued projection")
		}
		entered, release := make(chan struct{}), make(chan struct{})
		defer close(release)
		if err = watch.Start(func(context.Context, ConversationView, []Operation) error { close(entered); <-release; return nil }); err != nil {
			t.Fatal(err)
		}
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("missing delivery")
		}
		watch.sub.mu.Lock()
		stale := backing[0].conversation != nil || backing[0].Snapshot != nil || backing[0].Context != nil
		watch.sub.mu.Unlock()
		if stale {
			t.Fatal("consumed backing slot still retains an owned projection/context")
		}
	})
}

func TestMainTranscriptProjectionDoesNotCombineDTOValidationBudgets(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		h, c := transcriptFixture(t, b)
		watch, err := c.Watch(bg, transcriptBounds(50, 100))
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		frames := transcriptListener(t, watch, h.session.limits)
		items := make([]any, 600)
		for i := range items {
			items[i] = 0
		}
		ids := []ID{}
		if _, err = h.session.Commit(bg, func(tx *Tx) error {
			for i := 0; i < 100; i++ {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				ids = append(ids, id)
				if err = tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "app.note", Value: JSON{"items": items}}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		// The value and append operations each fit the original DTO/node limits.
		// Accounting both graphs must not invent a new joint validation envelope.
		select {
		case value := <-frames:
			transcriptIDs(t, value, ids, 0)
		case <-watch.Closed():
			end, _ := watch.End()
			t.Fatal("legal value/ops failed joint validation", end)
		case <-time.After(3 * time.Second):
			t.Fatal("missing legal large-node delivery")
		}
	})
}

func TestMainTranscriptPreparedDeliveryReadinessAndEnd(t *testing.T) {
	for _, mode := range []string{"ready", "stop", "cancel", "close", "failure"} {
		t.Run(mode, func(t *testing.T) {
			utf16Backends(t, func(t *testing.T, b backend) {
				h, c := transcriptFixture(t, b)
				watchCtx, cancelWatch := context.WithCancel(bg)
				defer cancelWatch()
				watch, err := c.Watch(watchCtx, transcriptBounds(1, 3))
				if err != nil {
					t.Fatal(err)
				}
				defer watch.Stop()
				frames := make(chan ConversationView, 1)
				type publicationKey struct{}
				var called atomic.Int32
				if err = watch.Start(func(ctx context.Context, value ConversationView, _ []Operation) error {
					called.Add(1)
					if ctx.Err() != nil || ctx.Value(publicationKey{}) != "publication" {
						return fmt.Errorf("wrong publication context")
					}
					if _, err := c.View(bg); err != nil {
						return err
					}
					frames <- value
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				id, err := h.session.MintID(bg)
				if err != nil {
					t.Fatal(err)
				}
				ready := make(chan struct{})
				defer func() {
					select {
					case <-ready:
					default:
						close(ready)
					}
				}()
				frameCtx, cancelFrame := context.WithCancel(context.WithValue(bg, publicationKey{}, "publication"))
				defer cancelFrame()
				h.session.taskBookkeeping(func() {
					prepared, receipt, candidate := mainPreparedPublication(t, h.session, frameCtx, id, ready)
					if _, err := h.session.publishSessionPublication(&prepared, receipt, &candidate); err != nil {
						t.Fatal(err)
					}
				})
				cancelFrame() // Producer cancellation cannot cancel an adopted projection.
				h.session.kickWatches()
				select {
				case <-frames:
					t.Fatal("callback crossed readiness fence")
				default:
				}
				reason := ""
				switch mode {
				case "stop":
					watch.Stop()
					reason = "stopped"
				case "cancel":
					cancelWatch()
					reason = "cancelled"
				case "close":
					if err = h.Close(bg); err != nil {
						t.Fatal(err)
					}
					reason = "session_closed"
				case "failure":
					h.session.taskBookkeeping(func() { _ = h.session.backendResult(bg, errors.New("projection publication failure"), true) })
					reason = "session_failed"
				}
				if mode != "ready" {
					select {
					case <-watch.Closed():
					case <-time.After(3 * time.Second):
						t.Fatal("prepared delivery did not end", mode)
					}
					end, _ := watch.End()
					if end.Reason != reason {
						t.Fatal("wrong prepared ending", mode, end)
					}
				}
				close(ready)
				if mode == "ready" {
					transcriptIDs(t, transcriptFrame(t, frames), []ID{id}, 0)
				} else {
					deadline := time.Now().Add(3 * time.Second)
					for {
						watch.sub.mu.Lock()
						running := watch.sub.running
						watch.sub.mu.Unlock()
						if !running {
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("ended projection worker did not drain")
						}
						time.Sleep(time.Millisecond)
					}
					if called.Load() != 0 {
						t.Fatal("ended watch invoked callback")
					}
					select {
					case <-frames:
						t.Fatal("ended watch delivered prepared frame")
					default:
					}
					current, err := watch.Value()
					if err != nil || len(current.Entries) != 0 {
						t.Fatal("ending changed last-delivered value", current, err)
					}
				}
			})
		})
	}
}

func TestMainTranscriptProjectionPreservesAdapterResnapshot(t *testing.T) {
	for _, install := range []string{"at-acquire", "after-acquire"} {
		t.Run(install, func(t *testing.T) {
			utf16Backends(t, func(t *testing.T, b backend) {
				h, c := transcriptFixture(t, b)
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				var provider ID
				for id, d := range state.Documents {
					if d.Owner == 1 && d.Kind == "pi.provider" {
						provider = id
					}
				}
				if provider == 0 {
					t.Fatal("missing provider fixture")
				}
				saved := h.session.store
				adapter := &mainDefinitionProjection{nativeStorage: saved.(nativeStorage), id: provider, origin: "before"}
				defer h.session.taskBookkeeping(func() { h.session.store = saved })
				if install == "at-acquire" {
					h.session.taskBookkeeping(func() { h.session.store = adapter })
				}
				watch, err := c.Watch(bg, transcriptBounds(3, 6))
				if err != nil {
					t.Fatal(err)
				}
				defer watch.Stop()
				h.session.taskBookkeeping(func() { h.session.store = adapter; adapter.origin = "authoritative" })
				var latest ID
				for i := 0; i < 105; i++ {
					latest = transcriptNotes(t, h, 1)[0]
				}
				frames := transcriptListener(t, watch, h.session.limits)
				for {
					value := transcriptFrame(t, frames)
					if len(value.Entries) > 0 && value.Entries[len(value.Entries)-1].ID == latest {
						if value.Docs["pi.provider"]["origin"] != "authoritative" {
							t.Fatal("projection hid authoritative adapter resnapshot", value.Docs["pi.provider"])
						}
						break
					}
				}
				// Restoring the native store cannot validate this watch's stale head.
				h.session.taskBookkeeping(func() { h.session.store = saved })
				if enabled, failed := watch.projectionStatus(); enabled || failed {
					t.Fatal("wrapped head was trusted after native restore", enabled, failed)
				}
				added := transcriptNotes(t, h, 1)
				value := transcriptFrame(t, frames)
				if len(value.Entries) == 0 || value.Entries[len(value.Entries)-1].ID != added[0] {
					t.Fatal("restored native publication lost", value)
				}
				fresh, err := c.Watch(bg, transcriptBounds(3, 6))
				if err != nil {
					t.Fatal(err)
				}
				defer fresh.Stop()
				if enabled, failed := fresh.projectionStatus(); !enabled || failed {
					t.Fatal("fresh native window did not regain its witness")
				}
				raw, err := saved.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				if raw.Documents[provider].Value["origin"] != nil {
					t.Fatal("adapter projection mutated native identity")
				}
			})
		})
	}
}
