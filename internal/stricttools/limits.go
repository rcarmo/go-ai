package stricttools

import "strings"

// LimitMessage matches the pinned provider messages eligible for a preferred
// strict-tool downgrade. Other validation errors must retain their failure.
func LimitMessage(message string) bool {
	for _, phrase := range []string{"The compiled grammar is too large", "Too many strict tools (", "Schemas contains too many parameters with union types ("} {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}
