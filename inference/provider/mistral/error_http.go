package mistral

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	goai "github.com/rcarmo/go-ai"
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
		body := strings.TrimFunc(decodeMistralErrorBody(raw), func(r rune) bool { return r == '\ufeff' || r != '\u0085' && unicode.IsSpace(r) })
		if body != "" {
			message = prefix + ": " + goai.TruncateErrorText(body, goai.MaxProviderErrorBodyChars)
		}
	}
	if capped {
		message += "\n[response body read capped at 4096 bytes]"
	}
	return fmt.Errorf("%s", message)
}

// Decode a complete admitted prefix with Response.text()/UTF-8 TextDecoder
// replacement rules. Incomplete valid prefixes produce one replacement unit.
func decodeMistralErrorBody(raw []byte) string {
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	if utf8.Valid(raw) {
		return string(raw)
	}
	var out strings.Builder
	for i := 0; i < len(raw); {
		b := raw[i]
		size := 0
		switch {
		case b < 0x80:
			size = 1
		case b >= 0xc2 && b <= 0xdf:
			size = 2
		case b >= 0xe0 && b <= 0xef:
			size = 3
		case b >= 0xf0 && b <= 0xf4:
			size = 4
		default:
			out.WriteRune(utf8.RuneError)
			i++
			continue
		}
		consumed := 1
		valid := true
		for consumed < size {
			if i+consumed >= len(raw) {
				valid = false
				break
			}
			c := raw[i+consumed]
			if c < 0x80 || c > 0xbf || consumed == 1 && (b == 0xe0 && c < 0xa0 || b == 0xed && c > 0x9f || b == 0xf0 && c < 0x90 || b == 0xf4 && c > 0x8f) {
				valid = false
				break
			}
			consumed++
		}
		if !valid {
			out.WriteRune(utf8.RuneError)
			i += consumed
			continue
		}
		out.Write(raw[i : i+size])
		i += size
	}
	return out.String()
}
