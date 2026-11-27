package durable

import (
	"strings"
	"testing"
)

// Source: pi v1.6.1 packages/durable/test/codemode.test.ts:612–630.
func TestSourceV161CodemodePublishedWorkerRejectsEarlyStoreReplay(t *testing.T) {
	const source = `if (load("k")) { store("x", 1); return "early"; } await tools.hold({});`
	got := runPublishedWorkerNestedReplay(t, officialWorkerPackageRoot(t, "GOAI_CODEMODE_V161_PACKAGE_ROOT", "1.6.1"), source, workerReplayChangesV160{
		guestStore:       workerReplayStoreWithKV160,
		unchangedStart:   true,
		unchangedJournal: true,
		observeFinal: func(final Snapshot, owner ID) {
			assertV161WorkerStoreValues(t, final, owner, nil)
		},
	})
	if got.firstIssued == nil || *got.firstIssued != 1 || len(got.beforeClose) != 0 {
		t.Fatalf("initial drain must record one call and no results: count=%v deliveries=%+v", got.firstIssued, got.beforeClose)
	}
	if !got.isError || !strings.Contains(got.text, "it ended after result 0, where it went on before") {
		t.Errorf("replay error=%t, text=%q; want exact initial early-return diagnostic", got.isError, got.text)
	}
	if got.starts != 2 || got.replies != 0 || got.nested != 1 {
		t.Errorf("guest starts=%d replies=%d nested tasks=%d; want 2 starts, no replies and only the original task", got.starts, got.replies, got.nested)
	}
}

// Supplemental positive control: the same write is visible in the owning
// conversation when the resumed guest finishes the expected call first.
func TestV161CodemodePublishedWorkerAdmittedStoreReplay(t *testing.T) {
	const source = `await tools.hold({}); if (load("k")) { store("x", 1); return "late"; }`
	got := runPublishedWorkerNestedReplay(t, officialWorkerPackageRoot(t, "GOAI_CODEMODE_V161_PACKAGE_ROOT", "1.6.1"), source, workerReplayChangesV160{
		guestStore:     workerReplayStoreWithKV160,
		unchangedStart: true,
		observeFinal: func(final Snapshot, owner ID) {
			assertV161WorkerStoreValues(t, final, owner, JSON{"x": 1})
		},
	})
	if got.isError || got.text != "late" || got.starts != 2 || got.replies != 1 || got.nested != 1 {
		t.Fatalf("result=%q error=%t starts=%d replies=%d nested=%d; want late, 2 starts, 1 reply and the original task", got.text, got.isError, got.starts, got.replies, got.nested)
	}
}

func assertV161WorkerStoreValues(t *testing.T, final Snapshot, owner ID, want JSON) {
	t.Helper()
	var got JSON
	for _, doc := range final.Documents {
		if doc.Retired {
			continue
		}
		if doc.Kind == codemodeStoreItemKind {
			t.Fatalf("small replay fixture unexpectedly split into store items: %+v", doc)
		}
		if doc.Kind != "pi.codemode.store" {
			continue
		}
		values := generationObject(doc.Value["values"])
		if doc.Scope != "conversation" || doc.Owner != owner || doc.Family || doc.Version != 1 || got != nil || len(doc.Value) != 1 || values == nil {
			t.Fatalf("unexpected conversation store: %+v", doc)
		}
		got = values
	}
	if len(got) != len(want) || (len(want) != 0 && !equalJSONValue(got, want)) {
		t.Fatalf("committed conversation store = %v; want %v", got, want)
	}
}
