package durable

// This budget covers keys/literal payloads that are not reference nodes.
func codemodeSchemaInputBudget(value any, depth int, nodes, bytes *int) bool {
	*nodes--
	if *nodes < 0 || depth > 32 {
		return false
	}
	switch v := value.(type) {
	case string:
		*bytes -= len(v)
	case JSON:
		return codemodeSchemaInputMap(map[string]any(v), depth, nodes, bytes)
	case map[string]any:
		return codemodeSchemaInputMap(v, depth, nodes, bytes)
	case []any:
		if len(v) > *nodes {
			return false
		}
		for _, child := range v {
			if !codemodeSchemaInputBudget(child, depth+1, nodes, bytes) {
				return false
			}
		}
	}
	return *bytes >= 0
}
func codemodeSchemaInputMap(value map[string]any, depth int, nodes, bytes *int) bool {
	if len(value) > *nodes {
		return false
	}
	for key, child := range value {
		*bytes -= len(key)
		if *bytes < 0 || !codemodeSchemaInputBudget(child, depth+1, nodes, bytes) {
			return false
		}
	}
	return true
}
