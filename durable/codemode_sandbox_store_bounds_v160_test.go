package durable

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

const v160StoreBoundsScript = `
const attempt = (fn) => { try { fn(); return "ok"; } catch (error) { return error.name; } };
return [
	attempt(() => store(1, "x")),
	attempt(() => load({})),
	attempt(() => store("fn", () => 1)),
	attempt(() => store("big", "x".repeat(300 * 1024))),
	attempt(() => { for (let i = 0; i < 8; i++) store("k" + i, "x".repeat(200 * 1024)); }),
];`

// Source: pi v1.6.0 packages/codemode/test/sandbox.test.ts:454–479.
func TestSourceV160CodemodeSandboxStoreBounds(t *testing.T) {
	const classes = `["TypeError","TypeError","TypeError","RangeError","RangeError"]`
	const oversized = `store("img", "x".repeat(300 * 1024));`
	checkDiagnostic := func(t *testing.T, message string) {
		t.Helper()
		if !strings.Contains(message, `store("img") value has 307202 characters of JSON`) || !strings.Contains(message, "Show images with image()") {
			t.Errorf("oversized store message = %q; want exact size and image guidance", message)
		}
	}
	t.Run("inline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		engine, err := quickjs.NewEngine(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer engine.Close(context.Background())
		result := engine.Execute(ctx, v160StoreBoundsScript, quickjs.Options{})
		if !result.OK || string(result.Value) != classes {
			t.Fatalf("inline ok=%v value=%s error=%v; want %s", result.OK, result.Value, result.Error, classes)
		}
		failed := engine.Execute(ctx, oversized, quickjs.Options{})
		if failed.OK || failed.Error == nil || failed.Error.Kind != "script" {
			t.Fatal("oversized inline store did not produce a script error")
		}
		checkDiagnostic(t, failed.Error.Message)
	})
	for _, placement := range v160StandaloneWorkerPlacements() {
		t.Run(placement.name, func(t *testing.T) {
			process := officialWorkerTransport120{root: officialWorkerPackageRootV160(t)}
			_, done, result, err := observeV160StandaloneScript(t, process, placement.launcher, placement.identity, v160StoreBoundsScript)
			if !done.OK || done.Value == nil || *done.Value != classes {
				t.Fatalf("published worker result ok=%v value=%v; want %s", done.OK, done.Value, classes)
			}
			// The raw sandbox can return a retained patch larger than a
			// native Durable record. Do not relax the Go adapter's limit.
			if done.Writes == nil || len(*done.Writes) <= DefaultLimits().MaxRecordBytes {
				t.Fatal("expected the actual script to leave a patch larger than a native record")
			}
			if err == nil || !strings.Contains(err.Error(), "worker JSON exceeds limit") || result.HasValue || len(result.StoreWrites) != 0 {
				t.Fatalf("oversized patch projection: error=%v value=%v writes=%d; want rejected before result publication", err, result.HasValue, len(result.StoreWrites))
			}
			_, failed, projected := executeV160StandaloneScript(t, process, placement.launcher, placement.identity, oversized)
			if failed.Type != "done" || failed.OK || projected.Error == "" {
				t.Fatal("oversized worker store did not produce a script error")
			}
			checkDiagnostic(t, projected.Error)
		})
	}
}
