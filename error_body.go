package goai

import (
	"encoding/json"
	"fmt"
	"strings"
)

const MaxProviderErrorBodyChars = 4000

type NormalizedProviderError struct {
	Status             int
	HasStatus          bool
	Body               string
	HasBody            bool
	Message            string
	MessageCarriesBody bool
}

type ProviderErrorShape interface {
	error
	ProviderErrorStatus() (int, bool)
	ProviderErrorBody() (any, bool)
}

func NormalizeProviderError(err any) NormalizedProviderError {
	if err == nil {
		return NormalizedProviderError{Message: "null"}
	}
	if pe, ok := err.(ProviderErrorShape); ok {
		status, hasStatus := pe.ProviderErrorStatus()
		body, hasBody := normalizeProviderErrorBody(pe.ProviderErrorBody())
		message := pe.Error()
		carries := !hasBody || strings.Contains(message, body)
		return NormalizedProviderError{Status: status, HasStatus: hasStatus, Body: body, HasBody: hasBody, Message: message, MessageCarriesBody: carries}
	}
	if e, ok := err.(error); ok {
		return NormalizedProviderError{Message: e.Error(), MessageCarriesBody: true}
	}
	return NormalizedProviderError{Message: SafeJSONStringify(err)}
}

func normalizeProviderErrorBody(body any, ok bool) (string, bool) {
	if !ok || body == nil {
		return "", false
	}
	var text string
	switch v := body.(type) {
	case string:
		text = v
	case map[string]any:
		if len(v) == 0 {
			return "", false
		}
		text = SafeJSONStringify(v)
	default:
		text = SafeJSONStringify(v)
	}
	text = strings.TrimSpace(text)
	if text == "" || text == "{}" {
		return "", false
	}
	return TruncateErrorText(text, MaxProviderErrorBodyChars), true
}

func FormatProviderError(norm NormalizedProviderError, prefix ...string) string {
	p := ""
	if len(prefix) > 0 {
		p = prefix[0]
	}
	if norm.MessageCarriesBody || !norm.HasStatus || !norm.HasBody {
		if p != "" && norm.HasStatus {
			return fmt.Sprintf("%s (%d): %s", p, norm.Status, norm.Message)
		}
		return norm.Message
	}
	if p != "" {
		return fmt.Sprintf("%s (%d): %s", p, norm.Status, norm.Body)
	}
	return fmt.Sprintf("%d: %s", norm.Status, norm.Body)
}

func TruncateErrorText(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	return fmt.Sprintf("%s... [truncated %d chars]", text[:maxChars], len(text)-maxChars)
}

func SafeJSONStringify(value any) string {
	b, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	if string(b) == "" {
		return fmt.Sprint(value)
	}
	return string(b)
}
