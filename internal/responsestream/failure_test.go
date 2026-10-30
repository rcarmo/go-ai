package responsestream

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestTerminalFailureMain(t *testing.T) {
	for _, reason := range []goai.StopReason{goai.StopReasonStop, goai.StopReasonLength, goai.StopReasonToolUse} {
		message := &goai.Message{StopReason: reason}
		if err := TerminalFailure(message); err != nil || message.ErrorMessage != "" {
			t.Fatal(message, err)
		}
	}
	for _, reason := range []goai.StopReason{goai.StopReasonError, goai.StopReasonAborted} {
		message := &goai.Message{StopReason: reason}
		err := TerminalFailure(message)
		if err == nil || err.Error() != "Request failed" || message.ErrorMessage != err.Error() {
			t.Fatal(message, err)
		}
		message.ErrorMessage = "provider diagnostic"
		err = TerminalFailure(message)
		if err == nil || err.Error() != message.ErrorMessage {
			t.Fatal(message, err)
		}
	}
}
