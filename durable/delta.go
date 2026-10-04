package durable

import (
	"encoding/json"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Operation is a decoded tuple: r/s/d/a/t/p/m with inline string/numeric paths.
// Wire dictionaries and shortened tuples are deliberately not accepted. Inputs
// and every payload placement are copied; returned values have no input authority.
type Operation []any

const maxDeltaOperations = 4096

// ApplyOperations applies a complete batch to a detached strict-JSON candidate.
// Every intermediate revision obeys the supplied document/container budgets.
// Truncation counts UTF-16 units, rejecting cuts inside a surrogate pair because
// the native persisted JSON contract requires well-formed Unicode strings.
func ApplyOperations(value any, operations []Operation, limits Limits) (any, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	ops, err := ownOperations(operations, limits)
	if err != nil {
		return nil, err
	}
	root, err := ownJSONValue(value, limits)
	if err != nil {
		return nil, err
	}
	if err = checkDeltaValue(root, limits); err != nil {
		return nil, err
	}
	for _, op := range ops {
		root, err = applyOperation(root, op, limits)
		if err != nil {
			return nil, err
		}
		if err = checkDeltaValue(root, limits); err != nil {
			return nil, err
		}
	}
	return root, nil
}

func ownJSONValue(value any, l Limits) (any, error) {
	budget := jsonBudget{l: l, active: map[uintptr]bool{}}
	if err := budget.walk(reflect.ValueOf(value), 1, true); err != nil {
		return nil, err
	}
	if _, err := encodeBounded(value, l, l.MaxRecordBytes); err != nil {
		return nil, err
	}
	// Allocate only after the bounded strict walk. Containers shared by a caller
	// are copied independently at each placement; the walk already rejects cycles.
	return cloneJSONValue(reflect.ValueOf(value)), nil
}
func cloneJSONValue(v reflect.Value) any {
	if !v.IsValid() {
		return nil
	}
	for v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Type() == reflect.TypeOf(json.Number("")) {
		return v.Interface().(json.Number)
	}
	switch v.Kind() {
	case reflect.Bool:
		return v.Bool()
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return json.Number(strconv.FormatInt(v.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return json.Number(strconv.FormatUint(v.Uint(), 10))
	case reflect.Float32, reflect.Float64:
		return json.Number(strconv.FormatFloat(v.Float(), 'g', -1, v.Type().Bits()))
	case reflect.Map:
		if v.IsNil() {
			return nil
		}
		n := map[string]any{}
		it := v.MapRange()
		for it.Next() {
			n[it.Key().String()] = cloneJSONValue(it.Value())
		}
		return n
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil
		}
		n := make([]any, v.Len())
		for i := range n {
			n[i] = cloneJSONValue(v.Index(i))
		}
		return n
	}
	panic("validated JSON value has unsupported kind")
}
func checkDeltaValue(v any, l Limits) error {
	_, err := encodeBounded(v, l, l.MaxDocumentBytes)
	return err
}

func ownOperations(operations []Operation, l Limits) ([]Operation, error) {
	if len(operations) > maxDeltaOperations || len(operations) > l.MaxMembers {
		return nil, reject("delta operation count limit")
	}
	if _, err := encodeBounded(operations, l, l.MaxRecordBytes); err != nil {
		return nil, err
	}
	owned := make([]Operation, len(operations))
	for i, op := range operations {
		v, err := ownJSONValue(op, l)
		if err != nil {
			return nil, err
		}
		tuple, ok := v.([]any)
		if !ok {
			return nil, reject("operation is not a tuple")
		}
		owned[i] = Operation(tuple)
		if err = validOperation(owned[i], l); err != nil {
			return nil, err
		}
	}
	return owned, nil
}
func validOperation(op Operation, l Limits) error {
	if len(op) == 0 {
		return reject("operation is empty")
	}
	verb, ok := op[0].(string)
	if !ok {
		return reject("operation verb is not a string")
	}
	arity := map[string]int{"r": 2, "s": 3, "d": 2, "a": 3, "t": 3, "p": 5, "m": 3}[verb]
	if arity == 0 || len(op) != arity {
		return reject("unknown verb or operation arity")
	}
	if verb == "r" {
		return nil
	}
	path, ok := op[1].([]any)
	if !ok || len(path) > l.MaxDepth || (len(path) == 0 && verb != "p" && verb != "m") {
		return reject("invalid decoded operation path")
	}
	for _, segment := range path {
		if key, ok := segment.(string); ok {
			if reservedSegment(key) {
				return reject("unsafe operation path")
			}
		} else if _, err := deltaIndex(segment); err != nil {
			return err
		}
	}
	switch verb {
	case "a":
		if _, ok := op[2].(string); !ok {
			return reject("append payload is not a string")
		}
	case "t":
		_, err := deltaIndex(op[2])
		return err
	case "p":
		if _, err := deltaIndex(op[2]); err != nil {
			return err
		}
		if _, err := deltaIndex(op[3]); err != nil {
			return err
		}
		if _, ok := op[4].([]any); !ok {
			return reject("splice payload is not an array")
		}
	case "m":
		order, ok := op[2].([]any)
		if !ok {
			return reject("permutation is not an array")
		}
		seen := make([]bool, len(order))
		for _, v := range order {
			n, err := deltaIndex(v)
			if err != nil || n >= uint64(len(order)) || seen[n] {
				return reject("permutation is not a bijection")
			}
			seen[n] = true
		}
	}
	return nil
}
func reservedSegment(s string) bool {
	return s == "__proto__" || s == "constructor" || s == "prototype"
}
func deltaIndex(value any) (uint64, error) {
	n, ok := value.(json.Number)
	if !ok {
		return 0, reject("operation index must be numeric")
	}
	negative, digits, scale := numberParts(string(n))
	if digits == "0" {
		return 0, nil
	}
	if negative || scale.Sign() < 0 || !scale.IsInt64() || scale.Int64() > 16 || int64(len(digits))+scale.Int64() > 16 {
		return 0, reject("operation index outside exact integer bounds")
	}
	value64, err := strconv.ParseUint(digits+strings.Repeat("0", int(scale.Int64())), 10, 64)
	if err != nil || value64 > MaxID {
		return 0, reject("operation index outside exact integer bounds")
	}
	return value64, nil
}
func segmentKey(segment any) string {
	if s, ok := segment.(string); ok {
		return s
	}
	n, _ := deltaIndex(segment)
	return strconv.FormatUint(n, 10)
}

// mutatePath returns a possibly resized array to its parent. All containers
// belong exclusively to this call, so no base or caller container is mutated.
func mutatePath(value any, path []any, fn func(any) (any, error)) (any, error) {
	if len(path) == 0 {
		return fn(value)
	}
	switch parent := value.(type) {
	case map[string]any:
		key := segmentKey(path[0])
		child, exists := parent[key]
		if !exists {
			return nil, reject("unresolvable operation path")
		}
		next, err := mutatePath(child, path[1:], fn)
		if err != nil {
			return nil, err
		}
		parent[key] = next
		return parent, nil
	case []any:
		index, err := deltaIndex(path[0])
		if err != nil || index >= uint64(len(parent)) {
			return nil, reject("unresolvable array path")
		}
		next, err := mutatePath(parent[index], path[1:], fn)
		if err != nil {
			return nil, err
		}
		parent[index] = next
		return parent, nil
	default:
		return nil, reject("operation path crosses a scalar")
	}
}
func applyOperation(root any, op Operation, l Limits) (any, error) {
	verb := op[0].(string)
	if verb == "r" {
		return ownJSONValue(op[1], l)
	}
	path := op[1].([]any)
	if verb == "p" || verb == "m" {
		return mutatePath(root, path, func(v any) (any, error) {
			xs, ok := v.([]any)
			if !ok {
				return nil, reject("array operation targets a non-array")
			}
			if verb == "m" {
				order := op[2].([]any)
				if len(order) != len(xs) {
					return nil, reject("permutation length mismatch")
				}
				next := make([]any, len(xs))
				for i, n := range order {
					index, _ := deltaIndex(n)
					next[i] = xs[index]
				}
				return next, nil
			}
			start, _ := deltaIndex(op[2])
			remove, _ := deltaIndex(op[3])
			if start > uint64(len(xs)) {
				start = uint64(len(xs))
			}
			if remove > uint64(len(xs))-start {
				remove = uint64(len(xs)) - start
			}
			items := op[4].([]any)
			length := len(xs) - int(remove)
			if len(items) > l.MaxMembers-length {
				return nil, reject("splice container growth limit")
			}
			next := make([]any, 0, length+len(items))
			next = append(next, xs[:start]...)
			for _, item := range items {
				owned, err := ownJSONValue(item, l)
				if err != nil {
					return nil, err
				}
				next = append(next, owned)
			}
			next = append(next, xs[start+remove:]...)
			return next, nil
		})
	}
	key := path[len(path)-1]
	return mutatePath(root, path[:len(path)-1], func(parent any) (any, error) {
		var current any
		var exists bool
		var index uint64
		switch p := parent.(type) {
		case map[string]any:
			current, exists = p[segmentKey(key)]
		case []any:
			var err error
			index, err = deltaIndex(key)
			if err != nil || index > uint64(len(p)) {
				return nil, reject("unsafe array index")
			}
			exists = index < uint64(len(p))
			if exists {
				current = p[index]
			}
		default:
			return nil, reject("operation parent is a scalar")
		}
		if verb == "d" {
			if p, ok := parent.(map[string]any); ok {
				delete(p, segmentKey(key))
				return p, nil
			}
			if !exists {
				return nil, reject("array deletion past the end")
			}
			p := parent.([]any)
			return append(p[:index], p[index+1:]...), nil
		}
		var next any
		switch verb {
		case "s":
			var err error
			next, err = ownJSONValue(op[2], l)
			if err != nil {
				return nil, err
			}
		case "a", "t":
			text, ok := current.(string)
			if !exists || !ok {
				return nil, reject("string operation targets a non-string")
			}
			if verb == "a" {
				addition := op[2].(string)
				if len(addition) > l.MaxStringBytes-len(text) {
					return nil, reject("append string growth limit")
				}
				next = text + addition
			} else {
				count, _ := deltaIndex(op[2])
				var err error
				next, err = truncateUTF16(text, count)
				if err != nil {
					return nil, err
				}
			}
		}
		if p, ok := parent.(map[string]any); ok {
			if !exists && len(p) >= l.MaxMembers {
				return nil, reject("object growth limit")
			}
			p[segmentKey(key)] = next
			return p, nil
		}
		p := parent.([]any)
		if index == uint64(len(p)) {
			if len(p) >= l.MaxMembers {
				return nil, reject("array growth limit")
			}
			return append(p, next), nil
		}
		p[index] = next
		return p, nil
	})
}
func truncateUTF16(s string, count uint64) (string, error) {
	var units uint64
	for offset, r := range s {
		if units == count {
			return s[offset:], nil
		}
		width := uint64(1)
		if r > 0xffff {
			width = 2
		}
		if count > units && count < units+width {
			return "", reject("UTF-16 truncation splits a surrogate pair")
		}
		units += width
	}
	return "", nil
}

func equalDeltaJSON(a, b any) bool {
	switch left := a.(type) {
	case nil:
		return b == nil
	case bool:
		right, ok := b.(bool)
		return ok && left == right
	case string:
		right, ok := b.(string)
		return ok && left == right
	case json.Number:
		right, ok := b.(json.Number)
		if !ok {
			return false
		}
		xn, xd, xe := numberParts(string(left))
		yn, yd, ye := numberParts(string(right))
		return xn == yn && xd == yd && xe.Cmp(ye) == 0
	case []any:
		right, ok := b.([]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for i := range left {
			if !equalDeltaJSON(left[i], right[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		right, ok := b.(map[string]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for key, value := range left {
			other, exists := right[key]
			if !exists || !equalDeltaJSON(value, other) {
				return false
			}
		}
		return true
	}
	return false
}

// diffOperations maps detached native Update revisions to replayable operations.
// Tuple shape is noncanonical. Reserved-key edits fold to their safe ancestor;
// bounded operation overflow folds to a root replacement, never a hash test.
func diffOperations(before, after any, l Limits) ([]Operation, error) {
	a, err := ownJSONValue(before, l)
	if err != nil {
		return nil, err
	}
	b, err := ownJSONValue(after, l)
	if err != nil {
		return nil, err
	}
	if err = checkDeltaValue(b, l); err != nil {
		return nil, err
	}
	limit := l.MaxMembers
	if limit > maxDeltaOperations {
		limit = maxDeltaOperations
	}
	ops := []Operation{}
	var visit func(any, any, []any)
	emitSet := func(path []any, value any) {
		if len(path) == 0 {
			ops = append(ops, Operation{"r", value})
		} else {
			ops = append(ops, Operation{"s", path, value})
		}
	}
	visit = func(x, y any, path []any) {
		if len(ops) > limit || equalDeltaJSON(x, y) {
			return
		}
		childPath := func(key any) []any { return append(append([]any{}, path...), key) }
		if left, ok := x.(map[string]any); ok {
			if right, ok := y.(map[string]any); ok {
				for key := range left {
					if reservedSegment(key) {
						emitSet(path, y)
						return
					}
				}
				for key := range right {
					if reservedSegment(key) {
						emitSet(path, y)
						return
					}
				}
				keys := make([]string, 0, len(right))
				for key := range right {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					if old, exists := left[key]; exists {
						visit(old, right[key], childPath(key))
					} else {
						emitSet(childPath(key), right[key])
					}
					if len(ops) > limit {
						return
					}
				}
				keys = keys[:0]
				for key := range left {
					if _, exists := right[key]; !exists {
						keys = append(keys, key)
					}
				}
				sort.Strings(keys)
				for _, key := range keys {
					ops = append(ops, Operation{"d", childPath(key)})
					if len(ops) > limit {
						return
					}
				}
				return
			}
		}
		if left, ok := x.([]any); ok {
			if right, ok := y.([]any); ok {
				start := 0
				for start < len(left) && start < len(right) && equalDeltaJSON(left[start], right[start]) {
					start++
				}
				endLeft, endRight := len(left), len(right)
				for endLeft > start && endRight > start && equalDeltaJSON(left[endLeft-1], right[endRight-1]) {
					endLeft--
					endRight--
				}
				ops = append(ops, Operation{"p", path, start, endLeft - start, right[start:endRight]})
				return
			}
		}
		if left, ok := x.(string); ok && len(path) > 0 {
			if right, ok := y.(string); ok {
				if len(right) > len(left) && right[:len(left)] == left {
					ops = append(ops, Operation{"a", path, right[len(left):]})
					return
				}
				// Bounded rolling-window suffix search. Only valid UTF-8 boundaries
				// can emit t; unit counts explicitly differ from byte offsets.
				probed := 0
				for offset := range left {
					if offset == 0 || left[offset]&0xc0 == 0x80 {
						continue
					}
					if len(left)-offset > len(right) || len(left)-offset > 65536 {
						continue
					}
					probed++
					if probed > 8 {
						break
					}
					suffix := left[offset:]
					if right[:len(suffix)] == suffix {
						units := 0
						for _, r := range left[:offset] {
							units++
							if r > 0xffff {
								units++
							}
						}
						ops = append(ops, Operation{"t", path, units})
						if len(right) > len(suffix) {
							ops = append(ops, Operation{"a", path, right[len(suffix):]})
						}
						return
					}
				}
			}
		}
		emitSet(path, y)
	}
	visit(a, b, []any{})
	if len(ops) > limit {
		ops = []Operation{{"r", b}}
	}
	owned, err := ownOperations(ops, l)
	if err != nil && len(ops) > 1 {
		return ownOperations([]Operation{{"r", b}}, l)
	}
	return owned, err
}

// numberParts normalises a validated decimal token without expanding its
// exponent. Work and memory stay proportional to the bounded spelling, even for
// 1e999999999. Numeric equality is exact and independent of JSON spelling.
func numberParts(s string) (negative bool, digits string, scale *big.Int) {
	negative = strings.HasPrefix(s, "-")
	if negative {
		s = s[1:]
	}
	scale = new(big.Int)
	if at := strings.IndexAny(s, "eE"); at >= 0 {
		scale.SetString(strings.TrimPrefix(s[at+1:], "+"), 10)
		s = s[:at]
	}
	if at := strings.IndexByte(s, '.'); at >= 0 {
		scale.Sub(scale, big.NewInt(int64(len(s)-at-1)))
		s = s[:at] + s[at+1:]
	}
	s = strings.TrimLeft(s, "0")
	if s == "" {
		return false, "0", new(big.Int)
	}
	digits = strings.TrimRight(s, "0")
	scale.Add(scale, big.NewInt(int64(len(s)-len(digits))))
	return negative, digits, scale
}
