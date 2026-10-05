package tools

import (
	"os"
	"regexp"
	"strings"
)

// These rules are the pinned tools/path-utils.ts transformations. NFD variants
// require a Unicode normaliser and are not approximated with an ad-hoc table.
func normalizeToolPath(path string) string {
	path = strings.Map(func(r rune) rune {
		if r == 0xa0 || r >= 0x2000 && r <= 0x200a || r == 0x202f || r == 0x205f || r == 0x3000 {
			return ' '
		}
		return r
	}, path)
	return strings.TrimPrefix(path, "@")
}
func (e *Env) resolveToolPath(path string) (string, error) { return e.resolve(normalizeToolPath(path)) }

var meridiemPath = regexp.MustCompile(`(?i) (AM|PM)\.`)

func (e *Env) resolveReadToolPath(path string) (string, error) {
	resolved, err := e.resolveToolPath(path)
	if err != nil {
		return "", err
	}
	meridiem := meridiemPath.ReplaceAllString(resolved, "\u202f$1.")
	// Preserve the pinned candidate order for implemented variants. A missing
	// NFD candidate remains explicit in the contract ledger.
	for _, variant := range []string{resolved, meridiem, strings.ReplaceAll(resolved, "'", "’")} {
		if _, err := os.Stat(variant); err == nil {
			return variant, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	return resolved, nil
}
