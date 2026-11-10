package durable

// applyImmutableValue ports Chord's applyImmutable path-copy rule: changed
// containers are new nodes; untouched branches and retained array elements keep
// their identity. Every previous revision remains read-only and valid.
func applyImmutableValue(value *ImmutableValue, operations []Operation, limits Limits) (*ImmutableValue, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	ops, err := ownOperations(operations, limits)
	if err != nil {
		return nil, err
	}
	if err := checkDeltaValue(value.Copy(), limits); err != nil {
		return nil, err
	}
	root := value
	for _, op := range ops {
		verb := op[0].(string)
		if verb == "r" {
			root, err = freezeImmutableValue(op[1])
		} else {
			path := op[1].([]any)
			target := path
			if verb != "p" && verb != "m" {
				target = path[:len(path)-1]
			}
			root, err = immutableUpdatePath(root, target, func(parent *ImmutableValue) (*ImmutableValue, error) {
				return immutableOperation(parent, path, op, limits)
			})
		}
		if err != nil {
			return nil, err
		}
		if err = checkDeltaValue(root.Copy(), limits); err != nil {
			return nil, err
		}
	}
	return root, nil
}

func immutableUpdatePath(value *ImmutableValue, path []any, update func(*ImmutableValue) (*ImmutableValue, error)) (*ImmutableValue, error) {
	if len(path) == 0 {
		return update(value)
	}
	if value == nil {
		return nil, reject("operation path crosses a scalar")
	}
	switch value.kind {
	case "object":
		key := segmentKey(path[0])
		child, ok := value.mapValue[key]
		if !ok {
			return nil, reject("unresolvable operation path")
		}
		next, err := immutableUpdatePath(child, path[1:], update)
		if err != nil {
			return nil, err
		}
		copy := immutableContainerCopy(value)
		copy.mapValue[key] = next
		return copy, nil
	case "array":
		index, err := deltaIndex(path[0])
		if err != nil || index >= uint64(len(value.arrayValue)) {
			return nil, reject("unresolvable array path")
		}
		next, err := immutableUpdatePath(value.arrayValue[index], path[1:], update)
		if err != nil {
			return nil, err
		}
		copy := immutableContainerCopy(value)
		copy.arrayValue[index] = next
		return copy, nil
	default:
		return nil, reject("operation path crosses a scalar")
	}
}

func immutableContainerCopy(value *ImmutableValue) *ImmutableValue {
	copy := *value
	switch value.kind {
	case "object":
		copy.mapValue = make(map[string]*ImmutableValue, len(value.mapValue))
		for key, child := range value.mapValue {
			copy.mapValue[key] = child
		}
	case "array":
		copy.arrayValue = append([]*ImmutableValue{}, value.arrayValue...)
	}
	return &copy
}

func immutableOperation(parent *ImmutableValue, path []any, op Operation, limits Limits) (*ImmutableValue, error) {
	verb := op[0].(string)
	if verb == "p" || verb == "m" {
		if !parent.IsArray() {
			return nil, reject("array operation targets a non-array")
		}
		items := parent.arrayValue
		if verb == "m" {
			order := op[2].([]any)
			if len(order) != len(items) {
				return nil, reject("permutation length mismatch")
			}
			next := make([]*ImmutableValue, len(items))
			for i, n := range order {
				index, _ := deltaIndex(n)
				next[i] = items[index]
			}
			return &ImmutableValue{kind: "array", arrayValue: next}, nil
		}
		start, _ := deltaIndex(op[2])
		remove, _ := deltaIndex(op[3])
		if start > uint64(len(items)) {
			start = uint64(len(items))
		}
		if remove > uint64(len(items))-start {
			remove = uint64(len(items)) - start
		}
		inserted := op[4].([]any)
		length := len(items) - int(remove)
		if len(inserted) > limits.MaxMembers-length {
			return nil, reject("splice container growth limit")
		}
		next := make([]*ImmutableValue, 0, length+len(inserted))
		next = append(next, items[:start]...)
		for _, item := range inserted {
			node, err := freezeImmutableValue(item)
			if err != nil {
				return nil, err
			}
			next = append(next, node)
		}
		next = append(next, items[start+remove:]...)
		return &ImmutableValue{kind: "array", arrayValue: next}, nil
	}
	if parent == nil || (!parent.IsObject() && !parent.IsArray()) {
		return nil, reject("operation parent is a scalar")
	}
	key := path[len(path)-1]
	var current *ImmutableValue
	var exists bool
	var index uint64
	if parent.IsObject() {
		current, exists = parent.mapValue[segmentKey(key)]
	} else {
		var err error
		index, err = deltaIndex(key)
		if err != nil || index > uint64(len(parent.arrayValue)) {
			return nil, reject("unsafe array index")
		}
		exists = index < uint64(len(parent.arrayValue))
		if exists {
			current = parent.arrayValue[index]
		}
	}
	copy := immutableContainerCopy(parent)
	if verb == "d" {
		if parent.IsObject() {
			delete(copy.mapValue, segmentKey(key))
			return copy, nil
		}
		if !exists {
			return nil, reject("array deletion past the end")
		}
		copy.arrayValue = append(copy.arrayValue[:index], copy.arrayValue[index+1:]...)
		return copy, nil
	}
	var next *ImmutableValue
	var err error
	switch verb {
	case "s":
		next, err = freezeImmutableValue(op[2])
	case "a", "t":
		if !exists || current == nil || current.kind != "string" {
			return nil, reject("string operation targets a non-string")
		}
		text := current.string
		if verb == "a" {
			addition := op[2].(string)
			if len(addition) > limits.MaxStringBytes-len(text) {
				return nil, reject("append string growth limit")
			}
			text += addition
		} else {
			count, _ := deltaIndex(op[2])
			text, err = truncateUTF16(text, count)
		}
		next = &ImmutableValue{kind: "string", string: text}
	}
	if err != nil {
		return nil, err
	}
	if parent.IsObject() {
		if !exists && len(copy.mapValue) >= limits.MaxMembers {
			return nil, reject("object growth limit")
		}
		copy.mapValue[segmentKey(key)] = next
		return copy, nil
	}
	if index == uint64(len(copy.arrayValue)) {
		if len(copy.arrayValue) >= limits.MaxMembers {
			return nil, reject("array growth limit")
		}
		copy.arrayValue = append(copy.arrayValue, next)
	} else {
		copy.arrayValue[index] = next
	}
	return copy, nil
}
