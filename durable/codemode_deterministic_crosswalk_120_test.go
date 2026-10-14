package durable

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"
)

// Pinned deterministic.test.ts: reproducibility, seed-zero sequence and UTC/GC
// environment. The serial adapter has no ordinary real-clock execute mode.
func TestCodemode120PinnedDeterministicSeedsAndEnvironment(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	for _, seed := range []uint64{0, 1} {
		var previous []float64
		for _, started := range []int64{1768471200000, 1768471201000} {
			ctx, cancel := context.WithTimeout(bg, 3*time.Second)
			vm, err := sandbox.Start(ctx, `return [Math.random(),Math.random(),Date.now(),new Date(0).getTimezoneOffset(),typeof WeakRef==='undefined'?1:0,typeof FinalizationRegistry==='undefined'?1:0];`, CodemodeStart{StartedAt: started, Seed: seed})
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			event, err := vm.Next(ctx)
			closeErr := vm.Close()
			cancel()
			if err != nil || closeErr != nil || event.Result == nil || event.Result.Error != "" {
				t.Fatal(event, err, closeErr)
			}
			bytes, err := json.Marshal(event.Result.Value)
			if err != nil {
				t.Fatal(err)
			}
			var values []float64
			if err = json.Unmarshal(bytes, &values); err != nil || len(values) != 6 {
				t.Fatal(string(bytes), err)
			}
			expected := []float64{0.6711372530266764, 0.7258461452833667}
			if seed == 1 {
				expected[0] = 0.34227450605335275
			}
			for i := 0; i < 2; i++ {
				if i == 1 && seed == 1 {
					continue
				}
				if math.Abs(values[i]-expected[i]) > 1e-15 {
					t.Fatalf("seed=%d random%d=%.17g want %.17g", seed, i, values[i], expected[i])
				}
			}
			if values[2] != float64(started) || values[3] != 0 || values[4] != 1 || values[5] != 1 {
				t.Fatal("deterministic environment", values)
			}
			if previous != nil && (values[0] != previous[0] || values[1] != previous[1]) {
				t.Fatal("random depends on start time", previous, values)
			}
			previous = values
		}
	}
}
