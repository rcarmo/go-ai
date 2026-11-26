package durable

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
)

const v160LateReplayScript = `await tools.hold({}); await tools.echo({ text: "late" });`

// codemode.test.ts:822–841, fingerprint variant. A different host sandbox
// identity on reopen creates the same mismatch without rewriting an immutable
// native start. The real published guest supplies the initial hold.
func TestSourceV160CodemodePublishedWorkerFingerprintRefusal(t *testing.T) {
	got := runV160PublishedWorkerNestedReplay(t, v160LateReplayScript, workerReplayChangesV160{
		beforeResume: func(_ context.Context, _ Storage, sandbox *RemoteCodemodeSandbox) {
			sandbox.Identity = "another sandbox"
		},
	})
	if !got.isError || !strings.Contains(got.text, "cannot resume") {
		t.Errorf("result error=%t, text=%q; want cannot resume", got.isError, got.text)
	}
	if len(got.echoes) != 0 || got.starts != 1 {
		t.Errorf("echo effects=%v, guest starts=%d; want no echo and one guest", got.echoes, got.starts)
	}
}

// pi v1.6.1 codemode.test.ts:822–841, fingerprint variant only.
func TestSourceV161CodemodePublishedWorkerFingerprintRefusal(t *testing.T) {
	got := runPublishedWorkerNestedReplay(t, officialWorkerPackageRoot(t, "GOAI_CODEMODE_V161_PACKAGE_ROOT", "1.6.1"), v160LateReplayScript, workerReplayChangesV160{
		unchangedJournal: true,
		unchangedStart:   true,
		beforeResume: func(_ context.Context, _ Storage, sandbox *RemoteCodemodeSandbox) {
			sandbox.Identity = "another sandbox"
		},
	})
	if !got.isError || got.text != "<harness>\n[error] durable: rejected: the script cannot resume: the sandbox changed since it started\n</harness>" {
		t.Errorf("result error=%t, text=%q; want exact sandbox-change refusal", got.isError, got.text)
	}
	if len(got.echoes) != 0 || got.starts != 1 || got.replies != 0 || got.nested != 1 {
		t.Errorf("echo effects=%v, guest starts=%d, replies=%d, nested tasks=%d; want one start, no reply or echo, and one existing nested task",
			got.echoes, got.starts, got.replies, got.nested)
	}
}

// Supplemental, not a translation of the source version result. Native run
// starts are immutable, so version 0 cannot be introduced by a public commit.
func TestV160CodemodePublishedWorkerStartVersionImmutable(t *testing.T) {
	got := runV160PublishedWorkerNestedReplay(t, v160LateReplayScript, workerReplayChangesV160{
		beforeResume: func(ctx context.Context, store Storage, _ *RemoteCodemodeSandbox) {
			state, err := readStorageProjection(ctx, store, DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			for _, doc := range state.Documents {
				if doc.Kind != codemodeRunKind {
					continue
				}
				var run codemodeRunState
				if err := decodeDocumentValue(doc, DefaultLimits(), &run); err != nil {
					t.Fatal(err)
				}
				if run.Start.Version != 2 {
					t.Fatalf("saved start version = %d; want 2", run.Start.Version)
				}
				run.Start.Version = 0
				value, err := dtoObject(run, DefaultLimits())
				if err != nil {
					t.Fatal(err)
				}
				var receipts int
				result, err := store.Commit(ctx, [][]StorageWrite{{{Type: "document.change", ID: doc.ID,
					Content: &DocumentContent{Kind: "base", Version: doc.Version, Value: value}}}}, func(int, uint64) { receipts++ })
				refusal := result.Refused.AsError()
				if err != nil || result.Stored != 0 || receipts != 0 || refusal == nil || refusal.Error() != "durable: rejected: codemode start identity changed" {
					t.Fatalf("version 0 write: stored=%d receipts=%d refusal=%v err=%v; want start identity refusal", result.Stored, receipts, refusal, err)
				}
				return
			}
			t.Fatal("missing persisted Codemode start")
		},
	})
	if got.isError || got.text != "" || got.starts != 2 || len(got.echoes) != 1 || got.echoes["late"] != 1 {
		t.Fatalf("valid resume after refused version write: %+v", got)
	}
}

// codemode.test.ts:843–856. Give the cold Node subprocess time to start;
// advance the public clock 100 ms past the saved deadline, as in the source.
func TestSourceV160CodemodePublishedWorkerExpiredReplay(t *testing.T) {
	checkPublishedWorkerExpiredReplay(t, officialWorkerPackageRootV160(t))
}

func TestSourceV161CodemodePublishedWorkerExpiredReplay(t *testing.T) {
	checkPublishedWorkerExpiredReplay(t, officialWorkerPackageRoot(t, "GOAI_CODEMODE_V161_PACKAGE_ROOT", "1.6.1"))
}

const workerReplayDeadlineSource = `// @options: {"timeout_ms": 3000}
` + v160LateReplayScript

func checkPublishedWorkerExpiredReplay(t *testing.T, packageRoot string) {
	t.Helper()
	var now atomic.Int64
	now.Store(1000)
	got := runPublishedWorkerNestedReplay(t, packageRoot, workerReplayDeadlineSource, workerReplayChangesV160{
		now:          now.Load,
		beforeResume: func(_ context.Context, _ Storage, _ *RemoteCodemodeSandbox) { now.Add(3100) },
	})
	if len(got.inputs) != 1 || got.inputs[0].deadline-got.inputs[0].clock != 3000 || now.Load()-got.inputs[0].deadline != 100 {
		t.Fatalf("expected one guest, with its saved deadline now 100 ms past: starts=%+v now=%d", got.inputs, now.Load())
	}
	if !got.isError || !strings.Contains(got.text, "deadline passed") {
		t.Errorf("result error=%t, text=%q; want deadline passed", got.isError, got.text)
	}
	if len(got.echoes) != 0 || got.starts != 1 {
		t.Errorf("echo effects=%v, guest starts=%d; want no echo and one guest", got.echoes, got.starts)
	}
}

func TestV161CodemodePublishedWorkerUnexpiredReplay(t *testing.T) {
	packageRoot := officialWorkerPackageRoot(t, "GOAI_CODEMODE_V161_PACKAGE_ROOT", "1.6.1")
	var now atomic.Int64
	now.Store(1000)
	got := runPublishedWorkerNestedReplay(t, packageRoot, workerReplayDeadlineSource, workerReplayChangesV160{
		now:          now.Load,
		beforeResume: func(_ context.Context, _ Storage, _ *RemoteCodemodeSandbox) { now.Add(500) },
	})
	if got.isError || got.text != "" || got.starts != 2 || len(got.echoes) != 1 || got.echoes["late"] != 1 {
		t.Fatalf("unexpired replay = %+v; want one echo and two guest starts", got)
	}
	if len(got.inputs) != 2 || now.Load() >= got.inputs[0].deadline || got.inputs[1].deadline != got.inputs[0].deadline {
		t.Fatalf("unexpired replay changed the saved deadline: starts=%+v now=%d", got.inputs, now.Load())
	}
}
