package durable

import (
	"context"
	"testing"
	"time"
)

func TestCodemode120CustomGlobalCloseJoinsCallback(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		defer releaseTaskGate(release)
		registry := NewRegistry()
		def := taskDefinition(t, "global-cancel-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			_, err := r.RunCodemode(ctx, "run", `await host.wait(null);store("late",true);`, engine)
			return err
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, CodemodeGlobals: []CodemodeGlobal{{Name: "host.wait", Version: 1, Execute: func(ctx context.Context, _ any) (any, error) {
			close(entered)
			<-ctx.Done()
			close(cancelled)
			<-release
			return nil, ctx.Err()
		}}}})
		createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		closed := make(chan error, 1)
		go func() { closed <- h.Close(bg) }()
		awaitTaskSignal(t, cancelled)
		select {
		case err := <-closed:
			t.Fatal("closed with live global", err)
		case <-time.After(30 * time.Millisecond):
		}
		releaseTaskGate(release)
		if err = watchReceive(t, closed); err != nil {
			t.Fatal(err)
		}
		store := reopenStoreAfterHarnessClose(t, b.store)
		next := openHarness(t, store, h.options)
		state, err := next.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, doc := range state.Documents {
			if doc.Kind == codemodeRunKind && doc.Value["result"] != nil {
				t.Fatal("cancelled global settled final result")
			}
			if doc.Kind == "pi.codemode.store" && len(doc.Value["values"].(map[string]any)) != 0 {
				t.Fatal("late store adopted")
			}
		}
	})
}
