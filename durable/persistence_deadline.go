package durable

import (
	"math"
	"math/big"
	"time"
)

// Keep Source Number addition for clocks in its safe-integer domain. Native
// clocks outside that domain retain their int64 origin separately, so casting
// an absolute timestamp cannot erase a small persistence window. Neither an
// int64 timestamp addition nor an unchecked duration conversion is required.
// A deadline may exceed the clock's range; +Inf remains the no-timer sentinel.
type persistenceDeadline struct {
	clock  int64
	within float64
}

func persistenceDeadlineAt(clock int64, within float64) persistenceDeadline {
	const maxSafeInteger = 1<<53 - 1
	if clock >= -maxSafeInteger && clock <= maxSafeInteger {
		return persistenceDeadline{within: float64(clock) + within}
	}
	return persistenceDeadline{clock: clock, within: within}
}

func (d persistenceDeadline) infinite() bool { return math.IsInf(d.within, 1) }

func (d persistenceDeadline) rational(out *big.Rat) *big.Rat {
	out.SetFloat64(d.within) // Finite internal deadlines only; windows are validated.
	var origin big.Rat
	return out.Add(out, origin.SetInt64(d.clock))
}

func (d persistenceDeadline) compare(other persistenceDeadline) int {
	if d.infinite() {
		if other.infinite() {
			return 0
		}
		return 1
	}
	if other.infinite() {
		return -1
	}
	if d.clock == other.clock {
		if d.within == other.within {
			return 0
		}
		if d.within < other.within {
			return -1
		}
		return 1
	}
	var left, right big.Rat
	return d.rational(&left).Cmp(other.rational(&right))
}

func (d persistenceDeadline) delay(now int64) time.Duration {
	if d.infinite() {
		return time.Duration(math.MaxInt64)
	}
	var remaining, origin, scale big.Rat
	const maxSafeInteger = 1<<53 - 1
	if d.clock == 0 && now >= -maxSafeInteger && now <= maxSafeInteger {
		// Preserve Source Number subtraction too, before crossing the native
		// nanosecond boundary. d.within is its captured absolute deadline.
		remaining.SetFloat64(d.within - float64(now))
	} else {
		d.rational(&remaining)
		remaining.Sub(&remaining, origin.SetInt64(now))
	}
	if remaining.Sign() <= 0 {
		return 0
	}
	remaining.Mul(&remaining, scale.SetInt64(int64(time.Millisecond)))
	// Nanosecond truncation matches the native duration boundary. Work on the
	// exact binary Number value, not an overflowing/rounded float64 product.
	var nanos big.Int
	nanos.Quo(remaining.Num(), remaining.Denom())
	if !nanos.IsInt64() {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(nanos.Int64())
}
