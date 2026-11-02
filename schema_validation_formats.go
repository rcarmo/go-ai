package goai

// Format assertions apply only to strings. Unknown names are annotations, not
// failures. Keep this predicate non-coercing: it participates in whole-union
// membership as well as the final check, not in primitive conversion.
func schemaStringFormatReason(value interface{}, schema map[string]interface{}) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	name, _ := schema["format"].(string)
	pattern := schemaFormatPatterns[name]
	if pattern == nil || pattern.MatchString(schemaFormatASCIIToLower(text)) {
		return ""
	}
	return "expected format " + name
}

// JS /i without /u does not fold non-ASCII Kelvin sign or long s into ASCII.
// Only the comparison copy is folded; successful values retain their casing.
func schemaFormatASCIIToLower(value string) string {
	for i := 0; i < len(value); i++ {
		if value[i] >= 'A' && value[i] <= 'Z' {
			lower := []byte(value)
			for j := i; j < len(lower); j++ {
				if lower[j] >= 'A' && lower[j] <= 'Z' {
					lower[j] += 'a' - 'A'
				}
			}
			return string(lower)
		}
	}
	return value
}
