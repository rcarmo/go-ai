package goai_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source-shaped OpenAI Decisions gateway refusal, with an additional server
// force-retry directive to exercise noRetryStatuses precedence through HTTP.
func TestMainDecisionsProviderExcludesGatewayTimeoutDespiteRetryHeader(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("X-Should-Retry", "true")
		w.Header().Set("Retry-After-Ms", "0")
		w.WriteHeader(http.StatusGatewayTimeout)
		_, _ = w.Write([]byte("<html>gateway timeout</html>"))
	}))
	defer srv.Close()
	result, err := goai.Classify(decisionsModel110(srv.URL), issue1Context(), &goai.ClassifierOptions{APIKey: "fixture", MaxRetries: 2})
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 || result.StopReason != goai.StopReasonError || !strings.Contains(result.ErrorMessage, "600K") || strings.Contains(result.ErrorMessage, "<html>") {
		t.Fatalf("requests=%d, stop=%v, error=%q", requests.Load(), result.StopReason, result.ErrorMessage)
	}
}
