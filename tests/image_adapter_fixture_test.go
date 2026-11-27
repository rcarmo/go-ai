package goai_test

import (
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
)

type imageRoundTripFunc func(*http.Request) (*http.Response, error)

func (f imageRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func imageJSONResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func generateImagesWithTransport(t *testing.T, model *images.ImagesModel, input images.ImagesContext, opts *images.ImagesOptions, transport imageRoundTripFunc) *images.AssistantImages {
	t.Helper()
	opts.HTTPClient = &http.Client{Transport: transport}
	if opts.Context == nil {
		opts.Context = t.Context()
	}
	got, err := images.GenerateImages(model, input, opts)
	if err != nil || got == nil {
		t.Fatalf("GenerateImages = %#v, %v", got, err)
	}
	return got
}

func assertImageResultEmpty(t *testing.T, got *images.AssistantImages, reason goai.StopReason) {
	t.Helper()
	if got.StopReason != reason || !reflect.DeepEqual(got.Output, []images.ImageOutput{}) {
		t.Errorf("result=%+v; want %s and empty array", got, reason)
	}
}
