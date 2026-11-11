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
