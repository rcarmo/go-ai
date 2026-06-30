package goai_test

import (
	"errors"
	"strings"
	"testing"

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
