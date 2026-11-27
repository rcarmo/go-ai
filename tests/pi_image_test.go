package goai_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
	_ "github.com/rcarmo/go-ai/images/piimage"
)

func piImageFixtureModel() *images.ImagesModel {
	return &images.ImagesModel{ID: "nano-banana", Name: "Nano Banana", Api: "pi-image", Provider: "radius", BaseURL: "https://radius.example/v1", Input: []string{"text", "image"}, Output: []string{"image", "text"}, Cost: goai.ModelCost{Input: .3, Output: 2.5, CacheRead: .03, CacheWrite: .083333}}
}

func piImageFixtureInput() images.ImagesContext {
	return images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "A red fox in the snow"}}}
}

func TestPiImageCanonicalRequest(t *testing.T) {
	const response = `{"output":[{"type":"text","text":"Here is your fox."},{"type":"image","data":"iVBORw0KGgo=","mimeType":"image/png"}],"usage":{"input":15,"output":1290,"cacheRead":0,"cacheWrite":0,"reasoning":200,"imageOutput":1000,"totalTokens":1305,"cost":{"input":0.0000045,"output":0.003225,"cacheRead":0,"cacheWrite":0,"total":0.0032295,"totalFromProvider":true}},"responseId":"gen-img-1"}`
	model, input := piImageFixtureModel(), piImageFixtureInput()
	var method, path, bearer string
	var requests int
	var payload any
	got := generateImagesWithTransport(t, model, input, &images.ImagesOptions{APIKey: "radius-key", AspectRatio: new("16:9"), Size: new("2K"), SessionID: new("sess-1"), Metadata: map[string]any{"tag": "demo"}}, func(req *http.Request) (*http.Response, error) {
		defer req.Body.Close()
		requests++
		method, path, bearer = req.Method, req.URL.String(), req.Header.Get("Authorization")
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			return nil, err
		}
		return imageJSONResponse(200, response), nil
	})
	if requests != 1 || path != "https://radius.example/v1/image" || method != "POST" || bearer != "Bearer radius-key" {
		t.Errorf("request count=%d path=%q method=%q authorization=%q", requests, path, method, bearer)
	}
	wantPayload := parsePiImageFixture(t, []byte(`{"model":"nano-banana","context":{"input":[{"type":"text","text":"A red fox in the snow"}]},"options":{"aspectRatio":"16:9","size":"2K","sessionId":"sess-1","metadata":{"tag":"demo"}}}`))
	if !reflect.DeepEqual(payload, wantPayload) {
		t.Errorf("payload=%#v; want %#v", payload, wantPayload)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wire := parsePiImageFixture(t, b)
	delete(wire, "timestamp")
	want := parsePiImageFixture(t, []byte(response))
	want["api"], want["provider"], want["model"], want["stopReason"] = "pi-image", "radius", "nano-banana", "stop"
	if !reflect.DeepEqual(wire, want) {
		t.Errorf("result=%s; want %#v", b, want)
	}
}

func parsePiImageFixture(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPiImageCalculatedCost(t *testing.T) {
	got := generateImagesWithTransport(t, piImageFixtureModel(), piImageFixtureInput(), &images.ImagesOptions{APIKey: "radius-key"}, func(req *http.Request) (*http.Response, error) {
		defer req.Body.Close()
		return imageJSONResponse(200, `{"output":[{"type":"image","data":"AAAA","mimeType":"image/png"}],"usage":{"input":1000,"output":2000,"cacheRead":0,"cacheWrite":0,"totalTokens":3000}}`), nil
	})
	if got.StopReason != goai.StopReasonStop || got.Usage == nil {
		t.Fatalf("result=%+v", got)
	}
	if math.Abs(got.Usage.Cost.Input-.0003) > 1e-15 || math.Abs(got.Usage.Cost.Output-.005) > 1e-15 || math.Abs(got.Usage.Cost.Total-.0053) > 1e-15 {
		t.Errorf("calculated cost=%+v", got.Usage.Cost)
	}
	if got.Usage.Cost.TotalFromProvider == nil || *got.Usage.Cost.TotalFromProvider {
		t.Errorf("catalog estimate provenance = %+v; want explicit false", got.Usage.Cost)
	}
	b, err := json.Marshal(got.Usage)
	if err != nil {
		t.Fatal(err)
	}
	usage := parsePiImageFixture(t, b)
	if _, ok := usage["reasoning"]; ok {
		t.Error("reasoning presence was invented")
	}
	if _, ok := usage["imageOutput"]; ok {
		t.Error("imageOutput presence was invented")
	}
}

func TestPiImageCostOnly(t *testing.T) {
	got := generateImagesWithTransport(t, piImageFixtureModel(), piImageFixtureInput(), &images.ImagesOptions{APIKey: "radius-key"}, func(req *http.Request) (*http.Response, error) {
		defer req.Body.Close()
		return imageJSONResponse(200, `{"output":[{"type":"image","data":"AAAA","mimeType":"image/png"}],"usage":{"cost":{"input":0,"output":0.04,"cacheRead":0,"cacheWrite":0,"total":0.04,"totalFromProvider":true}}}`), nil
	})
	if got.StopReason != goai.StopReasonStop || got.Usage == nil {
		t.Fatalf("result=%+v", got)
	}
	if got.Usage.Cost.Total != .04 || got.Usage.Cost.TotalFromProvider == nil || !*got.Usage.Cost.TotalFromProvider {
		t.Errorf("provider cost=%+v", got.Usage.Cost)
	}
}

func TestPiImageHTTPError(t *testing.T) {
	got := generateImagesWithTransport(t, piImageFixtureModel(), piImageFixtureInput(), &images.ImagesOptions{APIKey: "radius-key", MaxRetriesSet: true}, func(req *http.Request) (*http.Response, error) {
		defer req.Body.Close()
		return imageJSONResponse(402, `{"error":{"message":"Insufficient credits","code":"insufficient_credits"}}`), nil
	})
	assertImageResultEmpty(t, got, goai.StopReasonError)
	if !strings.Contains(got.ErrorMessage, "Insufficient credits") {
		t.Errorf("message=%q", got.ErrorMessage)
	}
}

type brokenPiImageBody struct{ beforeRead func() }

func (b brokenPiImageBody) Read([]byte) (int, error) {
	b.beforeRead()
	return 0, errors.New("body stream failed")
}
func (brokenPiImageBody) Close() error { return nil }

func TestPiImageResponseHookBeforeBody(t *testing.T) {
	var observed []images.ImagesResponseMetadata
	got := generateImagesWithTransport(t, piImageFixtureModel(), piImageFixtureInput(), &images.ImagesOptions{APIKey: "radius-key", MaxRetriesSet: true, OnResponse: func(meta images.ImagesResponseMetadata, _ *images.ImagesModel) error {
		observed = append(observed, meta)
		return nil
	}}, func(req *http.Request) (*http.Response, error) {
		defer req.Body.Close()
		return &http.Response{StatusCode: 503, Header: http.Header{"Retry-After": {"7"}}, Body: brokenPiImageBody{func() {
			if len(observed) != 1 {
				t.Error("body read before observing headers")
			}
		}}}, nil
	})
	if got.StopReason != goai.StopReasonError {
		t.Errorf("result=%+v", got)
	}
	if len(observed) != 1 || observed[0].Status != 503 || observed[0].Headers["retry-after"] != "7" {
		t.Errorf("response observations=%#v", observed)
	}
}

func TestPiImageMalformedOutput(t *testing.T) {
	got := generateImagesWithTransport(t, piImageFixtureModel(), piImageFixtureInput(), &images.ImagesOptions{APIKey: "radius-key"}, func(req *http.Request) (*http.Response, error) {
		defer req.Body.Close()
		return imageJSONResponse(200, `{"output":[{"type":"image","data":"","mimeType":"image/png"}]}`), nil
	})
	assertImageResultEmpty(t, got, goai.StopReasonError)
	if !strings.Contains(got.ErrorMessage, "invalid image block") {
		t.Errorf("message=%q", got.ErrorMessage)
	}
}

func TestPiImageInputCapability(t *testing.T) {
	var sent any
	var requests int
	transport := func(req *http.Request) (*http.Response, error) {
		defer req.Body.Close()
		requests++
		if err := json.NewDecoder(req.Body).Decode(&sent); err != nil {
			return nil, err
		}
		return imageJSONResponse(200, `{"output":[{"type":"image","data":"BBBB","mimeType":"image/png"}]}`), nil
	}
	model := piImageFixtureModel()
	input := images.ImagesContext{Input: []images.ImageInput{{Type: "image", Data: "AAAA", MimeType: "image/png"}, {Type: "text", Text: "Make it blue"}}}
	ok := generateImagesWithTransport(t, model, input, &images.ImagesOptions{APIKey: "radius-key"}, transport)
	if ok.StopReason != goai.StopReasonStop || requests != 1 {
		t.Errorf("accepted=%+v requests=%d", ok, requests)
	}
	want := parsePiImageFixture(t, []byte(`{"model":"nano-banana","context":{"input":[{"type":"image","data":"AAAA","mimeType":"image/png"},{"type":"text","text":"Make it blue"}]}}`))
	if !reflect.DeepEqual(sent, want) {
		t.Errorf("forwarded=%#v", sent)
	}
	model.Input = []string{"text"}
	rejected := generateImagesWithTransport(t, model, input, &images.ImagesOptions{APIKey: "radius-key"}, transport)
	if rejected.StopReason != goai.StopReasonError || !strings.Contains(rejected.ErrorMessage, "does not support image input") || requests != 1 {
		t.Errorf("rejected=%+v requests=%d", rejected, requests)
	}
}

func TestPiImageAborted(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got := generateImagesWithTransport(t, piImageFixtureModel(), piImageFixtureInput(), &images.ImagesOptions{APIKey: "radius-key", Context: ctx}, func(req *http.Request) (*http.Response, error) {
		defer req.Body.Close()
		return nil, context.Canceled
	})
	assertImageResultEmpty(t, got, goai.StopReasonAborted)
}

func TestPiImageRetryAndOptionalZeroCounters(t *testing.T) {
	var statuses []int
	requests := 0
	opts := &images.ImagesOptions{APIKey: "radius-key", MaxRetries: 1, OnResponse: func(meta images.ImagesResponseMetadata, _ *images.ImagesModel) error {
		statuses = append(statuses, meta.Status)
		return nil
	}}
	got := generateImagesWithTransport(t, piImageFixtureModel(), piImageFixtureInput(), opts, func(req *http.Request) (*http.Response, error) {
		defer req.Body.Close()
		requests++
		if requests == 1 {
			resp := imageJSONResponse(503, `{"error":{"message":"try again"}}`)
			resp.Header.Set("Retry-After-Ms", "1")
			return resp, nil
		}
		return imageJSONResponse(200, `{"output":[],"usage":{"input":0,"output":0,"reasoning":0,"imageOutput":0}}`), nil
	})
	if requests != 2 || !reflect.DeepEqual(statuses, []int{503, 200}) || got.StopReason != goai.StopReasonStop {
		t.Fatalf("requests=%d statuses=%v result=%+v", requests, statuses, got)
	}
	wire, err := json.Marshal(got.Usage)
	if err != nil {
		t.Fatal(err)
	}
	usage := parsePiImageFixture(t, wire)
	if usage["reasoning"] != float64(0) || usage["imageOutput"] != float64(0) {
		t.Fatalf("explicit zero counters are not present: %s", wire)
	}
	var detached goai.Usage
	if err := json.Unmarshal(wire, &detached); err != nil || !detached.ImageOutputSet || detached.ImageOutput != 0 || detached.HasUnknownJSONFields() {
		t.Fatalf("explicit zero counter round-trip: %+v %v", detached, err)
	}
}
