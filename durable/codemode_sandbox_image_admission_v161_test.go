package durable

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Source: pi v1.6.1 packages/codemode/test/sandbox.test.ts:192–204.
// This is the cooked template, including the original line breaks and tabs.
func v161InlineImageLiteral(t *testing.T) string {
	t.Helper()
	const wantDigest = "fbf988167fb2e02bcad9e00b83fb418ee13c71ece35086fcdc1ff2f7813bc7de"
	source := "\n\t\t\t\t" + strings.ReplaceAll(v160LargeImageScript(v160LargePNGData()), "\n", "\n\t\t\t\t") + "\n\t\t\t"
	if len(source) != 1048684 || fmt.Sprintf("%x", sha256.Sum256([]byte(source))) != wantDigest {
		t.Fatalf("inline literal = %d bytes, SHA-256 %x; want the exact pinned script", len(source), sha256.Sum256([]byte(source)))
	}
	return source
}

func TestSourceV161CodemodePublishedLargeImageData(t *testing.T) {
	checkPublishedLargeImageData(t, officialWorkerPackageRoot(t, "GOAI_CODEMODE_V161_PACKAGE_ROOT", "1.6.1"), v161InlineImageLiteral(t))
}

// Supplemental: measure native admission at its public sandbox boundary.
// The two admitted controls generate the same image bytes from smaller code;
// neither stands in for the original's oversized literal.
func TestV161CodemodeNativeInlineImageAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	sandbox, err := NewQuickJSSandbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sandbox.Close(context.Background()) })
	var reserved, started atomic.Int32
	sandbox.AcquireWorker = func() (func(), error) {
		reserved.Add(1)
		return func() {}, nil
	}
	sandbox.OnWorkerStarted = func(<-chan struct{}) error {
		started.Add(1)
		return nil
	}
	const capBytes = 1 << 20
	padded := func(bytes int) string {
		return v160GeneratedLargeImageScript + strings.Repeat(" ", bytes-len(v160GeneratedLargeImageScript))
	}
	for _, tc := range []struct {
		name   string
		source string
		admit  bool
	}{
		{"exact-original", v161InlineImageLiteral(t), false},
		{"one-byte-over", padded(capBytes + 1), false},
		{"one-byte-under", padded(capBytes - 1), true},
		{"exactly-at-cap", padded(capBytes), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			beforeReserved, beforeStarted := reserved.Load(), started.Load()
			vm, err := sandbox.Start(ctx, tc.source, CodemodeStart{RuntimeIdentity: sandbox.RuntimeIdentity()})
			if err != nil {
				t.Fatal(err)
			}
			defer vm.Close()
			event, err := vm.Next(ctx)
			if err != nil || event.Result == nil || event.Call != nil || event.Drain != nil {
				t.Fatalf("first event = %+v, %v; want a terminal result", event, err)
			}
			got := event.Result
			reservations, starts := reserved.Load()-beforeReserved, started.Load()-beforeStarted
			if !tc.admit {
				const failure = "Script error:\nScript sandbox failed: invalid or oversized QuickJS setup input\n\nNo tool calls were made."
				if got.Error != "sandbox: invalid or oversized QuickJS setup input" || got.HasValue || got.Output != failure || len(got.Images) != 0 || reservations != 0 || starts != 0 {
					t.Fatalf("%d-byte source: error=%q images=%d output=%d value=%v reservations=%d starts=%d; want exact setup refusal before capacity", len(tc.source), got.Error, len(got.Images), len(got.Output), got.HasValue, reservations, starts)
				}
				return
			}
			if got.Error != "" || got.HasValue || got.Output != "" || len(got.Images) != 2 || reservations != 1 || starts != 1 {
				t.Fatalf("%d-byte source: error=%q images=%d output=%d value=%v reservations=%d starts=%d; want two images and one acquired module", len(tc.source), got.Error, len(got.Images), len(got.Output), got.HasValue, reservations, starts)
			}
			for i, want := range []string{v160WrappedPNG, v160LargePNGData()} {
				if image := got.Images[i]; image.Type != "image" || image.MimeType != "image/png" || image.Data != want {
					t.Fatalf("image %d: type=%q MIME=%q bytes=%d SHA-256=%x; want the exact PNG block", i, image.Type, image.MimeType, len(image.Data), sha256.Sum256([]byte(image.Data)))
				}
			}
		})
	}
}
