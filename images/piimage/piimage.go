// Package piimage registers the pi-image JSON image-generation API.
package piimage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
)

const label = "Pi Image"

func init() {
	images.RegisterImagesApiProvider(&images.ImagesApiProvider{Api: images.ImagesApiPiImage, GenerateImages: GenerateImages})
}

// GenerateImages calls the canonical pi-image backend and returns provider
// failures in the result, as required by images.ImagesFunction.
func GenerateImages(model *images.ImagesModel, input images.ImagesContext, opts *images.ImagesOptions) (*images.AssistantImages, error) {
	if opts == nil {
		opts = &images.ImagesOptions{}
	}
	ctx := opts.Context
	if ctx == nil {
		ctx = opts.Signal
	}
	if ctx == nil {
		ctx = context.Background()
	}
	out := &images.AssistantImages{Api: model.Api, Provider: model.Provider, Model: model.ID, Output: []images.ImageOutput{}, StopReason: goai.StopReasonStop, Timestamp: time.Now().UnixMilli()}
	fail := func(err error) (*images.AssistantImages, error) {
		out.StopReason = goai.StopReasonError
		if ctx.Err() != nil {
			out.StopReason = goai.StopReasonAborted
		}
		out.ErrorMessage = goai.FormatProviderError(goai.NormalizeProviderError(err), label+" error")
		return out, nil
	}
	if model.Api != images.ImagesApiPiImage {
		//lint:ignore ST1005 pinned upstream diagnostic.
		return fail(fmt.Errorf("Unsupported image API: %s", model.Api))
	}
	if !slices.Contains(model.Input, "image") {
		for _, block := range input.Input {
			if block.Type == "image" {
				//lint:ignore ST1005 pinned upstream diagnostic.
				return fail(fmt.Errorf("Pi Image model %s does not support image input", model.ID))
			}
		}
	}
	base, err := url.Parse(strings.TrimRight(model.BaseURL, "/") + "/")
	if err != nil {
		return fail(err)
	}
	target := base.ResolveReference(&url.URL{Path: "image"})
	key := opts.APIKey
	if key == "" {
		key = goai.GetEnvAPIKeyWithEnv(goai.Provider(model.Provider), opts.Env)
	}
	if key == "" {
		//lint:ignore ST1005 pinned upstream diagnostic.
		return fail(fmt.Errorf("No API key for provider: %s", model.Provider))
	}

	if input.Input == nil {
		input.Input = []images.ImageInput{}
	}
	payload := map[string]any{"model": model.ID, "context": map[string]any{"input": input.Input}}
	wire := map[string]any{}
	if opts.AspectRatio != nil {
		wire["aspectRatio"] = *opts.AspectRatio
	}
	if opts.Size != nil {
		wire["size"] = *opts.Size
	}
	if opts.SessionID != nil {
		wire["sessionId"] = *opts.SessionID
	}
	if opts.Metadata != nil {
		wire["metadata"] = opts.Metadata
	}
	if len(wire) != 0 {
		payload["options"] = wire
	}
	if opts.OnPayloadWithTelemetry != nil {
		next, err := opts.OnPayloadWithTelemetry(payload, model, opts.Telemetry)
		if err != nil {
			return fail(err)
		}
		if next != nil {
			payload = next
		}
	}
	if opts.OnPayload != nil {
		next, err := opts.OnPayload(payload, model)
		if err != nil {
			return fail(err)
		}
		if next != nil {
			payload = next
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fail(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return fail(err)
	}
	goai.ApplyHeaders(req.Header, goai.GetIdentityHeaders(goai.ClientIdentityTarget{Provider: string(model.Provider), BaseURL: model.BaseURL}, opts.Identity))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	goai.ApplyHeaders(req.Header, model.Headers)
	goai.ApplyHeaders(req.Header, opts.Headers)
	goai.SuppressHeaders(req.Header, opts.SuppressHeaders)

	cfg := goai.DefaultRetryConfig()
	cfg.MaxRetries = 2
	if opts.MaxRetriesSet || opts.MaxRetries != 0 {
		cfg.MaxRetries = opts.MaxRetries
	}
	if opts.MaxRetryDelayMs != 0 {
		cfg.MaxRetryDelayMs = opts.MaxRetryDelayMs
	}
	cfg.RequestTimeout = opts.Timeout
	if cfg.RequestTimeout <= 0 && opts.TimeoutMs > 0 {
		cfg.RequestTimeout = time.Duration(opts.TimeoutMs) * time.Millisecond
	}
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := goai.DoObservedProviderJSONRequest(ctx, client, req, cfg, func(resp *http.Response) error {
		if opts.OnResponse == nil && opts.OnResponseWithTelemetry == nil {
			return nil
		}
		meta := images.ImagesResponseMetadata{Status: resp.StatusCode, Headers: make(map[string]string, len(resp.Header))}
		for name, values := range resp.Header {
			meta.Headers[strings.ToLower(name)] = strings.Join(values, ", ")
		}
		if opts.OnResponseWithTelemetry != nil {
			if err := opts.OnResponseWithTelemetry(meta, model, opts.Telemetry); err != nil {
				return err
			}
		}
		if opts.OnResponse != nil {
			return opts.OnResponse(meta, model)
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
		if err != nil {
			return fail(err)
		}
		if len(data) > 1<<20 {
			return fail(errors.New("provider error response exceeds 1 MiB"))
		}
		return fail(responseError{status: resp.StatusCode, body: string(data)})
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fail(err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return fail(err)
	}
	result, ok := value.(map[string]any)
	if !ok {
		//lint:ignore ST1005 pinned upstream diagnostic.
		return fail(errors.New("Pi Image returned an unexpected response"))
	}
	out.ResponseID, _ = result["responseId"].(string)
	out.Usage = parseUsage(result["usage"], model)
	blocks, err := parseOutput(result["output"])
	if err != nil {
		return fail(err)
	}
	out.Output = blocks
	return out, nil
}

type responseError struct {
	status int
	body   string
}

func (e responseError) Error() string                    { return fmt.Sprintf("%s returned %d", label, e.status) }
func (e responseError) ProviderErrorStatus() (int, bool) { return e.status, true }
func (e responseError) ProviderErrorBody() (any, bool)   { return e.body, true }

func parseOutput(value any) ([]images.ImageOutput, error) {
	items, ok := value.([]any)
	if !ok {
		//lint:ignore ST1005 pinned upstream diagnostic.
		return nil, errors.New("Pi Image returned an unexpected response")
	}
	out := make([]images.ImageOutput, 0, len(items))
	for i, value := range items {
		item, ok := value.(map[string]any)
		if !ok {
			//lint:ignore ST1005 pinned upstream diagnostic.
			return nil, fmt.Errorf("Pi Image returned an invalid output block at index %d", i)
		}
		switch item["type"] {
		case "text":
			text, ok := item["text"].(string)
			if !ok {
				//lint:ignore ST1005 pinned upstream diagnostic.
				return nil, fmt.Errorf("Pi Image returned an invalid text block at index %d", i)
			}
			out = append(out, images.ImageOutput{Type: "text", Text: text})
		case "image":
			data, _ := item["data"].(string)
			mime, _ := item["mimeType"].(string)
			if data == "" || mime == "" {
				//lint:ignore ST1005 pinned upstream diagnostic.
				return nil, fmt.Errorf("Pi Image returned an invalid image block at index %d", i)
			}
			out = append(out, images.ImageOutput{Type: "image", Data: data, MimeType: mime})
		default:
			//lint:ignore ST1005 pinned upstream diagnostic.
			return nil, fmt.Errorf("Pi Image returned an unknown output block type at index %d", i)
		}
	}
	return out, nil
}

func parseUsage(value any, model *images.ImagesModel) *goai.Usage {
	usage, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	cost, _ := usage["cost"].(map[string]any)
	_, hasCost := cost["total"].(float64)
	_, hasInput := usage["input"].(float64)
	_, hasOutput := usage["output"].(float64)
	if !hasCost && !hasInput && !hasOutput {
		return nil
	}
	u := new(goai.Usage)
	u.Input, _ = counter(usage["input"])
	u.Output, _ = counter(usage["output"])
	u.CacheRead, _ = counter(usage["cacheRead"])
	u.CacheWrite, _ = counter(usage["cacheWrite"])
	u.Reasoning, u.ReasoningSet = counter(usage["reasoning"])
	u.ImageOutput, u.ImageOutputSet = counter(usage["imageOutput"])
	if total, ok := counter(usage["totalTokens"]); ok {
		u.TotalTokens = total
	} else {
		for _, n := range [...]int{u.Input, u.Output, u.CacheRead, u.CacheWrite} {
			u.TotalTokens += min(n, math.MaxInt-u.TotalTokens)
		}
	}
	if hasCost {
		u.Cost = goai.CostBreakdown{Input: amount(cost["input"]), Output: amount(cost["output"]), CacheRead: amount(cost["cacheRead"]), CacheWrite: amount(cost["cacheWrite"]), Total: amount(cost["total"])}
		if billed, ok := cost["totalFromProvider"].(bool); ok {
			u.Cost.TotalFromProvider = &billed
		}
	} else {
		u.Cost = goai.CalculateCost(&goai.Model{Cost: model.Cost}, u)
		u.Cost.TotalFromProvider = nil
	}
	return u
}

func counter(value any) (int, bool) {
	n, ok := value.(float64)
	if !ok || n < 0 || n >= math.Ldexp(1, strconv.IntSize-1) || math.Trunc(n) != n {
		return 0, false
	}
	return int(n), true
}

func amount(value any) float64 {
	n, _ := value.(float64)
	return max(0, n)
}
