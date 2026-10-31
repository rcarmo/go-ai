package bedrock

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/smithy-go"
	goai "github.com/rcarmo/go-ai"
)

type bedrockErrorShapeMain struct {
	message, body string
	status        int
}

func (e bedrockErrorShapeMain) Error() string                    { return e.message }
func (e bedrockErrorShapeMain) ProviderErrorStatus() (int, bool) { return e.status, e.status != 0 }
func (e bedrockErrorShapeMain) ProviderErrorBody() (any, bool)   { return e.body, e.body != "" }

func TestBedrockErrorPrefixesMain(t *testing.T) {
	for code, prefix := range map[string]string{"InternalServerException": "Internal server error", "ModelStreamErrorException": "Model stream error", "ValidationException": "Validation error", "ThrottlingException": "Throttling error", "ServiceUnavailableException": "Service unavailable", "CustomException": "CustomException"} {
		t.Run(code, func(t *testing.T) {
			err := fmt.Errorf("operation wrapper: %w", &smithy.GenericAPIError{Code: code, Message: "request failed"})
			if got := formatBedrockError(err); got != prefix+": request failed" {
				t.Fatal("retry prefix changed", got)
			}
		})
	}
}

func TestBedrockNormalizedErrorFormattingMain(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"plain", errors.New("network failed"), "network failed"},
		{"body", bedrockErrorShapeMain{message: "Unknown", body: "proxy denied", status: 403}, "403: proxy denied"},
		{"carries-body", bedrockErrorShapeMain{message: "403 proxy denied", body: "proxy denied", status: 403}, "403 proxy denied"},
		{"no-status", bedrockErrorShapeMain{message: "original", body: "body"}, "original"},
		{"retention", &smithy.GenericAPIError{Code: "ValidationException", Message: "DATA RETENTION MODE is not available"}, "Validation error: DATA RETENTION MODE is not available See https://docs.aws.amazon.com/bedrock/latest/userguide/data-retention.html for supported data retention modes."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatBedrockError(tc.err); got != tc.want {
				t.Fatal("error formatting", got, tc.want)
			}
		})
	}
}

func TestBedrockHTTPErrorBodyWireMain(t *testing.T) {
	for _, large := range []bool{false, true} {
		t.Run(fmt.Sprint(large), func(t *testing.T) {
			body := `{"message":"proxy denied","gateway":"specific context"}`
			if large {
				body = `{"message":"proxy denied","gateway":"` + strings.Repeat("x", 6000) + `"}`
			}
			var calls int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("X-Amzn-Errortype", "GatewayRejectedException")
				w.Header().Set("X-Amzn-Requestid", "request-id")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(403)
				io.WriteString(w, body)
			}))
			defer server.Close()
			model := &goai.Model{ID: "model", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream, BaseURL: server.URL}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var terminal *goai.ErrorEvent
			for event := range streamBedrock(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{Region: "us-east-1", Env: goai.ProviderEnv{"AWS_BEDROCK_SKIP_AUTH": "1", "AWS_EC2_METADATA_DISABLED": "true"}}) {
				if e, ok := event.(*goai.ErrorEvent); ok {
					if terminal != nil {
						t.Fatal("multiple errors")
					}
					terminal = e
				}
			}
			if terminal == nil || terminal.Error == nil || calls != 1 {
				t.Fatal("missing SDK error", terminal, calls)
			}
			message := terminal.Error.ErrorMessage
			if !strings.HasPrefix(message, "GatewayRejectedException: 403: ") {
				t.Fatal("gateway HTTP context lost", message)
			}
			if large {
				if len(message) > goai.MaxProviderErrorBodyChars+200 || !strings.Contains(message, "[truncated ") {
					t.Fatal("error body capture not bounded", len(message))
				}
			} else if !strings.Contains(message, body) {
				t.Fatal("gateway body lost", message)
			}
			var api smithy.APIError
			if !errors.As(terminal.Err, &api) || api.ErrorCode() != "GatewayRejectedException" {
				t.Fatal("typed cause lost", terminal.Err)
			}
			diagnostics := terminal.Error.Diagnostics
			if len(diagnostics) != 1 || diagnostics[0].Details["status"] != 403 || diagnostics[0].Details["requestId"] != "request-id" || diagnostics[0].Details["errorCode"] != "GatewayRejectedException" {
				t.Fatal("error metadata lost", diagnostics)
			}
		})
	}
}

func TestBedrockDiagnosticClassificationAndFallbackMain(t *testing.T) {
	for _, code := range []string{"TimeoutError", "Unknown", "custom"} {
		details := diagnosticDetailsForBedrockErr(makeBedrockSendError(403, "request-id", code), "")
		if _, ok := details["errorCode"]; ok {
			t.Fatal("non-exception became provider code", details)
		}
	}
	details := diagnosticDetailsForBedrockErr(makeBedrockSendError(400, strings.Repeat("x", 201), "ValidationException"), "fallback-id")
	if details["requestId"] != "fallback-id" {
		t.Fatal("invalid SDK ID masked valid fallback", details)
	}
	err := makeBedrockSendError(400, "request-id", "ValidationException")
	message := bedrockErrorMessage(&goai.Model{}, err)
	before := message.ErrorMessage
	appendBedrockFailureDiagnostic(message, err, "")
	if message.ErrorMessage != before {
		t.Fatal("diagnostic changed retry message")
	}
}

type closeTrackedErrorBodyMain struct {
	io.Reader
	closed int
	err    error
}

func (b *closeTrackedErrorBodyMain) Close() error { b.closed++; return b.err }
func TestBedrockErrorBodyCapturePreservesReaderMain(t *testing.T) {
	raw := strings.Repeat("x", goai.MaxProviderErrorBodyChars+2000)
	sentinel := errors.New("close failure")
	underlying := &closeTrackedErrorBodyMain{Reader: strings.NewReader(raw), err: sentinel}
	capture := &capturedBedrockErrorBody{ReadCloser: underlying}
	got, err := io.ReadAll(capture)
	if err != nil || string(got) != raw || capture.total != len(raw) || len(capture.prefix) != goai.MaxProviderErrorBodyChars {
		t.Fatal("capture altered SDK body or exceeded bound", err, capture.total, len(capture.prefix))
	}
	if err := capture.Close(); !errors.Is(err, sentinel) || underlying.closed != 1 {
		t.Fatal("capture altered close ownership", err, underlying.closed)
	}
}
