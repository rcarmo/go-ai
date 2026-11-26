package durable

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// CheckV160PublishedWorkerOutput runs a model tool call in the real published
// portable guest. The external test supplies a production filesystem without
// introducing an import cycle between this package and durable/tools.
func CheckV160PublishedWorkerOutput(t *testing.T, script string, fs FileSystem, wantDeliveries int32, registrations ...ToolRegistration) (string, MessageReceipt) {
	t.Helper()
	_, text, result := runV160PublishedWorkerOutput(t, script, fs, wantDeliveries, registrations...)
	return text, result
}

// CheckV161PublishedWorkerOutput runs the same public Harness fixture with
// the verified pi v1.6.1 portable package.
func CheckV161PublishedWorkerOutput(t *testing.T, script string, fs FileSystem) (string, MessageReceipt) {
	t.Helper()
	_, text, result := runPublishedWorkerOutput(t, officialWorkerPackageRoot(t, "GOAI_CODEMODE_V161_PACKAGE_ROOT", "1.6.1"), script, fs, 0)
	return text, result
}

func runV160PublishedWorkerOutput(t *testing.T, script string, fs FileSystem, wantDeliveries int32, registrations ...ToolRegistration) (*Harness, string, MessageReceipt) {
	t.Helper()
	return runPublishedWorkerOutput(t, officialWorkerPackageRootV160(t), script, fs, wantDeliveries, registrations...)
}

func runPublishedWorkerOutput(t *testing.T, packageRoot, script string, fs FileSystem, wantDeliveries int32, registrations ...ToolRegistration) (*Harness, string, MessageReceipt) {
	t.Helper()
	sandbox, remote := newJournalWorkerSandbox(packageRoot)
	registry := NewRegistry()
	ext, err := NewCodemodeExtension(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Install(&ext); err != nil {
		t.Fatal(err)
	}
	for _, registration := range registrations {
		if err := registry.Register(registration); err != nil {
			t.Fatal(err)
		}
	}
	var requests atomic.Int32
	ref, opts := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		out := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			out <- toolAnswer("s1", "codemode", JSON{"code": script})
		} else {
			out <- terminal("done")
		}
		close(out)
		return out
	})
	opts.Registry = registry
	opts.Env = func(context.Context, EnvTarget) (ExecutionEnvironment, error) { return fs, nil }
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, opts)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	c := root(t, h, ref)
	sub, err := c.Submit(ctx, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := sub.Wait(ctx); err != nil || result.Submission.Status != "done" || requests.Load() != 2 {
		t.Fatalf("submission=%+v requests=%d error=%v; want done and two requests", result, requests.Load(), err)
	}
	result := v160ToolResult(t, ctx, c, "s1")
	text := v160ToolResultText(result)
	if result.IsError || remote.starts.Load() != 1 || remote.deliveries.Load() != wantDeliveries {
		if len(text) > 1024 {
			text = text[:1024]
		}
		t.Fatalf("error=%v starts=%d deliveries=%d text=%q; want successful guest output and %d deliveries", result.IsError, remote.starts.Load(), remote.deliveries.Load(), text, wantDeliveries)
	}
	return h, text, result
}

func TestCodemodeWorkerOutputDecoderBoundsV160(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxRecordBytes = 64
	limits.MaxStringBytes = 32
	text := strings.Repeat("x", 100)
	var got any
	if err := decodeWorkerReturnValue([]byte(`"`+text+`"`), limits, &got); err != nil || got != text {
		t.Fatalf("spilled string result = %v, %v; want the full string", got, err)
	}
	if err := decodeWorkerReturnValue([]byte(`{"text":"`+text+`"}`), limits, &got); err == nil || !strings.Contains(err.Error(), "worker JSON exceeds limit") {
		t.Fatalf("oversized structured result error = %v; want the unchanged record limit", err)
	}
	if err := decodeWorkerReturnValue([]byte(`"`+strings.Repeat("x", MaxCodemodeFullOutputBytes+1)+`"`), limits, &got); err == nil {
		t.Fatal("terminal string larger than the full Codemode text limit was accepted")
	}
}

func TestCodemodeModelOutputMetadataBudgetV160(t *testing.T) {
	const path = "/owned/output.txt"
	const body = "BEGIN…END"
	limits := DefaultLimits()
	result := CodemodeResult{Output: body + "\n[Full output: " + path + "]", FullOutputPath: path}
	const want = "Warning: truncated output\n\n" + body + "\n\n[Full output: " + path + " (read with offset/limit)]"
	if got := codemodeModelOutput(result, limits); got != want {
		t.Errorf("model output = %q; want %q", got, want)
	}
	limits.MaxStringBytes = 32
	if got := codemodeModelOutput(result, limits); got != result.Output {
		t.Errorf("tiny limits changed the available body: %q", got)
	}
	result = CodemodeResult{Output: body, FullOutputPath: "/owned/" + strings.Repeat("x", 1000)}
	if got := codemodeModelOutput(result, DefaultLimits()); got != "Warning: truncated output\n\n"+body {
		t.Errorf("long path must stay in structured details, got %q", got)
	}
}
