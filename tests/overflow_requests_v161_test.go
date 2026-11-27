package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestSourceV161OverflowRequestStatus(t *testing.T) {
	for _, row := range []struct{ name, message string }{
		{"copilot-status", `github-copilot API error (413): {"message":"failed to parse request","code":""}`},
		{"custom-status", "custom-provider API error (413): Payload Too Large"},
		{"raw-status", `413 {"error":{"message":"failed to parse request","code":""}}`},
		{"plugin-code-with-status", `413: {"code":"plugin_request_too_large","message":"request too large"}`},
		{"bare-status", "413"},
	} {
		t.Run(row.name, func(t *testing.T) {
			msg := sourceV161OverflowRequest(row.message)
			if !goai.IsContextOverflow(msg, 1_000_000) {
				t.Error("HTTP 413 with a context window must report overflow")
			}
			if !goai.IsContextOverflow(msg, 0) {
				t.Error("HTTP 413 without a context window must report overflow")
			}
		})
	}
}

func TestSourceV161OverflowCompleteRequestCode(t *testing.T) {
	for _, row := range []struct{ name, message string }{
		{"bare-code", "request_too_large"},
		{"error-type", `400 {"error":{"type":"request_too_large","message":"Request exceeds the maximum size"}}`},
		{"uppercase-code", "Anthropic error: REQUEST_TOO_LARGE"},
	} {
		t.Run(row.name, func(t *testing.T) {
			if !goai.IsContextOverflow(sourceV161OverflowRequest(row.message), 0) {
				t.Error("complete request_too_large code must report overflow")
			}
		})
	}
}

func TestSourceV161OverflowEmbeddedRequestCode(t *testing.T) {
	for _, row := range []struct{ name, message string }{
		{"namespaced-code", `400 {"code":"plugin_request_too_large","message":"plugin input rejected"}`},
		{"prefix-and-suffix", "400 invalid_request_error: my_plugin_request_too_large_flag was set"},
		{"prefix-letter", "Unknown error code xrequest_too_large"},
		{"suffix-number", "Unknown error code request_too_large2"},
		{"suffix-underscore", "Unknown error code request_too_large_flag"},
	} {
		t.Run(row.name, func(t *testing.T) {
			if goai.IsContextOverflow(sourceV161OverflowRequest(row.message), 200_000) {
				t.Error("embedded request_too_large code must not report overflow")
			}
		})
	}
}

func TestOverflowV161RequestCodeASCIIBoundaries(t *testing.T) {
	for _, row := range []struct {
		message string
		want    bool
	}{
		{"éREQUEST_TOO_LARGEé", true},
		{"request_too_large-", true},
		{"requeſt_too_large", false},
	} {
		if got := goai.IsContextOverflow(sourceV161OverflowRequest(row.message), 0); got != row.want {
			t.Errorf("IsContextOverflow(%q) = %v; want %v", row.message, got, row.want)
		}
	}
}

func sourceV161OverflowRequest(message string) *goai.Message {
	return &goai.Message{
		Role: goai.RoleAssistant, StopReason: goai.StopReasonError, ErrorMessage: message, Usage: new(goai.Usage),
	}
}
