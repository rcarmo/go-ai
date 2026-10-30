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
	var partial *goai.Message
	if errors.As(err, &protocol) {
		partial = protocol.partial
	} else if errors.As(err, &observer) {
		partial = observer.partial
	}
	if partial != nil {
		partial.StopReason = goai.StopReasonError
		partial.ErrorMessage = err.Error()
		partial.Timestamp = time.Now().UnixMilli()
		event.Error = partial
	}
	return event
}
