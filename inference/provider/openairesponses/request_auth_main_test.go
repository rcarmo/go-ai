package openairesponses

import (
	"context"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResponsesSDKAuthHeaderPrecedenceWireMain(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiOpenAIResponses, goai.ApiAzureOpenAIResponses} {
		for _, mode := range []string{"generated", "model", "caller", "header_only"} {
			t.Run(fmt.Sprintf("%s/%s", api, mode), func(t *testing.T) {
				captured := make(chan http.Header, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					captured <- r.Header.Clone()
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{}}}\n\n")
				}))
				defer server.Close()
				// Unknown provider has no environment credential fallback.
				model := &goai.Model{ID: "deployment", Api: api, Provider: goai.Provider("keyless-test"), BaseURL: server.URL}
				key, want := "Authorization", "Bearer configured-key"
				if api == goai.ApiAzureOpenAIResponses {
					key, want = "api-key", "configured-key"
				}
				opts := &goai.StreamOptions{APIKey: "configured-key"}
				if mode != "generated" {
					model.Headers = map[string]string{key: "model-value"}
					want = "model-value"
				}
				if mode == "caller" {
					opts.Headers = map[string]string{key: "caller-value"}
					want = "caller-value"
				}
				if mode == "header_only" {
					opts.APIKey = ""
				}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				done := 0
				for event := range streamResponses(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, opts) {
					switch e := event.(type) {
					case *goai.ErrorEvent:
						t.Error(e.Err)
					case *goai.DoneEvent:
						done++
					}
				}
				if done != 1 {
					t.Fatal("missing completion", done)
				}
				var headers http.Header
				select {
				case headers = <-captured:
				case <-ctx.Done():
					t.Fatal("missing authenticated request")
				}
				if headers.Get(key) != want {
					t.Fatal("SDK auth precedence", headers, key, want)
				}
				if api == goai.ApiAzureOpenAIResponses && headers.Get("Authorization") != "" {
					t.Fatal("static Azure key incorrectly sent as Bearer", headers)
				}
			})
		}
	}
}

func TestAzureAPIKeyRedirectDoesNotForwardCredentialsMain(t *testing.T) {
	for _, headerOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(headerOnly), func(t *testing.T) {
			forwarded := make(chan http.Header, 1)
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded <- r.Header.Clone(); w.WriteHeader(500) }))
			defer target.Close()
			seen := make(chan http.Header, 1)
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen <- r.Header.Clone()
				http.Redirect(w, r, target.URL+"/responses", http.StatusTemporaryRedirect)
			}))
			defer source.Close()
			model := &goai.Model{ID: "deployment", Api: goai.ApiAzureOpenAIResponses, Provider: goai.Provider("keyless-test"), BaseURL: source.URL}
			opts := &goai.StreamOptions{APIKey: "private-test-key"}
			if headerOnly {
				opts.APIKey = ""
				opts.Headers = map[string]string{"api-key": "private-test-key"}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			errors, done := 0, 0
			for event := range streamResponses(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, opts) {
				switch event.(type) {
				case *goai.ErrorEvent:
					errors++
				case *goai.DoneEvent:
					done++
				}
			}
			if errors != 1 || done != 0 {
				t.Fatal("redirect must fail without following", errors, done)
			}
			select {
			case headers := <-seen:
				if headers.Get("api-key") != "private-test-key" {
					t.Fatal("source missing Azure key")
				}
			default:
				t.Fatal("no initial request")
			}
			select {
			case headers := <-forwarded:
				t.Fatal("Azure key request followed redirect", headers)
			default:
			}
		})
	}
}

func TestAzureAPIKeyHeaderSuppressionPreflightMain(t *testing.T) {
	model := &goai.Model{Api: goai.ApiAzureOpenAIResponses}
	headers := map[string]string{"API-Key": "value"}
	if !hasResponsesAuthHeader(model, headers) {
		t.Fatal("explicit Azure key rejected")
	}
	if hasResponsesAuthHeader(&goai.Model{Api: goai.ApiOpenAIResponses}, headers) {
		t.Fatal("Azure key accepted for ordinary Responses")
	}
	if hasResponsesAuthHeader(model, goai.MergeProviderHeaders(headers, nil, []string{"api-key"})) {
		t.Fatal("suppressed key still authorizes preflight")
	}
}
