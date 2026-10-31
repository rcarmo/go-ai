package responsestream

import goai "github.com/rcarmo/go-ai"

// ServiceTierMultiplier implements the common flex/priority prices. Provider
// aliases (Responses fast) are resolved by the caller, not applied to Codex.
func ServiceTierMultiplier(modelID, tier string) float64 {
	switch tier {
	case "flex":
		return 0.5
	case "priority":
		if modelID == "gpt-5.5" {
			return 2.5
		}
		return 2
	default:
		return 1
	}
}

func ApplyServiceTierMultiplier(usage *goai.Usage, multiplier float64) {
	if usage == nil || multiplier == 1 {
		return
	}
	usage.Cost.Input *= multiplier
	usage.Cost.Output *= multiplier
	usage.Cost.CacheRead *= multiplier
	usage.Cost.CacheWrite *= multiplier
	usage.Cost.Total = usage.Cost.Input + usage.Cost.Output + usage.Cost.CacheRead + usage.Cost.CacheWrite
}
