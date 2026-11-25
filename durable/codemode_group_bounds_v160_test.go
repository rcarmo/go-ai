package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const groupedBoundDiagnosticV160 = "durable: rejected: encoded JSON byte limit"

type groupedBoundSandboxV160 struct {
	*RemoteCodemodeSandbox
	store  *SQLiteStorage
	worker *journalWorkerRemoteV160
	ready  chan codemodeCompletedCall
	gate   [2]chan struct{}
	values [3]string
	limit  int
	tight  bool
	run    *groupedBoundExecutionV160
}

func (s *groupedBoundSandboxV160) Start(ctx context.Context, code string, start CodemodeStart) (CodemodeExecution, error) {
	if start.JournalVersion != codemodeGroupedJournalVersion || !start.ParallelCalls || !start.DrainAcknowledgements {
		return nil, fmt.Errorf("worker did not start in grouped journal mode: %+v", start)
	}
	vm, err := s.RemoteCodemodeSandbox.Start(ctx, code, start)
	if err != nil {
		return nil, err
	}
	s.run = &groupedBoundExecutionV160{CodemodeExecution: vm, ack: vm.(CodemodeDrainExecution), sandbox: s}
	return s.run, nil
}

type groupedBoundExecutionV160 struct {
	CodemodeExecution
	ack     CodemodeDrainExecution
	sandbox *groupedBoundSandboxV160
	before  uint64
	acks    []int
}

func (e *groupedBoundExecutionV160) snapshot(ctx context.Context) (uint64, codemodeJournalState, error) {
	s, err := e.sandbox.store.Snapshot(ctx)
	if err != nil {
		return 0, codemodeJournalState{}, err
	}
	var state codemodeJournalState
	journals, runs := 0, 0
	for _, d := range s.Documents {
		switch d.Kind {
		case codemodeJournalKind:
			journals++
			if err := fromObject(d.Value, &state, DefaultLimits()); err != nil {
				return 0, state, err
			}
		case codemodeRunKind:
			runs++
			if d.Value["terminal"] != nil || d.Value["result"] != nil {
				return 0, state, fmt.Errorf("premature Codemode result or terminal position")
			}
		}
	}
	if runs != 1 || journals != 1 || state.Version != codemodeGroupedJournalVersion || state.FirstIssued == nil || *state.FirstIssued != 3 || len(state.Issued) != 3 {
		return 0, state, fmt.Errorf("SQLite lacks one live run with exactly three admitted calls in a versioned journal")
	}
	for i, call := range state.Issued {
		if call.ID != i+1 || call.Name != "receipt" || !call.Global || call.Arguments["args"] != string(rune('a'+i)) {
			return 0, state, fmt.Errorf("wrong admitted call %d: %+v", i+1, call)
		}
	}
	return s.Seq, state, nil
}

func (e *groupedBoundExecutionV160) check(ctx context.Context, stage string, advance uint64, sent int32, ends []int, counts ...int) error {
	seq, state, err := e.snapshot(ctx)
	if err != nil {
		return err
	}
	if got := e.sandbox.worker.deliveries.Load(); got != sent {
		return fmt.Errorf("%s: worker deliveries=%d, want %d", stage, got, sent)
	}
	if seq != e.before+advance || len(state.Deliveries) != len(counts) || !slices.Equal(state.BatchEnds, ends) {
		return fmt.Errorf("%s: SQLite advance=%d, results=%d, batch ends=%v; want %d/%d/%v", stage, seq-e.before, len(state.Deliveries), state.BatchEnds, advance, len(counts), ends)
	}
	for i, delivery := range state.Deliveries {
		count := 0
		if delivery.IssuedAfter != nil {
			count = *delivery.IssuedAfter
		}
		if delivery.Call.ID != i+1 || delivery.Seq != i+1 || delivery.Value != e.sandbox.values[i] || count != counts[i] || (counts[i] == 0 && delivery.IssuedAfter != nil) {
			return fmt.Errorf("%s: wrong persisted outcome or issued count for result %d (count %d, want %d)", stage, i+1, count, counts[i])
		}
	}
	return nil
}

func (e *groupedBoundExecutionV160) Next(ctx context.Context) (CodemodeEvent, error) {
	event, err := e.CodemodeExecution.Next(ctx)
	if err != nil || event.Drain == nil || event.Drain.DeliveryID != 1 {
		return event, err
	}
	if event.Drain.Final || event.Drain.Issued != 3 {
		return CodemodeEvent{}, fmt.Errorf("unexpected post-a worker drain: %+v", event.Drain)
	}
	// Hold the real worker at a's drain. Observe both complete host outcomes
	// in the coordinator's private queue, in b/c order, not just callback entry.
	var pending [2]codemodeCompletedCall
	for i, gate := range e.sandbox.gate {
		close(gate)
		select {
		case result := <-e.sandbox.ready:
			if result.err != nil || result.delivery.Call.ID != i+2 || result.delivery.Value != e.sandbox.values[i+1] {
				return CodemodeEvent{}, fmt.Errorf("wrong completion at queue position %d: call=%d err=%v", i, result.delivery.Call.ID, result.err)
			}
			pending[i] = result
		case <-ctx.Done():
			return CodemodeEvent{}, ctx.Err()
		}
	}
	seq, state, err := e.snapshot(ctx)
	if err != nil {
		return CodemodeEvent{}, err
	}
	e.before = seq
	if err := e.check(ctx, "held post-a drain", 0, 1, []int{1}, 0); err != nil {
		return CodemodeEvent{}, err
	}
	// Size the actual proposed journal from the persisted prefix and pending
	// outcomes. Both callbacks individually produce legal outcomes, b plus
	// the prefix fits, and appending both exceeds only the tight document cap.
	count := 3
	state.Deliveries[0].IssuedAfter = &count
	for i, result := range pending {
		delivery := result.delivery
		delivery.Seq = i + 2
		state.Deliveries = append(state.Deliveries, delivery)
		state.BatchEnds = []int{1, i + 2}
		wire, err := json.Marshal(state)
		if err != nil {
			return CodemodeEvent{}, err
		}
		if (len(wire) > e.sandbox.limit) != (i == 1 && e.sandbox.tight) {
			return CodemodeEvent{}, fmt.Errorf("wrong fixture size: encoded journal through result %d is %d bytes, cap %d", i+2, len(wire), e.sandbox.limit)
		}
	}
	for _, result := range pending {
		e.sandbox.ready <- result
	}
	return event, nil
}

func (e *groupedBoundExecutionV160) AcknowledgeDrain(ctx context.Context, drain CodemodeDrain) error {
	var err error
	switch drain.DeliveryID {
	case 1:
		if e.sandbox.tight {
			err = e.check(ctx, "count-only ack after a", 1, 1, []int{1}, 3)
		} else {
			err = e.check(ctx, "whole group ack after a", 1, 1, []int{1, 3}, 3, 0, 0)
		}
	case 2:
		if e.sandbox.tight {
			err = e.check(ctx, "count-only ack after b", 3, 2, []int{1, 2}, 3, 3)
		} else {
			err = e.check(ctx, "reused coverage after b", 1, 2, []int{1, 3}, 3, 0, 0)
		}
	}
	if err != nil {
		return err
	}
	if err := e.ack.AcknowledgeDrain(ctx, drain); err != nil {
		return err
	}
	e.acks = append(e.acks, drain.DeliveryID)
	return nil
}

func (e *groupedBoundExecutionV160) Deliver(ctx context.Context, delivery CodemodeDelivery) error {
	if e.sandbox.tight {
		switch delivery.Call.ID {
		case 2:
			if err := e.check(ctx, "before individual b delivery", 2, 1, []int{1, 2}, 3, 0); err != nil {
				return err
			}
		case 3:
			return fmt.Errorf("attempted worker delivery of oversized result c without a durable outcome")
		}
	}
	return e.CodemodeExecution.Deliver(ctx, delivery)
}

// Supplemental to the published grouped-delivery oracle in codemode.test.ts:
// Go's configured journal document cap is native, not an upstream assertion.
func TestV160CodemodeGroupedDocumentLimitFallback(t *testing.T) {
	for _, mode := range []struct {
		name  string
		limit int
	}{{"fits", 24 << 10}, {"tight", 16 << 10}} {
		t.Run(mode.name, func(t *testing.T) {
			remote, worker := newJournalWorkerSandboxV160(t)
			limits := DefaultLimits()
			limits.MaxDocumentBytes = mode.limit
			store, err := openSQLiteTestDirectory(filepath.Join(t.TempDir(), "journal"), SQLiteOptions{Limits: &limits})
			if err != nil {
				t.Fatal(err)
			}
			s := &groupedBoundSandboxV160{
				RemoteCodemodeSandbox: remote, worker: worker, store: store, limit: mode.limit, tight: mode.name == "tight",
				ready: make(chan codemodeCompletedCall, 3), gate: [2]chan struct{}{make(chan struct{}), make(chan struct{})},
				values: [3]string{"a", strings.Repeat("b", 9000), strings.Repeat("c", 9000)},
			}
			var effects [3]atomic.Int32
			globals := []CodemodeGlobal{{Name: "receipt", Version: 1, Execute: func(ctx context.Context, arg any) (any, error) {
				label, ok := arg.(string)
				if !ok || len(label) != 1 || label[0] < 'a' || label[0] > 'c' {
					return nil, fmt.Errorf("unexpected callback %v", arg)
				}
				i := label[0] - 'a'
				effects[i].Add(1)
				if i > 0 {
					select {
					case <-s.gate[i-1]:
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				return s.values[i], nil
			}}}
			def := taskDefinition(t, "grouped-bound-v160", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				const script = `const a=receipt("a"), b=receipt("b"), c=receipt("c"); return (await a)+":"+(await b).length+":"+(await c).length`
				result, err := r.runCodemodeWithCompletionQueue(ctx, "run", script, s, nil, nil, nil, nil, s.ready)
				wantAcks := []int{0, 1, 2, 3}
				if s.tight {
					if err == nil || err.Error() != groupedBoundDiagnosticV160 {
						return fmt.Errorf("individual c write: got %v, want %s", err, groupedBoundDiagnosticV160)
					}
					if err := s.run.check(ctx, "prefix after rejected c", 3, 2, []int{1, 2}, 3, 3); err != nil {
						return err
					}
					wantAcks = []int{0, 1, 2}
				} else if err != nil || result.Error != "" || !result.HasValue || result.Value != "a:9000:9000" {
					return fmt.Errorf("fitting group did not reach guest: result=%+v err=%v", result, err)
				}
				if !slices.Equal(s.run.acks, wantAcks) {
					return fmt.Errorf("worker drain acks=%v, want %v", s.run.acks, wantAcks)
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			})
			registry := NewRegistry()
			if _, err := registry.RegisterTask(def); err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, store, Options{Registry: registry, CodemodeGlobals: globals})
			id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			if err := h.Resume(ctx); err != nil {
				t.Fatal(err)
			}
			row, err := h.WaitForTask(ctx, id)
			if row.State.Outcome != nil && row.State.Outcome.Error != nil {
				t.Fatal(row.State.Outcome.Error.Message)
			}
			if err != nil || row.State.Outcome == nil || row.State.Outcome.Status != "completed" {
				t.Fatalf("test task did not complete: %+v; %v", row, err)
			}
			for i := range effects {
				if n := effects[i].Load(); n != 1 {
					t.Fatalf("callback %c ran %d times, want one", 'a'+i, n)
				}
			}
			wantDeliveries := int32(3)
			if s.tight {
				wantDeliveries = 2
			}
			if starts, deliveries := worker.starts.Load(), worker.deliveries.Load(); starts != 1 || deliveries != wantDeliveries {
				t.Fatalf("worker starts=%d deliveries=%d, want 1/%d", starts, deliveries, wantDeliveries)
			}
		})
	}
}
