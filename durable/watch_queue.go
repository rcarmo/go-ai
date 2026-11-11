package durable

// Fold before replacing for byte pressure: queued frames duplicate old values,
// whereas the source keeps just the latest value and its folded pending ops.
// Publication readiness is retained even when several frames become one.
func compactPendingWatchFrames(frames []rawWatchFrame, limits Limits, quota int64) (rawWatchFrame, int64, error) {
	gates := []<-chan struct{}{}
	seen := make(map[<-chan struct{}]bool)
	add := func(gate <-chan struct{}) error {
		if gate == nil || seen[gate] {
			return nil
		}
		select {
		case <-gate:
			return nil
		default:
		}
		if len(gates) >= limits.MaxMembers {
			return reject("watch readiness count limit")
		}
		seen[gate] = true
		gates = append(gates, gate)
		return nil
	}
	for _, frame := range frames {
		if err := add(frame.ready); err != nil {
			return rawWatchFrame{}, 0, err
		}
		for _, gate := range frame.pendingReady {
			if err := add(gate); err != nil {
				return rawWatchFrame{}, 0, err
			}
		}
	}
	folded, err := coalesceWatchFrames(frames, limits, quota)
	if err != nil {
		return rawWatchFrame{}, 0, err
	}
	encoded, err := encodeBounded(JSON{"value": folded.Value, "ops": folded.Ops}, limits, limits.MaxFramePayloadBytes)
	if err != nil {
		return rawWatchFrame{}, 0, err
	}
	if int64(len(encoded)) > quota {
		return rawWatchFrame{}, 0, reject("watch folded frame exceeds process quota")
	}
	folded.ready = nil
	folded.pendingReady = gates
	return folded, int64(len(encoded)), nil
}
