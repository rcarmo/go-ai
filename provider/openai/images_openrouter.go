package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// GenerateImagesOpenRouter mirrors upstream's openrouter-images provider: it
// sends an OpenAI-compatible chat completions request with image modalities and
// extracts data: URL image outputs from choice.message.images.
func GenerateImagesOpenRouter(model *goai.ImagesModel, ictx goai.ImagesContext, opts *goai.ImagesOptions) (*goai.AssistantImages, error) {
	out := &goai.AssistantImages{Api: model.Api, Provider: model.Provider, Model: model.ID, StopReason: goai.StopReasonStop, Timestamp: time.Now().UnixMilli()}
	apiKey := ""
	if opts != nil {
		apiKey = opts.APIKey
	}
	if apiKey == "" {
		apiKey = goai.GetEnvAPIKey(goai.Provider(model.Provider))
	}
	if apiKey == "" {
		out.StopReason = goai.StopReasonError
		out.ErrorMessage = fmt.Sprintf("No API key available for provider: %s", model.Provider)
		return out, nil
	}

	payload := buildImagesPayload(model, ictx)
	if opts != nil && opts.OnPayload != nil {
		next, err := opts.OnPayload(payload, model)
		if err != nil {
			out.StopReason = goai.StopReasonError
			out.ErrorMessage = err.Error()
			return out, nil
		}
		if next != nil {
			payload = next
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		out.StopReason = goai.StopReasonError
		out.ErrorMessage = err.Error()
		return out, nil
	}

	client := http.DefaultClient
	if opts != nil && opts.HTTPClient != nil {
		client = opts.HTTPClient
	}
	ctx := context.Background()
	if opts != nil {
		if opts.Signal != nil {
			ctx = opts.Signal
		}
		if opts.Context != nil {
			ctx = opts.Context
		}
	}
	if timeout := imageTimeout(opts); timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	maxRetries := 0
	if opts != nil && opts.MaxRetries > 0 {
		maxRetries = opts.MaxRetries
	}
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				out.StopReason = goai.StopReasonAborted
				out.ErrorMessage = ctx.Err().Error()
				return out, nil
			case <-time.After(time.Duration(attempt) * 250 * time.Millisecond):
			}
		}
		result, retry, err := doOpenRouterImageRequest(ctx, client, model, opts, apiKey, body)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !retry {
			break
		}
	}
	if ctx.Err() != nil {
		out.StopReason = goai.StopReasonAborted
		out.ErrorMessage = ctx.Err().Error()
	} else {
		out.StopReason = goai.StopReasonError
		out.ErrorMessage = lastErr.Error()
	}
	return out, nil
}

func buildImagesPayload(model *goai.ImagesModel, ictx goai.ImagesContext) map[string]any {
	content := make([]any, 0, len(ictx.Input))
	for _, in := range ictx.Input {
		if in.Type == "text" {
			content = append(content, map[string]any{"type": "text", "text": in.Text})
		} else if in.Type == "image" {
			content = append(content, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + in.MimeType + ";base64," + in.Data}})
		}
	}
	modes := []string{"image"}
	for _, o := range model.Output {
		if o == "text" {
			modes = []string{"image", "text"}
			break
		}
	}
	return map[string]any{
		"model":      model.ID,
		"messages":   []any{map[string]any{"role": "user", "content": content}},
		"stream":     false,
		"modalities": modes,
	}
}

func doOpenRouterImageRequest(ctx context.Context, client *http.Client, model *goai.ImagesModel, opts *goai.ImagesOptions, apiKey string, body []byte) (*goai.AssistantImages, bool, error) {
	out := &goai.AssistantImages{Api: model.Api, Provider: model.Provider, Model: model.ID, StopReason: goai.StopReasonStop, Timestamp: time.Now().UnixMilli()}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(model.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range model.Headers {
		req.Header.Set(k, v)
	}
	if opts != nil {
		for k, v := range opts.Headers {
			req.Header.Set(k, v)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if opts != nil && opts.OnResponse != nil {
		if err := opts.OnResponse(goai.ImagesResponseMetadata{Status: resp.StatusCode, Headers: headersToMap(resp.Header)}, model); err != nil {
			return nil, false, err
		}
	}
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return nil, true, fmt.Errorf("openrouter images request failed: status %d: %s", resp.StatusCode, string(raw))
	}
	if resp.StatusCode >= 300 {
		out.StopReason = goai.StopReasonError
		out.ErrorMessage = string(raw)
		return out, false, nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, false, err
	}
	parseOpenRouterImageResponse(decoded, model, out)
	return out, false, nil
}

func parseOpenRouterImageResponse(decoded map[string]any, model *goai.ImagesModel, out *goai.AssistantImages) {
	if id, ok := decoded["id"].(string); ok {
		out.ResponseID = id
	}
	if usage, ok := decoded["usage"].(map[string]any); ok {
		out.Usage = parseOpenRouterImageUsage(usage, model)
	}
	choices, ok := decoded["choices"].([]any)
	if !ok || len(choices) == 0 {
		return
	}
	c, ok := choices[0].(map[string]any)
	if !ok {
		return
	}
	msg, ok := c["message"].(map[string]any)
	if !ok {
		return
	}
	if txt, ok := msg["content"].(string); ok && txt != "" {
		out.Output = append(out.Output, goai.ImageOutput{Type: "text", Text: txt})
	}
	imgs, ok := msg["images"].([]any)
	if !ok {
		return
	}
	for _, it := range imgs {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		var u string
		if uu, ok := m["image_url"].(map[string]any); ok {
			u, _ = uu["url"].(string)
		} else {
			u, _ = m["image_url"].(string)
		}
		if !strings.HasPrefix(u, "data:") {
			continue
		}
		parts := strings.SplitN(strings.TrimPrefix(u, "data:"), ";base64,", 2)
		if len(parts) == 2 {
			out.Output = append(out.Output, goai.ImageOutput{Type: "image", MimeType: parts[0], Data: parts[1]})
		}
	}
}

func parseOpenRouterImageUsage(raw map[string]any, model *goai.ImagesModel) *goai.Usage {
	prompt := intFromAny(raw["prompt_tokens"])
	completion := intFromAny(raw["completion_tokens"])
	cached, cacheWrite := 0, 0
	if details, ok := raw["prompt_tokens_details"].(map[string]any); ok {
		cached = intFromAny(details["cached_tokens"])
		cacheWrite = intFromAny(details["cache_write_tokens"])
	}
	cacheRead := cached
	if cacheWrite > 0 {
		cacheRead = max(0, cached-cacheWrite)
	}
	input := max(0, prompt-cacheRead-cacheWrite)
	u := &goai.Usage{Input: input, Output: completion, CacheRead: cacheRead, CacheWrite: cacheWrite, TotalTokens: input + completion + cacheRead + cacheWrite}
	u.Cost.Input = model.Cost.Input / 1_000_000 * float64(input)
	u.Cost.Output = model.Cost.Output / 1_000_000 * float64(completion)
	u.Cost.CacheRead = model.Cost.CacheRead / 1_000_000 * float64(cacheRead)
	u.Cost.CacheWrite = model.Cost.CacheWrite / 1_000_000 * float64(cacheWrite)
	u.Cost.Total = u.Cost.Input + u.Cost.Output + u.Cost.CacheRead + u.Cost.CacheWrite
	return u
}

func imageTimeout(opts *goai.ImagesOptions) time.Duration {
	if opts == nil {
		return 0
	}
	if opts.Timeout > 0 {
		return opts.Timeout
	}
	if opts.TimeoutMs > 0 {
		return time.Duration(opts.TimeoutMs) * time.Millisecond
	}
	return 0
}

func headersToMap(h http.Header) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		out[k] = strings.Join(v, ", ")
	}
	return out
}

func intFromAny(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case json.Number:
		i, _ := x.Int64()
		return int(i)
	default:
		return 0
	}
}
