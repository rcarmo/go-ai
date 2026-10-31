package bedrock

import (
	"errors"
	"io"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBedrockErrorCaptureTextMain(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want, suffix string
		partial                 bool
	}{
		{name: "small", raw: "blocked", want: "blocked"},
		{name: "bom-trim", raw: "\ufeff blocked \ufeff", want: "blocked"},
		{name: "nel-retained", raw: "\u0085", want: "\u0085"},
		{name: "invalid-byte", raw: string([]byte{0xff}), want: "�"},
		{name: "incomplete-scalar", raw: string([]byte{0xe2, 0x82}), want: "�"},
		{name: "multibyte-omissions", raw: strings.Repeat("é", 2500), want: strings.Repeat("é", 2000), suffix: "\n[response body capture capped at 4000 bytes; 1000 bytes omitted]"},
		{name: "split-scalar", raw: strings.Repeat("a", 3999) + "€tail", want: strings.Repeat("a", 3999) + "�", suffix: "\n[response body capture capped at 4000 bytes; 6 bytes omitted]"},
		{name: "exact-complete", raw: strings.Repeat("x", 4000), want: strings.Repeat("x", 4000)},
		{name: "unknown-tail", raw: strings.Repeat("x", 5000), want: strings.Repeat("x", 4000), partial: true, suffix: "\n[response body capture capped at 4000 bytes; remaining length unknown]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := strings.NewReader(tc.raw)
			underlying := &closeTrackedErrorBodyMain{Reader: reader}
			capture := &capturedBedrockErrorBody{ReadCloser: underlying}
			var err error
			if tc.partial {
				_, err = io.ReadFull(capture, make([]byte, 4000))
			} else {
				_, err = io.ReadAll(capture)
			}
			if err != nil {
				t.Fatal(err)
			}
			remaining := reader.Len()
			failure := makeBedrockSendError(403, "request-id", "GatewayRejectedException")
			bedrockRawErrorResponse(failure).Body = capture
			got := formatBedrockError(failure)
			want := "GatewayRejectedException: 403: " + tc.want + tc.suffix
			if got != want || !utf8.ValidString(got) {
				t.Fatal("wrong/invalid capture diagnostic", got, want)
			}
			if len(capture.prefix) > 4000 || underlying.closed != 0 || reader.Len() != remaining {
				t.Fatal("capture/formatter changed SDK ownership or retention", len(capture.prefix), underlying.closed, reader.Len())
			}
			if capture.complete == tc.partial {
				t.Fatal("completion tracking", capture.complete, tc.partial)
			}
		})
	}
}

type bedrockFaultReaderMain struct{}

func (bedrockFaultReaderMain) Read(p []byte) (int, error) {
	return copy(p, "PARTIAL_SHOULD_NOT_SURFACE"), io.ErrUnexpectedEOF
}
func TestBedrockErrorCaptureFailureMain(t *testing.T) {
	underlying := &closeTrackedErrorBodyMain{Reader: bedrockFaultReaderMain{}}
	capture := &capturedBedrockErrorBody{ReadCloser: underlying}
	_, err := io.ReadAll(capture)
	if !errors.Is(err, io.ErrUnexpectedEOF) || !capture.failed {
		t.Fatal("read error changed", err)
	}
	failure := makeBedrockSendError(403, "request-id", "GatewayRejectedException")
	bedrockRawErrorResponse(failure).Body = capture
	if got := formatBedrockError(failure); got != "GatewayRejectedException: validation failed" {
		t.Fatal("failed partial capture replaced SDK message", got)
	}
	if underlying.closed != 0 {
		t.Fatal("formatter closed SDK body")
	}
}

func TestBedrockEmptyCapturePreservesSDKMessageMain(t *testing.T) {
	for _, raw := range []string{"", "{}", "\ufeff \t\n"} {
		capture := &capturedBedrockErrorBody{ReadCloser: io.NopCloser(strings.NewReader(raw))}
		if _, err := io.ReadAll(capture); err != nil {
			t.Fatal(err)
		}
		failure := makeBedrockSendError(403, "request-id", "GatewayRejectedException")
		bedrockRawErrorResponse(failure).Body = capture
		if got := formatBedrockError(failure); got != "GatewayRejectedException: validation failed" {
			t.Fatal("empty capture hid SDK message", got)
		}
	}
}
