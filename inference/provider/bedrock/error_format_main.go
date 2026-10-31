package bedrock

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	goai "github.com/rcarmo/go-ai"
)

// Keep only a bounded error prefix while leaving the SDK's body reads and close
// ownership unchanged. The SDK otherwise consumes the body before returning it.
type capturedBedrockErrorBody struct {
	io.ReadCloser
	prefix []byte
	total  int
}

func (b *capturedBedrockErrorBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.total += n
	if remaining := goai.MaxProviderErrorBodyChars - len(b.prefix); remaining > 0 {
		b.prefix = append(b.prefix, p[:min(n, remaining)]...)
	}
	return n, err
}
func addBedrockErrorBodyCapture(stack *middleware.Stack) error {
	return stack.Deserialize.Insert(middleware.DeserializeMiddlewareFunc("BedrockErrorBodyCapture", func(ctx context.Context, in middleware.DeserializeInput, next middleware.DeserializeHandler) (middleware.DeserializeOutput, middleware.Metadata, error) {
		out, metadata, err := next.HandleDeserialize(ctx, in)
		if response, ok := out.RawResponse.(*smithyhttp.Response); ok && response != nil && response.Response != nil && response.StatusCode >= 400 && response.Body != nil {
			response.Body = &capturedBedrockErrorBody{ReadCloser: response.Body}
		}
		return out, metadata, err
	}), "OperationDeserializer", middleware.After)
}

func bedrockRawErrorResponse(err error) *smithyhttp.Response {
	var awsErr *awshttp.ResponseError
	if errors.As(err, &awsErr) && awsErr.ResponseError != nil {
		return awsErr.ResponseError.Response
	}
	var responseErr *smithyhttp.ResponseError
	if errors.As(err, &responseErr) {
		return responseErr.Response
	}
	return nil
}
func formatBedrockError(err error) string {
	norm := goai.NormalizeProviderError(err)
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		if _, custom := err.(goai.ProviderErrorShape); !custom {
			norm.Message = apiErr.ErrorMessage()
		}
	}
	if response := bedrockRawErrorResponse(err); response != nil && response.Response != nil {
		if capture, ok := response.Body.(*capturedBedrockErrorBody); ok {
			body := strings.TrimSpace(string(capture.prefix))
			if body != "" && body != "{}" {
				if capture.total > len(capture.prefix) {
					body += fmt.Sprintf("... [truncated %d chars]", capture.total-len(capture.prefix))
				}
				norm.Body, norm.HasBody, norm.Status, norm.HasStatus = body, true, response.StatusCode, true
			}
		}
	}
	norm.MessageCarriesBody = !norm.HasBody || strings.Contains(norm.Message, norm.Body)
	core := norm.Message
	if !norm.MessageCarriesBody && norm.HasStatus && norm.HasBody {
		core = fmt.Sprintf("%d: %s", norm.Status, norm.Body)
	}
	if strings.Contains(strings.ToLower(core), "data retention mode") {
		core += " See https://docs.aws.amazon.com/bedrock/latest/userguide/data-retention.html for supported data retention modes."
	}
	if apiErr != nil {
		prefix := apiErr.ErrorCode()
		switch prefix {
		case "InternalServerException":
			prefix = "Internal server error"
		case "ModelStreamErrorException":
			prefix = "Model stream error"
		case "ValidationException":
			prefix = "Validation error"
		case "ThrottlingException":
			prefix = "Throttling error"
		case "ServiceUnavailableException":
			prefix = "Service unavailable"
		}
		return prefix + ": " + core
	}
	return core
}
