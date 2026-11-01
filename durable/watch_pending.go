package durable

import "encoding/json"

// coalesceWatchFrames runs off the Session line, before the next callback. The
// received frames/operations remain immutable until their consumed slots clear.
func coalesceWatchFrames(frames []WatchFrame, limits Limits, quota int64) (WatchFrame, error) {
	frame := frames[len(frames)-1]
	if len(frames) == 1 {
		return frame, nil
	}
	var ops []Operation
	leaves := map[string]int{}
	overflow := false
	for _, pending := range frames {
		frame.Retired = frame.Retired || pending.Retired
		incoming := pending.Ops
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
					return WatchFrame{}, e
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
			// Keep the native delta replay/DTO limit. Source permits 10,000 pending
			// ops; native frames must also remain valid ApplyOperations inputs.
			if len(ops) > maxDeltaOperations || len(ops) > limits.MaxMembers {
				overflow = true
				ops = nil
				clear(leaves)
				break
			}
		}
	}
	// Folding can concatenate strings and grow a batch: revalidate the detached
	// callback payload and process quota. A bounded root is the recovery view.
	var err error
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
		return WatchFrame{}, err
	}
	encoded, err := encodeBounded(JSON{"value": frame.Value, "ops": frame.Ops}, limits, limits.MaxFramePayloadBytes)
	if err != nil {
		return WatchFrame{}, err
	}
	if int64(len(encoded)) > quota {
		return WatchFrame{}, reject("watch folded root exceeds process quota")
	}
	return frame, nil
}

func watchPrimitive(v any) bool {
	switch v.(type) {
	case nil, bool, string, json.Number, float64, float32, int, int64, int32, uint, uint64, uint32:
		return true
	default:
		return false
	}
}
