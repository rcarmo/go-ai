package durable

import (
	"errors"
	"runtime"
	"testing"
	"weak"
)

// Actual pointerful container backing arrays, not tiny scalar proxies. The
// pinned runtime's weak/pointer_test.go uses explicit GC and last-use liveness;
// the public weak API does not guarantee eventual nil on every implementation.
type groupInputWeak struct {
	batches weak.Pointer[Batch]
	writes  weak.Pointer[Write]
	entry   weak.Pointer[Entry]
	receipt weak.Pointer[groupReceiptWitness]
}
type groupReceiptWitness struct{ calls []int }
type groupInputHold struct {
	batches []Batch
	receive func(int, uint64) error
}

// Keep allocation/call frames out of the GC assertion frame. No caller receives
// a strong input or receipt reference unless the deliberate retention control
// requests one. Weak.Make forces the pointerful objects onto the heap.
//
//go:noinline
func runGroupInputLifetime(t *testing.T, store Storage, outcome string, retain bool) (groupInputWeak, *groupInputHold) {
	t.Helper()
	batches, _ := groupEntryBatches(t, store, 3)
	witness := &groupReceiptWitness{calls: []int{0}}
	failure := errors.New("receipt lifetime fixture")
	receive := func(index int, seq uint64) error {
		witness.calls[0]++
		if outcome == "receipt-error" && index == 1 {
			return failure
		}
		return nil
	}
	refs := groupInputWeak{weak.Make(&batches[0]), weak.Make(&batches[0].Writes[0]), weak.Make(batches[0].Writes[0].Entry), weak.Make(witness)}
	if outcome == "refusal" {
		batches[1].Writes = nil
	}
	result, err := store.(nativeStorage).nativeCore().commitBatches(bg, batches, receive, false)
	switch outcome {
	case "success":
		if err != nil || result.stored != 3 || result.refused != nil || witness.calls[0] != 3 {
			t.Fatal(result, err, witness.calls)
		}
	case "refusal":
		if err != nil || result.stored != 1 || result.refused == nil || witness.calls[0] != 1 {
			t.Fatal(result, err, witness.calls)
		}
	case "receipt-error":
		if !errors.Is(err, failure) || witness.calls[0] != 2 {
			t.Fatal(result, err, witness.calls)
		}
	default:
		t.Fatal("invalid lifetime outcome")
	}
	if retain {
		return refs, &groupInputHold{batches, receive}
	}
	return refs, nil
}

//go:noinline
func groupInputsCollected(refs groupInputWeak) bool {
	return refs.batches.Value() == nil && refs.writes.Value() == nil && refs.entry.Value() == nil && refs.receipt.Value() == nil
}

//go:noinline
func checkGroupInputsRetained(t *testing.T, refs groupInputWeak, hold *groupInputHold) {
	t.Helper()
	if refs.batches.Value() == nil || refs.writes.Value() == nil || refs.entry.Value() == nil || refs.receipt.Value() == nil {
		t.Fatal("retention control lost a reachable object")
	}
	runtime.KeepAlive(hold)
}

func TestInternalGroupReleasesInputContainersAndReceipt(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		for _, outcome := range []string{"success", "refusal", "receipt-error"} {
			t.Run(outcome, func(t *testing.T) {
				refs, hold := runGroupInputLifetime(t, b.store, outcome, true)
				for cycle := 0; cycle < 3; cycle++ {
					runtime.GC()
					checkGroupInputsRetained(t, refs, hold)
				}
				// Release the same objects that the positive control kept reachable. The
				// native core/backend stays open and retains its durable materialised state.
				hold = nil
				for cycle := 0; cycle < 5; cycle++ {
					runtime.GC()
					if groupInputsCollected(refs) {
						break
					}
				}
				if !groupInputsCollected(refs) {
					t.Fatal("settled input containers or receipt capture remain reachable")
				}
				runtime.KeepAlive(b.store)
			})
			t.Run(outcome+"-without-control", func(t *testing.T) {
				refs, _ := runGroupInputLifetime(t, b.store, outcome, false)
				for cycle := 0; cycle < 5; cycle++ {
					runtime.GC()
					if groupInputsCollected(refs) {
						break
					}
				}
				if !groupInputsCollected(refs) {
					t.Fatal("settled input containers or receipt capture remain reachable")
				}
				runtime.KeepAlive(b.store)
			})
		}
		state, err := b.store.Snapshot(bg)
		if err != nil || len(state.Entries) == 0 {
			t.Fatal("open backend lost its materialised state", err)
		}
		for _, entry := range state.Entries {
			if legacyEntryValue(entry)["text"] != "original" {
				t.Fatal("input collection changed durable data", entry)
			}
		}
	})
}
