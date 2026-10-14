package durable

import goai "github.com/rcarmo/go-ai"

func copyProtocolTools(tools []goai.Tool) []goai.Tool {
	out := make([]goai.Tool, len(tools))
	for i, tool := range tools {
		out[i] = tool
		out[i].Parameters = append([]byte(nil), tool.Parameters...)
	}
	return out
}

func toolCallerAllowed(offer toolOffer, caller string) bool {
	if offer.Callers == nil {
		return true
	}
	for _, value := range *offer.Callers {
		if value == caller {
			return true
		}
	}
	return false
}
func modelToolOffered(offer toolOffer, filter *ExtensionFilter) bool {
	if !toolCallerAllowed(offer, "model") {
		return false
	}
	if filter != nil {
		for _, name := range filter.Add {
			if name == offer.Name {
				return true
			}
		}
		for _, name := range filter.Remove {
			if name == offer.Name {
				return false
			}
		}
	}
	return !offer.Deferred
}
