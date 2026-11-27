package openai

import (
	"io"
	"net/http"
	"strings"
)

// openaiTestRoundTripper injects a request-local transport without opening a socket.
type openaiTestRoundTripper func(*http.Request) (*http.Response, error)

func (f openaiTestRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// successfulSSETestResponse wraps the fixture's exact bytes, including any terminator.
func successfulSSETestResponse(request *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}
