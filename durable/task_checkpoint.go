package durable

// taskCheckpointMap returns the object-backed checkpoint map for phase-aware
// state access while leaving primitive or opaque checkpoint values untouched.
func taskCheckpointMap(value any) JSON {
	switch v := value.(type) {
	case JSON:
		return v
	case map[string]any:
		return JSON(v)
	default:
		return nil
	}
}

// copyTaskCheckpointValue preserves arbitrary checkpoint values by cloning the
// object form when present, otherwise taking the original value through the JSON
// ownership validation path.
func copyTaskCheckpointValue(value any, l Limits) (any, error) {
	if value == nil {
		return nil, nil
	}
	if checkpoint := taskCheckpointMap(value); checkpoint != nil {
		return copyObject(checkpoint, l)
	}
	return ownJSONValue(value, l)
}
