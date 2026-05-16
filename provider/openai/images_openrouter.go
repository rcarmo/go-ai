package openai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	goai "github.com/rcarmo/go-ai"
)

type chatReq struct {
	Model      string   `json:"model"`
	Messages   []any    `json:"messages"`
	Stream     bool     `json:"stream"`
	Modalities []string `json:"modalities,omitempty"`
}

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
	reqObj := chatReq{Model: model.ID, Messages: []any{map[string]any{"role": "user", "content": content}}, Stream: false, Modalities: modes}
	body, _ := json.Marshal(reqObj)
	req, _ := http.NewRequest("POST", strings.TrimRight(model.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
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
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		out.StopReason = goai.StopReasonError
		out.ErrorMessage = err.Error()
		return out, nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		out.StopReason = goai.StopReasonError
		out.ErrorMessage = string(raw)
		return out, nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		out.StopReason = goai.StopReasonError
		out.ErrorMessage = err.Error()
		return out, nil
	}
	if id, ok := decoded["id"].(string); ok {
		out.ResponseID = id
	}
	if choices, ok := decoded["choices"].([]any); ok && len(choices) > 0 {
		if c, ok := choices[0].(map[string]any); ok {
			if msg, ok := c["message"].(map[string]any); ok {
				if txt, ok := msg["content"].(string); ok && txt != "" {
					out.Output = append(out.Output, goai.ImageOutput{Type: "text", Text: txt})
				}
				if imgs, ok := msg["images"].([]any); ok {
					for _, it := range imgs {
						m, ok := it.(map[string]any)
						if !ok {
							continue
						}
						u, _ := m["image_url"].(string)
						if uu, ok := m["image_url"].(map[string]any); ok {
							u, _ = uu["url"].(string)
						}
						if strings.HasPrefix(u, "data:") {
							parts := strings.SplitN(strings.TrimPrefix(u, "data:"), ";base64,", 2)
							if len(parts) == 2 {
								if _, e := base64.StdEncoding.DecodeString(parts[1]); e == nil {
									out.Output = append(out.Output, goai.ImageOutput{Type: "image", MimeType: parts[0], Data: parts[1]})
								}
							}
						}
					}
				}
			}
		}
	}
	return out, nil
}
