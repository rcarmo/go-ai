// Provider hook helpers — shared by all provider implementations.
package goai

import "net/http"

// InvokeOnPayload calls the OnPayload hook if set, returning the (possibly replaced) payload.
func InvokeOnPayload(opts *StreamOptions, payload interface{}, model *Model) (interface{}, error) {
	if opts == nil || opts.OnPayload == nil {
		return payload, nil
	}
	replaced, err := opts.OnPayload(payload, model)
	if err != nil {
		return nil, err
	}
	if replaced != nil {
		return replaced, nil
	}
	return payload, nil
}

// InvokeOnResponse calls the OnResponse hook if set.
func InvokeOnResponse(opts *StreamOptions, resp *http.Response, model *Model) {
	if opts == nil || opts.OnResponse == nil || resp == nil {
		return
	}
	headers := make(map[string]string)
	for k := range resp.Header {
		headers[k] = resp.Header.Get(k)
	}
	opts.OnResponse(resp.StatusCode, headers, model)
}
