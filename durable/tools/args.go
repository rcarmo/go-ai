package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

func intArg(args map[string]any, key string, def int) (int, error) {
	value, ok := args[key]
	if !ok || value == nil {
		return def, nil
	}
	switch v := value.(type) {
	case int:
		if v < 1 {
			return 0, fmt.Errorf("%s must be >= 1", key)
		}
		return v, nil
	case int64:
		if v < 1 || v > math.MaxInt {
			return 0, fmt.Errorf("%s must be >= 1", key)
		}
		return int(v), nil
	case float64:
		if v < 1 || math.Trunc(v) != v || v > math.MaxInt {
			return 0, fmt.Errorf("%s must be an integer >= 1", key)
		}
		return int(v), nil
	case json.Number:
		i, err := v.Int64()
		if err != nil || i < 1 || i > math.MaxInt {
			return 0, fmt.Errorf("%s must be an integer >= 1", key)
		}
		return int(i), nil
	default:
		return 0, errors.New(key + " must be an integer")
	}
}
