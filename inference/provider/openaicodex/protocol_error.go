package openaicodex

import (
	"errors"
	goai "github.com/rcarmo/go-ai"
	"time"
)

func codexStreamErrorEvent(err error) *goai.ErrorEvent {
	event := &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
	var protocol *codexProtocolError
	var observer *codexObserverError
	var nullEvent *codexNullEventError
	var api *codexAPIError
	var partial *goai.Message
	if errors.As(err, &protocol) {
		partial = protocol.partial
	} else if errors.As(err, &observer) {
		partial = observer.partial
	} else if errors.As(err, &nullEvent) {
		partial = nullEvent.partial
	} else if errors.As(err, &api) {
		partial = api.partial
	}
	if partial != nil {
		partial.StopReason = goai.StopReasonError
		partial.ErrorMessage = err.Error()
		partial.Timestamp = time.Now().UnixMilli()
		event.Error = partial
	}
	return event
}

func recordCodexStartedTransportFailure(opts *goai.StreamOptions, partial *goai.Message, requestBytes int, err error) {
	configured := goai.TransportAuto
	session := ""
	if opts != nil {
		session = opts.SessionID
		if opts.Transport != "" {
			configured = opts.Transport
		}
	}
	partial.StopReason = goai.StopReasonError
	partial.ErrorMessage = err.Error()
	partial.Timestamp = time.Now().UnixMilli()
	goai.AppendAssistantMessageDiagnostic(partial, goai.CreateAssistantMessageDiagnostic("provider_transport_failure", err, map[string]any{"configuredTransport": string(configured), "eventsEmitted": true, "phase": "after_message_stream_start", "requestBytes": requestBytes}))
	recordCodexWebSocketFailure(session, err)
}
