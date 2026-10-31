package mistral

import (
	"fmt"
	"io"
	"net/http"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/errortext"
)

const maxMistralErrorBodyBytes = 4096

func readMistralHTTPError(response *http.Response) error {
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxMistralErrorBodyBytes))
	capped := len(raw) == maxMistralErrorBodyBytes && response.ContentLength != maxMistralErrorBodyBytes
	return formatMistralHTTPError(response.StatusCode, raw, err, capped)
}

func formatMistralHTTPError(status int, raw []byte, readErr error, capped bool) error {
	prefix := fmt.Sprintf("Mistral API error (%d)", status)
	// The fetch error's message is already this prefix; the source formatter
	// adds its status prefix again when no readable body is available.
	message := prefix + ": " + prefix
	// Response.text().catch(() => "") discards a failed body read upstream.
	if readErr == nil {
		body := errortext.Trim(errortext.DecodeUTF8(raw))
		if body != "" {
			message = prefix + ": " + goai.TruncateErrorText(body, goai.MaxProviderErrorBodyChars)
		}
	}
	if capped {
		message += "\n[response body read capped at 4096 bytes]"
	}
	return fmt.Errorf("%s", message)
}
