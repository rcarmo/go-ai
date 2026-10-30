package responsestream

import (
	"github.com/rcarmo/go-ai/internal/jsonparse"
	"strconv"
	"strings"
	"unicode/utf16"
)

// ToolLoadID uses the pinned Responses protocol hash, not the native SDK's
// existing hash (whose other identity contracts must stay stable).
func ToolLoadID(marker string, names []string) string {
	text := marker + ":" + strings.Join(names, ",")
	units, err := jsonparse.StringCodeUnits(text)
	if err != nil {
		units = utf16.Encode([]rune(text))
	}
	h1, h2 := uint32(0xdeadbeef), uint32(0x41c6ce57)
	for _, unit := range units {
		h1 = (h1 ^ uint32(unit)) * 2654435761
		h2 = (h2 ^ uint32(unit)) * 1597334677
	}
	h1 = (h1^(h1>>16))*2246822507 ^ (h2^(h2>>13))*3266489909
	h2 = (h2^(h2>>16))*2246822507 ^ (h1^(h1>>13))*3266489909
	return "pi_tool_load_" + strconv.FormatUint(uint64(h2), 36) + strconv.FormatUint(uint64(h1), 36)
}
