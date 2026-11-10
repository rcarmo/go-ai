package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// Source: session.ts #loadDocument, snapshotAsOf, and keys.ts GatedStorage.
// An address read waits for that address, not every unrelated admitted write.
func TestSessionV150DocumentReadsTouchOnlyTheirDependencies(t *testing.T) {
	for _, historical := range []bool{false, true} {
		name := "current"
		if historical {
			name = "historical"
		}
		t.Run(name, func(t *testing.T) {
			s := gatedSession(t)
			definition := func(kind string) *DocumentDefinition {
				d, err := DefineDocument(DefinitionOptions{Kind: kind, Version: 1, Scope: "conversation", History: "rewindable", Initial: func(JSON) (JSON, error) { return JSON{"n": 0}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
				if err != nil {
					t.Fatal(err)
				}
				return d
			}
			a, b := definition("keyed.a"), definition("keyed.b")
			var at ID
			if _, err := s.Commit(bg, func(tx *Tx) error {
				if _, err := tx.AcquireDocument(a, 1, nil, nil); err != nil {
					return err
				}
				if _, err := tx.AcquireDocument(b, 1, nil, nil); err != nil {
					return err
				}
				var err error
				at, err = tx.MintID()
				if err != nil {
					return err
				}
				return tx.AppendEntry(Entry{ID: at, Conversation: 1, Kind: "cutoff", Value: JSON{}})
			}); err != nil {
				t.Fatal(err)
			}

			entered, release := make(chan struct{}), make(chan struct{})
			released := false
			unblock := func() {
				if !released {
					released = true
					close(release)
				}
			}
			defer unblock()
			commit := s.writer.host.commit
			s.writer.host.commit = func(ctx context.Context, batches []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
				close(entered)
				<-release
				return commit(ctx, batches, receive)
			}
			stored := make(chan error, 1)
			go func() {
				_, err := s.Commit(bg, func(tx *Tx) error {
					doc, err := tx.AcquireDocument(a, 1, nil, nil)
					if err != nil {
						return err
					}
					return doc.Set(JSON{"n": 1})
				})
				stored <- err
			}()
			<-entered
			read := func(ctx context.Context, def *DocumentDefinition) error {
				var err error
				var found bool
				var value JSON
				if historical {
					value, found, err = s.SnapshotDefinitionAsOf(ctx, def, 1, nil, at)
				} else {
					value, found, err = s.SnapshotDefinition(ctx, def, 1, nil)
				}
				if err == nil && !found {
					return reject("document missing")
				}
				want := 0
				if def == a && !historical {
					want = 1
				}
				n, ok := exactNumber(value["n"])
				if err == nil && (!ok || n == nil || !n.IsInt() || n.Num().Int64() != int64(want)) {
					return reject("wrong document value at requested point")
				}
				return err
			}
			disjoint := make(chan error, 1)
			go func() { disjoint <- read(bg, b) }()
			select {
			case err := <-disjoint:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				unblock()
				<-stored
				<-disjoint
				t.Fatal("unrelated address read waited for the held storage call")
			}
			dependent := make(chan error, 1)
			queued := make(chan struct{})
			ctx := &documentGateContext{Context: bg, queued: queued}
			go func() { dependent <- read(ctx, a) }()
			// The third Done consultation is touch's select, after both queues.
			// The signal witnesses the actual gate wait, not goroutine launch.
			awaitTaskSignal(t, queued)
			select {
			case err := <-dependent:
				t.Fatalf("dependent read escaped before publication: %v", err)
			default:
			}
			unblock()
			if err := <-stored; err != nil {
				t.Fatal(err)
			}
			if err := <-dependent; err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Two queue acquisitions precede the read gate. Signal only at the gate so
// launch/admission alone cannot satisfy a dependent-read assertion.
type documentGateContext struct {
	context.Context
	queued chan struct{}
	calls  atomic.Int64
}

func (c *documentGateContext) Done() <-chan struct{} {
	if c.calls.Add(1) == 3 {
		close(c.queued)
	}
	return c.Context.Done()
}
