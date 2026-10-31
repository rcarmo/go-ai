package mistral

import (
	"strconv"
	"strings"
	"unicode/utf16"
)

// The maps belong to one request, as in createMistralToolCallIdNormalizer.
func newMistralToolCallIDNormalizer() func(string) string {
	forward, reverse := make(map[string]string), make(map[string]string)
	return func(id string) string {
		if candidate, ok := forward[id]; ok {
			return candidate
		}
		for attempt := 0; ; attempt++ {
			candidate := deriveMistralToolCallID(id, attempt)
			owner, claimed := reverse[candidate]
			if !claimed || owner == "" || owner == id {
				forward[id] = candidate
				reverse[candidate] = id
				return candidate
			}
		}
	}
}

func deriveMistralToolCallID(id string, attempt int) string {
	var clean strings.Builder
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			clean.WriteRune(r)
		}
	}
	normalized := clean.String()
	if attempt == 0 && len(normalized) == 9 {
		return normalized
	}
	seed := normalized
	if seed == "" {
		seed = id
	}
	if attempt != 0 {
		seed += ":" + strconv.Itoa(attempt)
	}
	hash := mistralToolIDHash(seed)
	if len(hash) > 9 {
		hash = hash[:9]
	}
	return hash
}

// Source shortHash uses UTF-16 code units and wrapping 32-bit arithmetic.
// Keep the public native ShortHash (used outside Mistral) unchanged.
func mistralToolIDHash(value string) string {
	h1, h2 := uint32(0xdeadbeef), uint32(0x41c6ce57)
	for _, unit := range utf16.Encode([]rune(value)) {
		h1 = (h1 ^ uint32(unit)) * 2654435761
		h2 = (h2 ^ uint32(unit)) * 1597334677
	}
	h1 = (h1^(h1>>16))*2246822507 ^ (h2^(h2>>13))*3266489909
	h2 = (h2^(h2>>16))*2246822507 ^ (h1^(h1>>13))*3266489909
	return strconv.FormatUint(uint64(h2), 36) + strconv.FormatUint(uint64(h1), 36)
}
