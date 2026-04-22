// Hash utilities.
package goai

import (
	"crypto/sha256"
	"encoding/hex"
)

// ShortHash returns a short deterministic hash of a string.
// Used for normalizing long IDs (e.g., OpenAI Responses tool call IDs).
func ShortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8]) // 16 hex chars
}
