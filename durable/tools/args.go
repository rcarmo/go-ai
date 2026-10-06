package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

func intArgMinimum(args map[string]any, key string, def, minimum int) (int, error) {
	value, ok := args[key]
	if !ok || value == nil {
		return def, nil
	}
	switch v := value.(type) {
	case int:
		if v < minimum {
			return 0, fmt.Errorf("%s must be >= %d", key, minimum)
		}
		return v, nil
	case int64:
		if v < int64(minimum) || v > math.MaxInt {
			return 0, fmt.Errorf("%s must be >= %d", key, minimum)
		}
		return int(v), nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || v < float64(minimum) || math.Trunc(v) != v || v >= float64(math.MaxInt) {
			return 0, fmt.Errorf("%s must be an integer >= %d", key, minimum)
		}
		return int(v), nil
	case json.Number:
		i, err := v.Int64()
		if err != nil || i < int64(minimum) || i > math.MaxInt {
			return 0, fmt.Errorf("%s must be an integer >= %d", key, minimum)
		}
		return int(i), nil
	default:
		return 0, errors.New(key + " must be an integer")
	}
}
