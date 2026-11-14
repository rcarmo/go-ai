package durable

import (
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func mainContextScopeHarness(t *testing.T, store Storage, clock *atomic.Int64) *Harness {
	t.Helper()
	retention := int64(1000)
	// Accepted virtual timers never fire in this fixture: publication must
	// enforce expiry without a timer callback racing the assertion.
	return taskTestHarnessOptions(t, store, Options{
		Now: clock.Load, Settings: &HarnessSettings{ContextRetentionMs: &retention},
		SetTimer: func(func(), time.Duration, TimerOptions) func() { return func() {} },
	})
}

func TestMainContextBusyFollowsEndedOwnerChain(t *testing.T) {
	for _, check := range []string{"idle-scope", "zero-retention"} {
		t.Run(check, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				entry, owner, owned, child, other := mint(t, b.store), mint(t, b.store), mint(t, b.store), mint(t, b.store), mint(t, b.store)
				contribution := contextEntry(entry, 1, goai.RoleUser, "kept")
				// An ended task still connects its owned conversation to root.
				// These are ordinary validated legacy records, not fake hosts.
				apply(t, b.store,
					Write{Op: "put-task", Task: &Task{ID: owner, Conversation: 1, Kind: "context.fixture", Status: "done", Checkpoint: JSON{}}},
					Write{Op: "create-conversation", Conversation: &Conversation{ID: owned, Owner: owner}},
					Write{Op: "put-task", Task: &Task{ID: child, Conversation: owned, Kind: "context.fixture", Status: "pending", Checkpoint: JSON{}}},
					Write{Op: "create-conversation", Conversation: &Conversation{ID: other}},
					Write{Op: "append-entry", Entry: &contribution},
				)
				var clock atomic.Int64
				clock.Store(1000)
				h := mainContextScopeHarness(t, b.store, &clock)
				if check == "idle-scope" {
					h.session.taskBookkeeping(func() {
						state := h.session.core.state // Session owns its only writer
						if targets := h.scheduler.idleTargets(state, 1); len(targets) != 1 || targets[0] != child {
							t.Fatal("fixture missing root idle membership", targets)
						}
						if !h.session.contextBusy(1) || !h.session.contextBusy(owned) || h.session.contextBusy(other) {
							t.Fatal("context busy disagrees with ownership-scoped idle work")
						}
					})
					return
				}
				if _, err := h.session.ContextView(bg, 1, 0); err != nil {
					t.Fatal(err)
				}
				zero := int64(0)
				if err := h.SetSettings(bg, HarnessSettings{ContextRetentionMs: &zero}); err != nil {
					t.Fatal(err)
				}
				h.session.taskBookkeeping(func() {
					rangeInUse := h.session.contextRanges[1]
					if rangeInUse == nil || rangeInUse.idleSince != nil {
						t.Fatal("foreground descendant's busy context discarded or marked idle")
					}
				})
			})
		})
	}
}

func TestMainContextExpiryAtPublication(t *testing.T) {
	for _, change := range []string{"becomes-busy", "entry-only"} {
		t.Run(change, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				var clock atomic.Int64
				clock.Store(1000)
				h := mainContextScopeHarness(t, b.store, &clock)
				appendContext := func(tx *Tx) error {
					id, err := tx.mintID()
					if err != nil {
						return err
					}
					return tx.appendEntryRecord(contextEntry(id, 1, goai.RoleUser, "entry"))
				}
				if _, err := h.session.Commit(bg, func(tx *Tx) (any, error) { return nil, appendContext(tx) }); err != nil {
					t.Fatal(err)
				}
				if _, err := h.session.ContextView(bg, 1, 0); err != nil {
					t.Fatal(err)
				}
				h.session.taskBookkeeping(func() {
					kept := h.session.contextRanges[1]
					if kept == nil || kept.idleSince == nil || *kept.idleSince != 1000 {
						t.Fatal("fixture did not retain idle context")
					}
				})
				clock.Store(2500)
				_, err := h.session.Commit(bg, func(tx *Tx) (any, error) {
					if change == "entry-only" {
						return nil, appendContext(tx)
					}
					id, err := tx.mintID()
					if err != nil {
						return nil, err
					}
					return nil, tx.putTaskRecord(Task{ID: id, Conversation: 1, Kind: "context.fixture", Status: "pending", Checkpoint: JSON{}})
				})
				if err != nil {
					t.Fatal(err)
				}
				h.session.taskBookkeeping(func() {
					if h.session.contextRanges[1] != nil {
						t.Fatal("overdue idle context survived publication or was resurrected as busy")
					}
				})
			})
		})
	}
}
