package durable

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Delegate actual writes to the supplied capability. The first harness stops
// after a successful write but before Codemode receives its return.
type codemodeSpillWriteProbeV160 struct {
	FileSystem
	written chan<- string
	cutoff  bool
}

func (p codemodeSpillWriteProbeV160) WriteFile(ctx context.Context, path string, value []byte) error {
	if err := p.FileSystem.WriteFile(ctx, path, value); err != nil {
		return err
	}
	select {
	case p.written <- path:
	case <-ctx.Done():
		return ctx.Err()
	}
	if p.cutoff {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

// CheckV160PublishedWorkerSpillReplay contains the harness fixture shared with
// the external test, which supplies genuinely local tools.FileSystem access.
// The test lives outside this package to avoid an import cycle with tools.
func CheckV160PublishedWorkerSpillReplay(t *testing.T, firstFS, replayFS FileSystem) {
	t.Helper()
	sandbox, remote := newJournalWorkerSandboxV160(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	registry := NewRegistry()
	ext, err := NewCodemodeExtension(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Install(&ext); err != nil {
		t.Fatal(err)
	}
	const source = "// @options: {\"max_output_tokens\": 20}\nreturn \"x\".repeat(1000);"
	var requests atomic.Int32
	ref, opts := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			ch <- toolAnswer("s1", "codemode", JSON{"code": source})
		} else {
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	opts.Registry = registry
	written := make(chan string, 2)
	opts.Env = func(context.Context, EnvTarget) (ExecutionEnvironment, error) {
		return codemodeSpillWriteProbeV160{FileSystem: firstFS, written: written, cutoff: true}, nil
	}
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	firstHarness := openHarness(t, store, opts)
	conversation := root(t, firstHarness, ref)
	sub, err := conversation.Submit(ctx, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := firstHarness.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	settled := make(chan error, 1)
	go func() { _, err := sub.Wait(ctx); settled <- err }()
	var first string
	select {
	case first = <-written:
	case err := <-settled:
		t.Fatalf("submission settled before spill: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Extra byte checks follow the source formatter: no header or trailing LF
	// is in the spill, only the returned text.
	want := strings.Repeat("x", 1000)
	if raw, err := firstFS.ReadBinaryFile(ctx, first); err != nil || string(raw) != want {
		t.Fatalf("first spill length=%d exact=%t err=%v", len(raw), string(raw) == want, err)
	}
	if err := firstHarness.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopenedStore := reopenStoreAfterHarnessClose(t, store).(*SQLiteStorage)
	t.Cleanup(func() { _ = reopenedStore.Close(context.Background()) })
	state, err := reopenedStore.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runs := 0
	for _, doc := range state.Documents {
		if doc.Kind != codemodeRunKind {
			continue
		}
		runs++
		var run codemodeRunState
		if err := decodeDocumentValue(doc, DefaultLimits(), &run); err != nil {
			t.Fatal(err)
		}
		if run.Result != nil || run.OutputDir != filepath.Dir(first) || run.OutputFS != firstFS.ID() {
			t.Errorf("before replay: settled=%t, output dir=%q, filesystem=%q; want undecided result and saved capability address", run.Result != nil, run.OutputDir, run.OutputFS)
		}
	}
	if runs != 1 {
		t.Fatalf("persisted runs = %d; want one", runs)
	}
	opts.Env = func(context.Context, EnvTarget) (ExecutionEnvironment, error) {
		return codemodeSpillWriteProbeV160{FileSystem: replayFS, written: written}, nil
	}
	next := openHarness(t, reopenedStore, opts)
	if err := next.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := next.Submission(ctx, sub.ID())
	if err != nil || reopened == nil {
		t.Fatalf("reopen submission: %v, %v", reopened, err)
	}
	if result, err := reopened.Wait(ctx); err != nil || result.Submission.Status != "done" { // L1034
		t.Fatalf("reopened submission status=%q err=%v; want done", result.Submission.Status, err)
	}
	c, err := next.Conversation(ctx, conversation.ID())
	if err != nil {
		t.Fatal(err)
	}
	result := v160ToolResult(t, ctx, c, "s1")
	if result.IsError || result.Details["fullOutputPath"] != first { // L1037
		t.Errorf("published spill path=%v, error=%t; want %q", result.Details["fullOutputPath"], result.IsError, first)
	}
	if err := next.Close(ctx); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(filepath.Dir(first))
	if err != nil || len(files) != 1 || files[0].Name() != "output.txt" || files[0].IsDir() { // L1039
		t.Errorf("files at the first spill directory = %v, %v; want [output.txt]", files, err)
	}
	if raw, err := replayFS.ReadBinaryFile(ctx, first); err != nil || string(raw) != want {
		t.Errorf("replayed spill length=%d exact=%t err=%v", len(raw), string(raw) == want, err)
	}
	select {
	case second := <-written:
		if second != first {
			t.Errorf("real replay write path=%q; want first path %q", second, first)
		}
	default:
		t.Error("replay did not write the full output")
	}
	if len(written) != 0 || requests.Load() != 2 || remote.starts.Load() != 2 {
		t.Errorf("extra writes=%d, model requests=%d, guest starts=%d; want 0, 2, 2", len(written), requests.Load(), remote.starts.Load())
	}
}
