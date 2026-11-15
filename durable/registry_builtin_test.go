package durable

import "testing"

func TestRegistryBuiltinCompactionCannotReplaceOrRemoveAcrossReopen(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		registry := NewRegistry()
		h := openHarness(t, b.store, Options{Registry: registry})
		before, err := registry.taskSnapshot(DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		builtin := before.Task("pi.compaction")
		if builtin == nil {
			t.Fatal("builtin compaction absent")
		}
		replacement := taskDefinition(t, "pi.compaction", taskAbort)
		if _, err := registry.RegisterTask(replacement); err == nil {
			t.Fatal("builtin task replaced")
		}
		if err := registry.Install(&Extension{Name: "collision", Tasks: []*TaskDefinition{replacement}}); err == nil {
			t.Fatal("builtin replaced by extension")
		}
		registry.Uninstall(&Extension{Name: "collision"})
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		openHarness(t, reopenStoreAfterHarnessClose(t, b.store), Options{Registry: registry})
		after, err := registry.taskSnapshot(DefaultLimits())
		if err != nil || after.Task("pi.compaction") != builtin {
			t.Fatal("builtin identity replaced on reopen", err)
		}
	})
}

// Source harness/registry.ts10-26 installs builtin tokens when the registry
// is constructed; opening a Harness is not the installation boundary.
func TestSourceRegistryBuiltinCompactionBeforeHarnessOpen(t *testing.T) {
	registry := NewRegistry()
	snapshot, err := registry.taskSnapshot(DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Task("pi.compaction") != CompactionTask() {
		t.Fatal("source registry builtin absent before Harness.Open")
	}
	replacement := taskDefinition(t, "pi.compaction", taskAbort)
	if _, err := registry.RegisterTask(replacement); err == nil {
		t.Fatal("source builtin replaced before Harness.Open")
	}
	if err := registry.Install(&Extension{Name: "collision", Tasks: []*TaskDefinition{replacement}}); err == nil {
		t.Fatal("source builtin replaced by extension before Harness.Open")
	}
}
