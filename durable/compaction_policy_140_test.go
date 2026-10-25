package durable

import "testing"

func TestCompaction140ModelOverridesResolvePinAndValidate(t *testing.T) {
	zero := 0
	disabled := false
	reserve := 100
	host := &Harness{options: Options{Settings: &HarnessSettings{Compaction: &CompactionSettings{ReserveTokens: &reserve, ModelOverrides: map[string]CompactionSettings{"provider/model": {Enabled: &disabled, KeepRecentTokens: &zero}}}}}}
	settings := host.resolvedSettings(RequestSettings{})
	matching := compactionPolicy(settings, ModelRef{Provider: "provider", ID: "model"})
	other := compactionPolicy(settings, ModelRef{Provider: "provider", ID: "other"})
	if matching.Enabled || matching.KeepRecentTokens != 0 || matching.ReserveTokens != 100 || !other.Enabled || other.KeepRecentTokens != 20000 {
		t.Fatal(matching, other)
	}
	pinned, err := cloneSettings(settings, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	reserve = 999
	if compactionPolicy(pinned, ModelRef{Provider: "provider", ID: "model"}).ReserveTokens != 100 {
		t.Fatal("pinned model policy aliases host")
	}
	for _, override := range []CompactionSettings{{ReserveTokens: func() *int { n := -1; return &n }()}, {ModelOverrides: map[string]CompactionSettings{"nested": {}}}} {
		if _, err := cloneSettings(RequestSettings{CompactionModelOverrides: map[string]CompactionSettings{"provider/model": override}}, DefaultLimits()); err == nil {
			t.Fatal("invalid model override accepted")
		}
	}
}
