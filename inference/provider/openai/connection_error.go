package openai

import (
	"errors"
	"net"
	"syscall"

	goai "github.com/rcarmo/go-ai"
)

type refusedConnection struct {
	address string
	cause   error
}

func (e refusedConnection) Error() string {
	return "Connection error.: connect ECONNREFUSED " + e.address
}

func (e refusedConnection) Unwrap() error { return e.cause }

// The JS SDK's diagnostic uses the socket code and peer address rather than
// the HTTP method, URL and platform-specific syscall text returned by net/http.
func openAIConnectionError(err error) error {
	var request *goai.ProviderRequestError
	var op *net.OpError
	if errors.As(err, &request) && request.Status == 0 &&
		errors.Is(request.Cause, syscall.ECONNREFUSED) &&
		errors.As(request.Cause, &op) && op.Op == "dial" && op.Addr != nil {
		return refusedConnection{address: op.Addr.String(), cause: err}
	}
	return err
}
