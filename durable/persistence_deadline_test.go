package durable

import (
	"math"
	"math/big"
	"math/rand"
	"testing"
	"time"
)

// Independent fixed-precision binary arithmetic, separate from production's
// rational arithmetic. 2304 bits retain every binary64 window bit and any int64
// origin throughout addition, clock subtraction and nanosecond scaling.
func referencePersistenceAt(clock int64, window float64) *big.Float {
	value := new(big.Float).SetPrec(2304).SetFloat64(window)
	if clock >= -(1<<53-1) && clock <= 1<<53-1 {
		return value.SetFloat64(float64(clock) + window) // Source Number addition.
	}
	return value.Add(value, new(big.Float).SetPrec(2304).SetInt64(clock))
}

func referencePersistenceDelay(at *big.Float, now int64, sourceNumber bool) time.Duration {
	value := new(big.Float).SetPrec(2304).Copy(at)
	if sourceNumber {
		absolute, _ := at.Float64()
		value.SetFloat64(absolute - float64(now))
	} else {
		value.Sub(value, new(big.Float).SetPrec(2304).SetInt64(now))
	}
	if value.Sign() <= 0 {
		return 0
	}
	value.Mul(value, new(big.Float).SetPrec(2304).SetInt64(int64(time.Millisecond)))
	if value.IsInf() {
		return time.Duration(math.MaxInt64)
	}
	nanos, _ := value.Int(nil)
	if !nanos.IsInt64() {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(nanos.Int64())
}

func TestPersistenceDeadlineIndependentArithmetic(t *testing.T) {
	rng := rand.New(rand.NewSource(6139))
	clocks := []int64{math.MinInt64, math.MaxInt64, -(1 << 53), 1 << 53, -1, 0, 1, 1700000000000}
	windows := []float64{0, math.SmallestNonzeroFloat64, .1, .125, 1.25, 1000, 9223372036854, math.MaxFloat64, math.Inf(1)}
	for i := 0; i < 4096; i++ {
		clock, other, now := int64(rng.Uint64()), int64(rng.Uint64()), int64(rng.Uint64())
		window, next := math.Float64frombits(rng.Uint64()&0x7fefffffffffffff), math.Float64frombits(rng.Uint64()&0x7fefffffffffffff)
		if i%2 == 0 {
			clock, other = clocks[i%len(clocks)], clocks[(i+1)%len(clocks)]
			window, next = windows[i%len(windows)], windows[(i+1)%len(windows)]
		}
		if i%3 == 0 {
			now = clock // Cancellation of a large origin must retain tiny windows.
		}
		left, right := persistenceDeadlineAt(clock, window), persistenceDeadlineAt(other, next)
		wantLeft, wantRight := referencePersistenceAt(clock, window), referencePersistenceAt(other, next)
		if got, want := left.compare(right), wantLeft.Cmp(wantRight); got != want {
			t.Fatalf("case%d compare clock=%d/%d window=%g/%g: %d != %d", i, clock, other, window, next, got, want)
		}
		sourceNumber := clock >= -(1<<53-1) && clock <= 1<<53-1 && now >= -(1<<53-1) && now <= 1<<53-1
		if got, want := left.delay(now), referencePersistenceDelay(wantLeft, now, sourceNumber); got != want {
			t.Fatalf("case%d delay clock=%d now=%d window=%g: %s != %s", i, clock, now, window, got, want)
		}
	}
}

func TestPersistenceDeadlineCrossRangeAndEquivalent(t *testing.T) {
	// Same absolute deadline from different full-width origins. Equality must
	// suppress rearming even when its timestamp is beyond the clock's range.
	left := persistenceDeadlineAt(math.MaxInt64-1, 1.25)
	right := persistenceDeadlineAt(math.MaxInt64, .25)
	if left.compare(right) != 0 || left.delay(math.MaxInt64) != 250*time.Microsecond {
		t.Fatal("equal deadline beyond MaxInt64 was rounded or overflowed")
	}
	cross := persistenceDeadlineAt(math.MinInt64, float64(uint64(1)<<63)*2)
	if cross.delay(math.MaxInt64) != time.Millisecond || cross.compare(right) != 1 {
		t.Fatal("full-width clock subtraction overflowed")
	}
	if left.delay(math.MinInt64) != time.Duration(math.MaxInt64) || left.delay(math.MaxInt64) < 0 {
		t.Fatal("duration saturation wrapped")
	}
}
