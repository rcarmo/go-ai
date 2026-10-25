package durable

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
)

func TestTaskGraph140MigrationFailureMetadataWithoutCommit(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var migrations, phases atomic.Int64
		def := taskDefinition(t, "task.graph-migrate-140", func(context.Context, TaskRecord, *TaskRuntime) error { phases.Add(1); return nil })
		reported := make(chan struct{}, 1)
		h := taskTestHarnessOptions(t, b.store, Options{OnReport: func(error) {
			select {
			case reported <- struct{}{}:
			default:
			}
		}}, def)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		watch, err := h.WatchTaskGraph(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		frames := make(chan TaskGraph, 8)
		if err = watch.Start(func(_ context.Context, g TaskGraph, _ []Operation) error { frames <- g; return nil }); err != nil {
			t.Fatal(err)
		}
		options := def.options
		options.Version = 2
		options.Migrate = func(any, JSON, uint64) (any, JSON, error) {
			migrations.Add(1)
			return nil, nil, errors.New("migration failed")
		}
		replacement, err := DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = h.options.Registry.RegisterTask(replacement); err != nil {
			t.Fatal(err)
		}
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		graphReason140(t, frames, strconv.FormatUint(uint64(id), 10), "migration_failed")
		awaitTaskSignal(t, reported)
		after, err := h.Snapshot(bg)
		if err != nil || before.Seq != after.Seq || migrations.Load() != 1 || phases.Load() != 0 {
			t.Fatal("migration metadata committed/dispatched", err, migrations.Load(), phases.Load())
		}
	})
}
