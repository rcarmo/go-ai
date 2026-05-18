package websocket

import (
	"context"
	"net/http"

	coderws "github.com/coder/websocket"
)

type Conn = coderws.Conn
type DialOptions = coderws.DialOptions
type AcceptOptions = coderws.AcceptOptions
type MessageType = coderws.MessageType

type StatusCode = coderws.StatusCode

const (
	MessageText   = coderws.MessageText
	MessageBinary = coderws.MessageBinary

	StatusNormalClosure   = coderws.StatusNormalClosure
	StatusPolicyViolation = coderws.StatusPolicyViolation
)

func Dial(ctx context.Context, url string, opts *DialOptions) (*Conn, *http.Response, error) {
	return coderws.Dial(ctx, url, opts)
}

func Accept(w http.ResponseWriter, r *http.Request, opts *AcceptOptions) (*Conn, error) {
	return coderws.Accept(w, r, opts)
}

func CloseStatus(err error) StatusCode { return coderws.CloseStatus(err) }
