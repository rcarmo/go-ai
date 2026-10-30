package openaicodex

import (
	"errors"
	goai "github.com/rcarmo/go-ai"
	"time"
)

func codexStreamErrorEvent(err error) *goai.ErrorEvent {
	event := &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
	var protocol *codexProtocolError
	if errors.As(err, &protocol) && protocol.partial != nil {
		partial := protocol.partial
		partial.StopReason = goai.StopReasonError
		partial.ErrorMessage = err.Error()
		partial.Timestamp = time.Now().UnixMilli()
		event.Error = partial
	}
	return event
}
