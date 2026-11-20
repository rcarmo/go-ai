package goai_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

type providerErr struct {
	msg       string
	status    int
	hasStatus bool
	body      any
	hasBody   bool
}

func (e providerErr) Error() string                    { return e.msg }
func (e providerErr) ProviderErrorStatus() (int, bool) { return e.status, e.hasStatus }
func (e providerErr) ProviderErrorBody() (any, bool)   { return e.body, e.hasBody }

func TestNormalizeProviderErrorExtractsStatusAndBodyFromMistralShape(t *testing.T) {
	n := goai.NormalizeProviderError(providerErr{msg: "Mistral request failed", status: 403, hasStatus: true, body: `{"error":"blocked by gateway WAF"}`, hasBody: true})
	if n.Status != 403 || n.Body != `{"error":"blocked by gateway WAF"}` || n.MessageCarriesBody {
		t.Fatalf("norm=%#v", n)
	}
}

func TestNormalizeProviderErrorReadsParsedOpenAIAPIErrorBody(t *testing.T) {
	n := goai.NormalizeProviderError(providerErr{msg: "403 status code (no body)", status: 403, hasStatus: true, body: map[string]any{"error": "blocked by gateway WAF"}, hasBody: true})
	if n.Status != 403 || n.Body != `{"error":"blocked by gateway WAF"}` || n.MessageCarriesBody {
		t.Fatalf("norm=%#v", n)
	}
}

func TestNormalizeProviderErrorPreservesMessageWhenGoogleAlreadyCarriesBody(t *testing.T) {
	body := `{"error":{"code":403,"message":"Permission denied"}}`
	n := goai.NormalizeProviderError(providerErr{msg: body, status: 403, hasStatus: true})
	if n.Status != 403 || !n.MessageCarriesBody || n.Message != body {
		t.Fatalf("norm=%#v", n)
	}
}

func TestNormalizeProviderErrorExtractsBedrockShape(t *testing.T) {
	n := goai.NormalizeProviderError(providerErr{msg: "UnknownError", status: 403, hasStatus: true, body: `{"message":"blocked by gateway WAF"}`, hasBody: true})
	if n.Status != 403 || n.Body != `{"message":"blocked by gateway WAF"}` || n.MessageCarriesBody {
		t.Fatalf("norm=%#v", n)
	}
}

func TestNormalizeProviderErrorJSONStringifiesNonError(t *testing.T) {
	n := goai.NormalizeProviderError(map[string]any{"reason": "boom"})
	if n.HasStatus || n.HasBody || n.Message != `{"reason":"boom"}` || n.MessageCarriesBody {
		t.Fatalf("norm=%#v", n)
	}
}

func TestNormalizeProviderErrorTreatsEmptyParsedBodyAsNoBody(t *testing.T) {
	n := goai.NormalizeProviderError(providerErr{msg: "403 status code (no body)", status: 403, hasStatus: true, body: map[string]any{}, hasBody: true})
	if n.HasBody || !n.MessageCarriesBody {
		t.Fatalf("norm=%#v", n)
	}
}

func TestNormalizeProviderErrorTruncatesBodyAtCap(t *testing.T) {
	longBody := strings.Repeat("x", goai.MaxProviderErrorBodyChars+50)
	n := goai.NormalizeProviderError(providerErr{msg: "failed", status: 500, hasStatus: true, body: longBody, hasBody: true})
	if !strings.Contains(n.Body, "... [truncated 50 chars]") || len(n.Body) >= len(longBody) {
		t.Fatalf("body len=%d body suffix=%q", len(n.Body), n.Body[len(n.Body)-30:])
	}
}

func TestNormalizeProviderErrorSetsCarriesWhenMessageContainsBody(t *testing.T) {
	n := goai.NormalizeProviderError(providerErr{msg: "500: upstream exploded", status: 500, hasStatus: true, body: "upstream exploded", hasBody: true})
	if !n.MessageCarriesBody {
		t.Fatalf("norm=%#v", n)
	}
}

func TestFormatProviderErrorSurfacesStatusAndBodyWithoutPrefix(t *testing.T) {
	n := goai.NormalizeProviderError(providerErr{msg: "403 status code (no body)", status: 403, hasStatus: true, body: map[string]any{"error": "blocked by gateway WAF"}, hasBody: true})
	got := goai.FormatProviderError(n)
	if !strings.Contains(got, "403") || !strings.Contains(got, "blocked by gateway WAF") || got == "403 status code (no body)" {
		t.Fatalf("formatted=%q", got)
	}
}

func TestFormatProviderErrorAppliesPrefixWithStatusAndBody(t *testing.T) {
	n := goai.NormalizeProviderError(providerErr{msg: "403 status code (no body)", status: 403, hasStatus: true, body: map[string]any{"error": "blocked by gateway WAF"}, hasBody: true})
	if got := goai.FormatProviderError(n, "OpenAI API error"); got != `OpenAI API error (403): {"error":"blocked by gateway WAF"}` {
		t.Fatalf("formatted=%q", got)
	}
}

func TestFormatProviderErrorPreservesMessageWithPrefixWhenBodyAlreadyPresent(t *testing.T) {
	body := `{"error":{"message":"Permission denied"}}`
	n := goai.NormalizeProviderError(providerErr{msg: body, status: 403, hasStatus: true})
	if got := goai.FormatProviderError(n, "OpenAI API error"); got != "OpenAI API error (403): "+body {
		t.Fatalf("formatted=%q", got)
	}
}

func TestFormatProviderErrorReturnsBareMessageForNonError(t *testing.T) {
	n := goai.NormalizeProviderError(map[string]any{"reason": "boom"})
	if got := goai.FormatProviderError(n); got != `{"reason":"boom"}` {
		t.Fatalf("formatted=%q", got)
	}
}

func TestNormalizeProviderErrorPlainError(t *testing.T) {
	n := goai.NormalizeProviderError(errors.New("plain"))
	if n.Message != "plain" || !n.MessageCarriesBody {
		t.Fatalf("norm=%#v", n)
	}
}

type providerCause struct {
	message string
	cause   error
	code    string
}

func (e providerCause) Error() string     { return e.message }
func (e providerCause) Unwrap() error     { return e.cause }
func (e providerCause) ErrorCode() string { return e.code }

// Pinned packages/ai/test/error-body.test.ts:256, connection error cause detail.
func TestSourceV152ProviderConnectionCause(t *testing.T) {
	const message = "Connection error."
	const refused = "connect ECONNREFUSED 127.0.0.1:8080"
	for _, tc := range []struct {
		name string
		in   error
		want string
	}{
		{"deepest", providerCause{message: message, cause: providerCause{message: "fetch failed", cause: providerCause{message: refused, code: "ECONNREFUSED"}}}, message + ": " + refused},
		{"code", providerCause{message: message, cause: providerCause{message: "other side closed", code: "UND_ERR_SOCKET"}}, message + ": other side closed (UND_ERR_SOCKET)"},
		{"no cause", errors.New(message), message},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := goai.FormatProviderError(goai.NormalizeProviderError(tc.in)); got != tc.want {
				t.Fatalf("provider error = %q, want %q", got, tc.want)
			}
			if got := goai.WithCauseDetail(tc.in); got != tc.want {
				t.Fatalf("cause detail = %q, want %q", got, tc.want)
			}
		})
	}
}

type providerCauseWithResponse struct {
	providerCause
	status int
	body   string
}

func (e providerCauseWithResponse) ProviderErrorStatus() (int, bool) { return e.status, e.status != 0 }
func (e providerCauseWithResponse) ProviderErrorBody() (any, bool)   { return e.body, e.body != "" }

func TestV152ProviderConnectionCauseControls(t *testing.T) {
	cause := providerCause{message: "response error", cause: errors.New("unrelated cause")}
	for _, e := range []providerCauseWithResponse{{providerCause: cause, status: 502}, {providerCause: cause, body: "response body"}} {
		if got := goai.NormalizeProviderError(e); got.Message != "response error" {
			t.Fatalf("status or body did not suppress cause detail: %+v", got)
		}
	}
	var chain error = errors.New("sixth")
	for i := 5; i >= 0; i-- {
		chain = providerCause{message: fmt.Sprintf("depth %d", i), cause: chain}
	}
	if got := goai.WithCauseDetail(chain); got != "depth 0: depth 5" {
		t.Fatalf("cause limit = %q", got)
	}
}

func TestSourceV152OpenAIRefusedConnectionCause(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	model := &goai.Model{ID: "m", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, BaseURL: "http://" + address + "/v1"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = goai.Complete(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{APIKey: "test"})
	if want := fmt.Sprintf("Connection error.: connect ECONNREFUSED %s", address); err == nil || err.Error() != want {
		t.Fatalf("OpenAI completion error = %v, want %q", err, want)
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("OpenAI completion lost the socket cause: %v", err)
	}
}
