package responsestream

import (
	"errors"
	goai "github.com/rcarmo/go-ai"
)

// TerminalFailure keeps a provider's diagnostics on the terminal message while
// preventing an unsuccessful output from being delivered as done.
func TerminalFailure(message *goai.Message) error {
	if message.StopReason != goai.StopReasonError && message.StopReason != goai.StopReasonAborted {
		return nil
	}
	if message.ErrorMessage == "" {
		message.ErrorMessage = "Request failed"
	}
	return errors.New(message.ErrorMessage)
}
