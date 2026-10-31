package mistral

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	goai "github.com/rcarmo/go-ai"
)

func TestMistralHTTPErrorFormatMain(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		failure          bool
	}{
		{name: "json", body: ` {"error":"blocked"} `, want: `Mistral API error (403): {"error":"blocked"}`},
		{name: "html", body: "<html>denied</html>", want: "Mistral API error (403): <html>denied</html>"},
		{name: "empty", want: "Mistral API error (403): Mistral API error (403)"},
		{name: "whitespace", body: "\ufeff\t \n", want: "Mistral API error (403): Mistral API error (403)"},
		{name: "nel-not-js-whitespace", body: "\u0085", want: "Mistral API error (403): \u0085"},
		{name: "bom", body: "\ufeffdenied\ufeff", want: "Mistral API error (403): denied"},
		{name: "invalid-bytes", body: string([]byte{0xff, 0xff}), want: "Mistral API error (403): ��"},
		{name: "incomplete-scalar", body: string([]byte{0xe2, 0x82}), want: "Mistral API error (403): �"},
		{name: "encoded-surrogate", body: string([]byte{0xed, 0xa0, 0x80}), want: "Mistral API error (403): ���"},
		{name: "read-failure", body: "partial secret", failure: true, want: "Mistral API error (403): Mistral API error (403)"},
		{name: "display-limit", body: strings.Repeat("x", 4003), want: "Mistral API error (403): " + strings.Repeat("x", 4000) + "... [truncated 3 chars]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var failure error
			if tc.failure {
				failure = errors.New("read failed")
			}
			got := formatMistralHTTPError(403, []byte(tc.body), failure, false).Error()
			if got != tc.want || !utf8.ValidString(got) {
				t.Fatal("wrong HTTP diagnostic", got, tc.want)
			}
		})
	}
}

type countedMistralErrorBody struct {
	source  *strings.Reader
	read    int
	closed  bool
	failure bool
}

func (r *countedMistralErrorBody) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	r.read += n
	if r.failure {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}
func (r *countedMistralErrorBody) Close() error { r.closed = true; return nil }

func TestMistralHTTPErrorReadLimitMain(t *testing.T) {
	for _, tc := range []struct {
		name, body      string
		length          int64
		capped, failure bool
	}{
		{name: "known-over-limit", body: strings.Repeat("x", 5000) + "TAIL_NOT_ADMITTED", length: 5017, capped: true},
		{name: "unknown-over-limit", body: strings.Repeat("x", 5000) + "TAIL_NOT_ADMITTED", length: -1, capped: true},
		{name: "known-exact", body: strings.Repeat("x", 4096), length: 4096},
		{name: "unknown-exact", body: strings.Repeat("x", 4096), length: -1, capped: true},
		{name: "short", body: "blocked", length: 7},
		{name: "unicode-cut", body: strings.Repeat("a", 4095) + "€TAIL_NOT_ADMITTED", length: -1, capped: true},
		{name: "read-failure", body: "partial secret", length: -1, failure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &countedMistralErrorBody{source: strings.NewReader(tc.body), failure: tc.failure}
			got := readMistralHTTPError(&http.Response{StatusCode: 403, Body: reader, ContentLength: tc.length}).Error()
			if reader.read > 4096 || reader.closed {
				t.Fatal("read budget/ownership violated", reader.read, reader.closed)
			}
			if len(tc.body) >= 4096 && reader.read != 4096 {
				t.Fatal("wrong prefix budget", reader.read)
			}
			if strings.Contains(got, "TAIL_NOT_ADMITTED") || !utf8.ValidString(got) {
				t.Fatal("unsafe diagnostic", got)
			}
			if strings.Contains(got, "[response body read capped at 4096 bytes]") != tc.capped {
				t.Fatal("cap uncertainty lost", got)
			}
			if tc.failure && got != "Mistral API error (403): Mistral API error (403)" {
				t.Fatal("failed read retained partial body", got)
			}
			reader.Close()
		})
	}
}

func TestMistralHTTPErrorWireMain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Error("wrong route", r.URL.Path)
		}
		w.WriteHeader(403)
		io.WriteString(w, `{"error":"gateway denied"}`)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	count := 0
	for event := range streamMistral(ctx, &goai.Model{ID: "test", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations, BaseURL: server.URL, MaxTokens: 1024}, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{APIKey: "test"}) {
		failure, ok := event.(*goai.ErrorEvent)
		if !ok {
			t.Fatal("unexpected event", event)
		}
		count++
		if failure.Reason != goai.StopReasonError || failure.Err == nil || failure.Err.Error() != `Mistral API error (403): {"error":"gateway denied"}` {
			t.Fatal("wire error lost formatting", failure)
		}
	}
	if count != 1 {
		t.Fatal("wrong failure count", count)
	}
}

func TestMistralHTTPErrorBodyCancellationMain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	opts := &goai.StreamOptions{APIKey: "test", OnResponse: func(int, map[string]string, *goai.Model) { cancel() }}
	failures := 0
	for event := range streamMistral(ctx, &goai.Model{ID: "test", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations, BaseURL: server.URL, MaxTokens: 1024}, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, opts) {
		failure, ok := event.(*goai.ErrorEvent)
		if !ok {
			t.Fatal("unexpected event", event)
		}
		failures++
		if failure.Reason != goai.StopReasonAborted {
			t.Fatal("body cancellation not classified", failure)
		}
	}
	if failures != 1 {
		t.Fatal("missing cancellation", failures)
	}
}
