package goai

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type responseHookTransportV161 func(*http.Request) (*http.Response, error)

func (f responseHookTransportV161) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type responseHookBodyV161 struct{ reads, closes int }

func (b *responseHookBodyV161) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (b *responseHookBodyV161) Close() error             { b.closes++; return nil }

func TestProviderResponseHookPreservesTypedRefusalV161(t *testing.T) {
	body := new(responseHookBodyV161)
	marker := &ProviderRequestError{Status: 503, Headers: http.Header{"Retry-After-Ms": {"0"}}, Message: "fixture refused headers"}
	var requests, retries int
	client := &http.Client{Transport: responseHookTransportV161(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		requests++
		return &http.Response{StatusCode: 503, Header: marker.Headers, Body: body}, nil
	})}
	ctx := t.Context()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://fixture.invalid/decision", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := NoRetryConfig()
	cfg.MaxRetries = 1
	cfg.OnRetry = func(int, time.Duration, int) { retries++ }
	response, err := doProviderRequestWithRetryObserved(ctx, client, req, cfg, nil, true, nil,
		func(*http.Response) error { return marker })
	if response != nil || err != marker || requests != 1 || retries != 0 || body.reads != 0 || body.closes != 1 {
		t.Fatalf("response=%v refusal identity=%t requests=%d retries=%d reads=%d closes=%d", response, err == marker, requests, retries, body.reads, body.closes)
	}
}
