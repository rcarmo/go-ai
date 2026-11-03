package durable

import goai "github.com/rcarmo/go-ai"

// Carriers are immutable inside candidates but detached at public boundaries.
// Keep the optional pointer so an empty object survives as a present carrier.
func copyTelemetryCarrier(source *goai.TelemetryCarrier) *goai.TelemetryCarrier {
	if source == nil {
		return nil
	}
	var owned goai.TelemetryCarrier
	if *source != nil {
		owned = make(goai.TelemetryCarrier, len(*source))
		for key, value := range *source {
			owned[key] = value
		}
	}
	return &owned
}

func submissionTelemetryCarrier(ctxTrace *submitTrace) *goai.TelemetryCarrier {
	if ctxTrace == nil {
		return nil
	}
	return copyTelemetryCarrier(ctxTrace.carrier)
}
