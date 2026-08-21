package oauth

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestEnableAllGitHubCopilotModelsCapsPolicyConcurrency(t *testing.T) {
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	var total atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := inFlight.Add(1)
		for {
			max := maxInFlight.Load()
			if current <= max || maxInFlight.CompareAndSwap(max, current) {
				break
			}
		}
		total.Add(1)
		time.Sleep(25 * time.Millisecond)
		inFlight.Add(-1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	oldClient := copilotPolicyHTTPClient
	oldList := copilotPolicyListModels
	oldBase := copilotPolicyBaseURL
	t.Cleanup(func() {
		copilotPolicyHTTPClient = oldClient
		copilotPolicyListModels = oldList
		copilotPolicyBaseURL = oldBase
	})
	copilotPolicyHTTPClient = server.Client()
	copilotPolicyBaseURL = func(_ string, _ string) string { return server.URL }
	copilotPolicyListModels = func() []*goai.Model {
		models := make([]*goai.Model, 11)
		for i := range models {
			models[i] = &goai.Model{ID: string(rune('a' + i)), Provider: goai.ProviderGitHubCopilot}
		}
		return models
	}

	EnableAllGitHubCopilotModels("token", "")
	if total.Load() != 11 {
		t.Fatalf("total requests = %d, want 11", total.Load())
	}
	if got := maxInFlight.Load(); got > copilotPolicyConcurrency {
		t.Fatalf("max in-flight = %d, want <= %d", got, copilotPolicyConcurrency)
	}
}
