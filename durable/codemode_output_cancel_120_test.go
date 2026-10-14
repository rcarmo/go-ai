package durable

import (
	"context"
	"testing"
	"time"
)

type cancelOutputWriter120 struct{ entered, cancelled, release chan struct{} }

func (*cancelOutputWriter120) Cwd() string { return "/owned" }
func (w *cancelOutputWriter120) WriteCodemodeOutput(ctx context.Context, _ ID, _ string, _ string, _ []byte) (string, error) {
	close(w.entered)
	<-ctx.Done()
	close(w.cancelled)
	<-w.release
	return "", ctx.Err()
}
func TestCodemode120OutputWriterCloseJoinsAndCannotSettle(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		writer := &cancelOutputWriter120{entered: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
		defer releaseTaskGate(writer.release)
		registry := NewRegistry()
		def := taskDefinition(t, "cancel-output-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			_, err := r.RunCodemode(ctx, "run", "// @options: {\"max_output_tokens\":1}\nstore('late',true);return 'long output';", engine)
			return err
		})
		if _, err = registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, Env: func(context.Context, EnvTarget) (ExecutionEnvironment, error) { return writer, nil }})
		createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, writer.entered)
		closed := make(chan error, 1)
		go func() { closed <- h.Close(bg) }()
		awaitTaskSignal(t, writer.cancelled)
		select {
		case err := <-closed:
			t.Fatal("closed with live output writer", err)
		case <-time.After(30 * time.Millisecond):
		}
		releaseTaskGate(writer.release)
		if err = watchReceive(t, closed); err != nil {
			t.Fatal(err)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		state, err := next.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, doc := range state.Documents {
			if doc.Kind == codemodeRunKind && doc.Value["result"] != nil {
				t.Fatal("cancelled output settled")
			}
			if doc.Kind == "pi.codemode.store" && len(doc.Value["values"].(map[string]any)) != 0 {
				t.Fatal("late store adopted")
			}
		}
	})
}
