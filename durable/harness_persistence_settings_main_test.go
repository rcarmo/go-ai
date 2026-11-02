package durable

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
)

// f9a26365 harness/harness.ts:218-230 and harness/agent.ts:66-79:
// persistence reads CURRENT host progress settings independently of throttling.
func mainPersistenceProgress(window *float64) *ProgressSettings {
	return &ProgressSettings{PersistWithinMs: window}
}
func mainPersistenceValue(p *ProgressSettings) (float64, bool) {
	if p == nil || p.PersistWithinMs == nil {
		return 0, false
	}
	return *p.PersistWithinMs, true
}
func mainHarnessWindow(t *testing.T, h *Harness, want float64) {
	t.Helper()
	got, err := h.session.persistenceWindow(nil)
	if err != nil || got != want {
		t.Fatalf("host persistence window got%v want%v: %v", got, want, err)
	}
}

func TestMainHarnessPersistencePolicyDynamic(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		// The new finite field must also pass through the public JSON shape.
		var progress ProgressSettings
		if err := json.Unmarshal([]byte(`{"persistWithinMs":400,"partialIntervalMs":25}`), &progress); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Settings: &HarnessSettings{Progress: &progress}})
		mainHarnessWindow(t, h, 400)
		*progress.PersistWithinMs = 700
		*progress.PartialIntervalMs = 80
		mainHarnessWindow(t, h, 400) // Open owns its input.
		window := .25
		settings := HarnessSettings{Progress: mainPersistenceProgress(&window)}
		if err := h.SetSettings(bg, settings); err != nil {
			t.Fatal(err)
		}
		mainHarnessWindow(t, h, .25)
		window = 600
		mainHarnessWindow(t, h, .25) // SetSettings owns its input too.
		// A native per-request legacy Stream.Progress policy cannot choose the
		// global Session persistence window; Source reads host.progress only.
		if err := h.SetSettings(bg, HarnessSettings{Stream: RequestSettings{Progress: mainPersistenceProgress(&window)}}); err != nil {
			t.Fatal(err)
		}
		mainHarnessWindow(t, h, 1000)
		resolved := h.resolvedSettings(RequestSettings{Progress: mainPersistenceProgress(&window)})
		if got, ok := mainPersistenceValue(resolved.Progress); !ok || got != 1000 {
			t.Fatal("request selected host persistence policy", got, ok)
		}
		got, err := h.session.persistenceWindow([]float64{.125})
		if err != nil || got != .125 {
			t.Fatal("explicit commit policy lost", got, err)
		}
		called := false
		_, err = h.session.commitDeferred(bg, func(*Tx) error { called = true; return nil })
		if !errors.Is(err, ErrUnsupported) || called {
			t.Fatal("policy wiring prematurely enabled Harness deferral", err, called)
		}
	})
}

func TestMainHarnessPersistencePolicyValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value *float64
		want  float64
	}{
		{"default", nil, 1000},
		{"zero", func() *float64 { v := float64(0); return &v }(), 0},
		{"negative-zero", func() *float64 { v := math.Copysign(0, -1); return &v }(), 0},
		{"fraction", func() *float64 { v := .125; return &v }(), .125},
		{"infinity", func() *float64 { v := math.Inf(1); return &v }(), math.Inf(1)},
		{"nan-default", func() *float64 { v := math.NaN(); return &v }(), 1000},
		{"negative-infinity-default", func() *float64 { v := math.Inf(-1); return &v }(), 1000},
		{"negative-default", func() *float64 { v := float64(-1); return &v }(), 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			utf16Backends(t, func(t *testing.T, b backend) {
				h := openHarness(t, b.store, Options{Settings: &HarnessSettings{Progress: mainPersistenceProgress(tc.value)}})
				mainHarnessWindow(t, h, tc.want)
				resolved := h.resolvedSettings(RequestSettings{})
				if got, ok := mainPersistenceValue(resolved.Progress); !ok || got != tc.want {
					t.Fatal("resolved window", got, ok)
				}
				if err := h.SetSettings(bg, HarnessSettings{Progress: mainPersistenceProgress(tc.value)}); err != nil {
					t.Fatal(err)
				}
				mainHarnessWindow(t, h, tc.want)
			})
		})
	}
}

func TestMainHarnessPersistenceRuntimeAndJSON(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		window := math.Inf(1)
		progress := mainPersistenceProgress(&window)
		for _, v := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
			encoded, err := json.Marshal(mainPersistenceProgress(&v))
			var wire map[string]any
			if err != nil || json.Unmarshal(encoded, &wire) != nil {
				t.Fatal("progress JSON", err)
			}
			if value, ok := wire["persistWithinMs"]; !ok || value != nil {
				t.Error("Source JSON nonfinite field must become null", string(encoded))
			}
		}
		seen := make(chan RequestSettings, 1)
		def := taskDefinition(t, "test.persistence-settings", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			settings, err := r.Settings(ctx)
			if err != nil {
				return err
			}
			seen <- settings
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		h := taskTestHarnessOptions(t, b.store, Options{Settings: &HarnessSettings{Progress: progress}}, def)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		settings := watchReceive(t, seen)
		if got, ok := mainPersistenceValue(settings.Progress); !ok || !math.IsInf(got, 1) {
			t.Error("runtime setting lost Infinity", got, ok)
		}
		if _, err := h.WaitForTask(bg, id); err != nil {
			t.Fatal(err)
		}
		// The process-local setting is copied without a JSON roundtrip losing
		// infinity; serialising that public value still follows JSON.stringify.
		if _, err := json.Marshal(settings); err != nil {
			t.Fatal("resolved settings cannot be serialised", err)
		}
	})
}

// Embedding the native type must not grant callers encoding authority.
type mainPersistenceMarshalCaller struct{ ProgressSettings }

func TestMainHarnessPersistenceJSONBoundary(t *testing.T) {
	infinity := math.Inf(1)
	progress := mainPersistenceProgress(&infinity)
	for _, value := range []any{*progress, progress} {
		encoded, err := encodeBounded(value, DefaultLimits(), 4096)
		if err != nil || string(encoded) != `{"persistWithinMs":null}` {
			t.Fatal("native settings JSON", string(encoded), err)
		}
	}
	for _, value := range []any{
		mainPersistenceMarshalCaller{*progress},
		&mainPersistenceMarshalCaller{*progress},
		JSON{"persistWithinMs": infinity},
	} {
		var rejection *StorageRejected
		if _, err := encodeBounded(value, DefaultLimits(), 4096); !errors.As(err, &rejection) {
			t.Fatalf("caller authority/nonfinite number accepted: %T: %v", value, err)
		}
	}
	for _, tc := range []struct {
		value ProgressSettings
		want  string
	}{
		{ProgressSettings{}, `{}`},
		{*mainPersistenceProgress(new(float64)), `{"persistWithinMs":0}`},
		{*mainPersistenceProgress(func() *float64 { v := .125; return &v }()), `{"persistWithinMs":0.125}`},
	} {
		native, err := encodeBounded(tc.value, DefaultLimits(), 4096)
		public, publicErr := json.Marshal(tc.value)
		if err != nil || publicErr != nil || string(native) != tc.want || string(public) != tc.want {
			t.Fatal("settings encoding mismatch", string(native), string(public), err, publicErr)
		}
	}
	// The returned task settings must not alias the caller or subsequent copies.
	input := RequestSettings{Progress: progress}
	copy1, err := cloneSettings(input, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	copy2, err := cloneSettings(input, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	*copy1.Progress.PersistWithinMs = 9
	if !math.IsInf(*input.Progress.PersistWithinMs, 1) || !math.IsInf(*copy2.Progress.PersistWithinMs, 1) {
		t.Fatal("settings ownership lost")
	}
}
