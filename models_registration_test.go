package goai

import "testing"

func TestBuiltinRegistrationPreservesDynamicModelsAndCopies(t *testing.T) {
	modelRuntimeMu.RLock()
	previous := defaultRuntime
	modelRuntimeMu.RUnlock()
	t.Cleanup(func() { _ = RegisterRuntimeModels(previous) })
	runtime := NewModelRuntime(nil)
	if err := RegisterRuntimeModels(runtime); err != nil {
		t.Fatal(err)
	}
	RegisterDynamicModelProvider(StaticModelProvider{Provider: "batch-test", Models: []*Model{{ID: "custom", Provider: "batch-test"}}})
	RegisterBuiltinModels()
	RegisterBuiltinModels()
	if GetModel("batch-test", "custom") == nil {
		t.Fatal("built-in registration removed an unrelated dynamic provider")
	}
	for i := range builtinModels {
		want := &builtinModels[i]
		got := GetModel(want.Provider, want.ID)
		if got == nil || got.Name != want.Name {
			t.Fatalf("missing built-in %s/%s", want.Provider, want.ID)
		}
		modelsMu.RLock()
		legacy := models[string(want.Provider)+"/"+want.ID]
		modelsMu.RUnlock()
		if legacy == nil || legacy.Name != want.Name {
			t.Fatalf("legacy catalogue missing %s/%s", want.Provider, want.ID)
		}
		if len(got.Input) > 0 {
			original := got.Input[0]
			got.Input[0] = "mutated"
			if GetModel(want.Provider, want.ID).Input[0] != original || legacy.Input[0] != original || want.Input[0] != original {
				t.Fatal("model input slice escaped copy boundary")
			}
		}
	}
}
