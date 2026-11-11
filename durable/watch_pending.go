package durable

import "encoding/json"

// coalesceWatchFrames runs off the Session line, before the next callback. The
// received frames/operations remain immutable until their consumed slots clear.
func coalesceWatchFrames(frames []rawWatchFrame, limits Limits, quota int64) (rawWatchFrame, error) {
	frame := frames[len(frames)-1]
	if len(frames) == 1 {
		return frame, nil
	}
	batches := make([][]Operation, len(frames))
	for i, pending := range frames {
		frame.Retired = frame.Retired || pending.Retired
		batches[i] = pending.Ops
	}
	ops, overflow, err := foldWatchOperations(batches, limits)
	if err != nil {
		return rawWatchFrame{}, err
	}
	// A single batch stays as-is; subsequent folded batches are kept replayable up
	// to the source's 10,000-op threshold. Once pending operations overflow, the
	// recovery view is a root replacement that resets the folded state.
	if !overflow {
		frame.Ops, err = ownOperations(ops, limits)
		if err == nil {
			var encoded []byte
			encoded, err = encodeBounded(JSON{"value": frame.Value, "ops": frame.Ops}, limits, limits.MaxFramePayloadBytes)
			if err == nil && int64(len(encoded)) <= quota {
				return frame, nil
			}
		}
	}
	frame.Ops, err = ownOperations([]Operation{{"r", mapOrNull(frame.Value)}}, limits)
	if err != nil {
		return rawWatchFrame{}, err
	}
	encoded, err := encodeBounded(JSON{"value": frame.Value, "ops": frame.Ops}, limits, limits.MaxFramePayloadBytes)
	if err != nil {
		return rawWatchFrame{}, err
	}
	if int64(len(encoded)) > quota {
		return rawWatchFrame{}, reject("watch folded root exceeds process quota")
	}
	return frame, nil
}

const maxPendingWatchOperations = 10_000

func watchFoldThreshold(ops []Operation, limits Limits) bool {
	if len(ops) > maxDeltaOperations || len(ops) > limits.MaxMembers {
		return true
	}
	return len(ops) > maxPendingWatchOperations
}

func watchPrimitive(v any) bool {
	switch v.(type) {
	case nil, bool, string, json.Number, float64, float32, int, int64, int32, uint, uint64, uint32:
		return true
	default:
		return false
	}
}

// foldWatchOperations shares Chord leaf/barrier rules between JSON watch kinds.
// It never modifies input operations or paths. Native replay limits still apply.
func foldWatchOperations(batches [][]Operation, limits Limits) ([]Operation, bool, error) {
	// Empty pushes do not turn one source batch into a folded queue.
	var single []Operation
	count := 0
	for _, batch := range batches {
		if len(batch) > 0 {
			single = batch
			count++
		}
	}
	if count <= 1 {
		return single, false, nil
	}
	var ops []Operation
	leaves := map[string]int{}
	overflow := false
	for _, incoming := range batches {
		if len(incoming) > 0 && incoming[0][0] == "r" {
			ops = nil
			clear(leaves)
			overflow = false
		}
		if overflow {
			continue
		}
		for _, op := range incoming {
			if op[0] == "r" {
				ops = nil
				clear(leaves)
			}
			leaf := op[0] == "a" || (op[0] == "s" && watchPrimitive(op[2]))
			if leaf {
				path, e := json.Marshal(op[1])
				if e != nil {
					return nil, false, e
				}
				key := string(path)
				if at, ok := leaves[key]; ok {
					previous := ops[at]
					if op[0] == "s" {
						ops[at] = op
						continue
					}
					if text, ok := previous[2].(string); ok {
						ops[at] = Operation{previous[0], previous[1], text + op[2].(string)}
						continue
					}
				}
				leaves[key] = len(ops)
			} else {
				clear(leaves)
			}
			ops = append(ops, op)
			// Source folding threshold and independent persisted replay limits.
			if watchFoldThreshold(ops, limits) {
				overflow = true
				ops = nil
				clear(leaves)
				break
			}
		}
	}
	return ops, overflow, nil
}
