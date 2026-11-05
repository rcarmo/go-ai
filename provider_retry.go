package goai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const defaultProviderMaxRetryDelay = 60 * time.Second

type ProviderRequestError struct {
	Status  int
	Headers http.Header
	Message string
	Cause   error
}

func (e *ProviderRequestError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("provider error: %d", e.Status)
}

type ProviderRetryOptions struct {
	MaxRetries      int
	MaxRetryDelay   time.Duration
	DisableDelayCap bool
	// NoRetryStatuses excludes these provider statuses even when a server
	// explicitly requests a retry.
	NoRetryStatuses []int
}

func DoProviderRequestWithRetry(ctx context.Context, client *http.Client, req *http.Request, cfg RetryConfig) (*http.Response, error) {
	return doProviderRequestWithRetry(ctx, client, req, cfg, nil, false)
}

var classifierAttemptDeadline = errors.New("classifier attempt deadline")

// The caller owns cancel until it transfers ownership to the response body.
func classifierAttemptContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeoutCause(ctx, timeout, classifierAttemptDeadline)
}

type classifierAttemptBody struct {
	io.ReadCloser
	attempt context.Context
	caller  context.Context
	cancel  context.CancelFunc
	timeout time.Duration
}

func (b *classifierAttemptBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		err = classifierAttemptError(err, b.caller, b.attempt, b.timeout)
	}
	return n, err
}

func (b *classifierAttemptBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

func classifierAttemptError(err error, caller, attempt context.Context, timeout time.Duration) error {
	if caller.Err() == nil && context.Cause(attempt) == classifierAttemptDeadline {
		return fmt.Errorf("Request timed out after %dms", timeout.Milliseconds())
	}
	return err
}

func doProviderRequestWithRetry(ctx context.Context, client *http.Client, req *http.Request, cfg RetryConfig, noRetryStatuses []int, stopOnTransportError bool) (*http.Response, error) {
	attemptTimeout := cfg.RequestTimeout
	cfg.applyDefaults()
	if client == nil {
		client = cfg.NewHTTPClient()
	}
	if req == nil {
		return nil, fmt.Errorf("nil request")
	}
	if req.Body != nil && req.GetBody == nil && cfg.MaxRetries > 0 {
		return nil, fmt.Errorf("retry requires request.GetBody for replayable request body")
	}
	options := ProviderRetryOptions{MaxRetries: cfg.MaxRetries, MaxRetryDelay: time.Duration(cfg.MaxRetryDelayMs) * time.Millisecond, DisableDelayCap: cfg.MaxRetryDelayMs == 0}
	attempt := 0
	return retryProviderRequest(ctx, func() (*http.Response, error) {
		attempt++
		attemptCtx := ctx
		cancel := func() {}
		if stopOnTransportError && attemptTimeout > 0 {
			attemptCtx, cancel = classifierAttemptContext(ctx, attemptTimeout)
		}
		attemptReq := req.Clone(attemptCtx)
		if req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				cancel()
				return nil, fmt.Errorf("clone request body: %w", err)
			}
			attemptReq.Body = body
		}
		resp, err := client.Do(attemptReq)
		if err != nil {
			if stopOnTransportError {
				err = classifierAttemptError(err, ctx, attemptCtx, attemptTimeout)
				cancel()
				return nil, err
			}
			cancel()
			return nil, &ProviderRequestError{Status: 0, Headers: http.Header{}, Message: err.Error(), Cause: err}
		}
		if stopOnTransportError && attemptTimeout > 0 {
			resp.Body = &classifierAttemptBody{ReadCloser: resp.Body, attempt: attemptCtx, caller: ctx, cancel: cancel, timeout: attemptTimeout}
		}
		// Retry directives apply to failures, never an opened successful stream.
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		if !isRetryableProviderStatus(resp.StatusCode, resp.Header, cfg) {
			return resp, nil
		}
		for _, status := range noRetryStatuses {
			if resp.StatusCode == status {
				return resp, nil
			}
		}
		if attempt > cfg.MaxRetries {
			return resp, nil
		}
		if stopOnTransportError {
			// Classifier fetch reads the error body before constructing the
			// retryable HTTP error. Bound this read like the final response.
			bytesRead, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, (1<<20)+1))
			if readErr != nil || bytesRead > 1<<20 {
				resp.Body.Close()
				if readErr != nil {
					return nil, readErr
				}
				return nil, fmt.Errorf("provider error response exceeds 1 MiB")
			}
		}
		headers := resp.Header.Clone()
		// Carry an explicitly allowed native status through the generic retry
		// classifier without mutating the actual response headers.
		if cfg.RetryableStatuses != nil {
			if headers == nil {
				headers = make(http.Header)
			}
			headers.Set("x-should-retry", "true")
		}
		resp.Body.Close()
		return nil, &ProviderRequestError{Status: resp.StatusCode, Headers: headers, Message: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}, options, cfg.OnRetry)
}

func RetryProviderRequest[T any](ctx context.Context, request func() (T, error), options ProviderRetryOptions) (T, error) {
	return retryProviderRequest(ctx, request, options, nil)
}

func retryProviderRequest[T any](ctx context.Context, request func() (T, error), options ProviderRetryOptions, onRetry func(int, time.Duration, int)) (T, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	remaining := options.MaxRetries
	maxRetries := options.MaxRetries
	for {
		value, err := request()
		if err == nil {
			return value, nil
		}
		if ctx.Err() != nil {
			var zero T
			return zero, providerAbortError()
		}
		perr, ok := err.(*ProviderRequestError)
		if remaining <= 0 || !ok || !isRetryableProviderRequestError(perr) {
			return value, err
		}
		for _, status := range options.NoRetryStatuses {
			if perr.Status == status {
				return value, err
			}
		}
		retryIndex := maxRetries - remaining
		remaining--
		delay, err := providerRetryDelay(perr, retryIndex, options)
		if err != nil {
			var zero T
			return zero, err
		}
		if onRetry != nil {
			onRetry(retryIndex, delay, perr.Status)
		}
		if err := sleepProviderRetry(ctx, delay); err != nil {
			var zero T
			return zero, err
		}
	}
}

func isRetryableProviderStatus(code int, headers http.Header, cfg RetryConfig) bool {
	// Decide before closing the body: a server veto is a final response, not a
	// retry error that discards its payload. Explicit native status lists remain
	// authoritative over a server force-retry directive.
	if headers.Get("x-should-retry") == "false" {
		return false
	}
	if cfg.RetryableStatuses != nil {
		for _, status := range cfg.RetryableStatuses {
			if status == code {
				return true
			}
		}
		return false
	}
	return headers.Get("x-should-retry") == "true" || code == 408 || code == 409 || code == 429 || code >= 500
}

func isRetryableProviderRequestError(err *ProviderRequestError) bool {
	if err.Headers != nil {
		switch err.Headers.Get("x-should-retry") {
		case "true":
			return true
		case "false":
			return false
		}
	}
	if err.Status == 0 {
		return true
	}
	return err.Status == 408 || err.Status == 409 || err.Status == 429 || err.Status >= 500
}

func providerRetryDelay(err *ProviderRequestError, retryIndex int, options ProviderRetryOptions) (time.Duration, error) {
	if err.Headers != nil {
		if v := err.Headers.Get("retry-after-ms"); v != "" {
			ms, numeric := parseProviderRetryFloat(v)
			if numeric && !math.IsInf(ms, 0) {
				return validateProviderRetrySeconds(ms/1000, options, err.Error())
			}
		}
		if v := err.Headers.Get("retry-after"); v != "" {
			seconds, numeric := parseProviderRetryFloat(v)
			if numeric {
				if !math.IsInf(seconds, 0) && !math.IsInf(seconds*1000, 0) {
					return validateProviderRetrySeconds(seconds, options, err.Error())
				}
			} else if when, dateErr := parseProviderRetryDate(v); dateErr == nil {
				return validateProviderRetryDelay(time.Until(when), options, err.Error())
			}
		}
	}
	// Cap before shifting: the retry index can grow beyond the integer width.
	delay := 8 * time.Second
	if retryIndex < 4 {
		delay = 500 * time.Millisecond << max(retryIndex, 0)
	}
	return time.Duration(float64(delay) * (1 - rand.Float64()*0.25)), nil
}

// parseProviderRetryDate supports HTTP dates and selected long-month and
// numeric GMT-offset forms accepted by Date.parse. Other formats remain open.
func parseProviderRetryDate(value string) (time.Time, error) {
	when, err := http.ParseTime(value)
	if err == nil {
		return when, nil
	}
	for _, layout := range []string{
		"January 2, 2006 15:04:05 GMT-0700",
		"Mon, 02 Jan 2006 15:04:05 GMT-0700",
		"January 2, 2006 15:04:05 GMT",
	} {
		if when, err = time.Parse(layout, value); err == nil {
			return when, nil
		}
	}
	return time.Time{}, err
}

// parseProviderRetryFloat follows Number.parseFloat's decimal-prefix grammar.
// Its bool distinguishes NaN (which permits the date fallback) from infinity.
func parseProviderRetryFloat(value string) (float64, bool) {
	value = strings.TrimLeftFunc(value, func(r rune) bool {
		// ECMAScript whitespace is Unicode Zs plus these whitespace and line
		// terminator characters; Go's IsSpace also includes U+0085.
		return unicode.Is(unicode.Zs, r) || r == '\uFEFF' || r == '\t' || r == '\v' || r == '\f' || r == '\n' || r == '\r' || r == '\u2028' || r == '\u2029'
	})
	i := 0
	if i < len(value) && (value[i] == '+' || value[i] == '-') {
		i++
	}
	if strings.HasPrefix(value[i:], "Infinity") {
		if i > 0 && value[0] == '-' {
			return math.Inf(-1), true
		}
		return math.Inf(1), true
	}
	digits := 0
	for i < len(value) && value[i] >= '0' && value[i] <= '9' {
		i++
		digits++
	}
	if i < len(value) && value[i] == '.' {
		i++
		for i < len(value) && value[i] >= '0' && value[i] <= '9' {
			i++
			digits++
		}
	}
	if digits == 0 {
		return 0, false
	}
	end := i
	if i < len(value) && (value[i] == 'e' || value[i] == 'E') {
		i++
		if i < len(value) && (value[i] == '+' || value[i] == '-') {
			i++
		}
		start := i
		for i < len(value) && value[i] >= '0' && value[i] <= '9' {
			i++
		}
		if i > start {
			end = i
		}
	}
	parsed, _ := strconv.ParseFloat(value[:end], 64)
	return parsed, true
}

func validateProviderRetryDelay(delay time.Duration, options ProviderRetryOptions, message string) (time.Duration, error) {
	maxDelay := options.MaxRetryDelay
	if maxDelay == 0 {
		maxDelay = defaultProviderMaxRetryDelay
	}
	if !options.DisableDelayCap && maxDelay > 0 && delay > maxDelay {
		return 0, fmt.Errorf("server requested %.0fs retry delay (max: %.0fs). %s", math.Ceil(delay.Seconds()), math.Ceil(maxDelay.Seconds()), message)
	}
	return delay, nil
}

func validateProviderRetrySeconds(seconds float64, options ProviderRetryOptions, message string) (time.Duration, error) {
	maxDelay := options.MaxRetryDelay
	if maxDelay == 0 {
		maxDelay = defaultProviderMaxRetryDelay
	}
	if !options.DisableDelayCap && maxDelay > 0 && seconds > maxDelay.Seconds() {
		return 0, fmt.Errorf("server requested %.0fs retry delay (max: %.0fs). %s", math.Ceil(seconds), math.Ceil(maxDelay.Seconds()), message)
	}
	// Convert only after checking the cap. A finite server value can exceed
	// time.Duration's range; saturate instead of wrapping to an immediate retry.
	nanos := seconds * float64(time.Second)
	if nanos >= float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64), nil
	}
	if nanos <= float64(math.MinInt64) {
		return time.Duration(math.MinInt64), nil
	}
	return time.Duration(nanos), nil
}

func sleepProviderRetry(ctx context.Context, delay time.Duration) error {
	if ctx.Err() != nil {
		return providerAbortError()
	}
	timer := time.NewTimer(max(delay, 0))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return providerAbortError()
	case <-timer.C:
		return nil
	}
}

func providerAbortError() error {
	err := fmt.Errorf("request aborted")
	return err
}
