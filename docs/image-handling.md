# Image Handling

## Sending images to the model

Images are sent as base64-encoded content blocks within user messages:

```go
import (
    "encoding/base64"
    "os"
)

// Read and encode an image
imageData, _ := os.ReadFile("screenshot.png")
b64 := base64.StdEncoding.EncodeToString(imageData)

ctx := &goai.Context{
    Messages: []goai.Message{
        {
            Role: goai.RoleUser,
            Content: []goai.ContentBlock{
                {Type: "text", Text: "What's in this image?"},
                {Type: "image", Data: b64, MimeType: "image/png"},
            },
        },
    },
}

msg, _ := goai.Complete(context.Background(), model, ctx, nil)
```

## Supported image formats

| Format | MIME type | OpenAI | Anthropic | Google | Bedrock |
|---|---|---|---|---|---|
| PNG | `image/png` | ✅ | ✅ | ✅ | ✅ |
| JPEG | `image/jpeg` | ✅ | ✅ | ✅ | ✅ |
| GIF | `image/gif` | ✅ | ✅ | ✅ | ✅ |
| WebP | `image/webp` | ✅ | ✅ | ✅ | ✅ |

## Multiple images

Send multiple images in a single message:

```go
{
    Role: goai.RoleUser,
    Content: []goai.ContentBlock{
        {Type: "text", Text: "Compare these two screenshots:"},
        {Type: "image", Data: image1B64, MimeType: "image/png"},
        {Type: "image", Data: image2B64, MimeType: "image/png"},
    },
}
```

## Images in tool results

When a tool produces images (e.g., screenshots, charts), include them in the tool result. go-ai converts them to the appropriate format per provider:

```go
// Tool result with image
ctx.Messages = append(ctx.Messages, goai.Message{
    Role:       goai.RoleToolResult,
    ToolCallID: tc.ID,
    ToolName:   tc.Name,
    Content: []goai.ContentBlock{
        {Type: "text", Text: "Screenshot captured"},
        {Type: "image", Data: screenshotB64, MimeType: "image/png"},
    },
})
```

## Provider wire formats

go-ai handles the format differences automatically:

| Provider | User images | Tool result images |
|---|---|---|
| **OpenAI** | `image_url` with data URI | Follow-up user message |
| **Anthropic** | `source.type: "base64"` | In tool result content |
| **Google** | `inlineData` | In function response (Gemini 3+) or separate user turn |
| **Bedrock** | `ImageBlock` with bytes | In tool result content |
| **Mistral** | Not supported | Not supported |

## Non-vision models

When a model doesn't support images (`model.Input` doesn't include `"image"`), go-ai's `TransformMessages()` automatically replaces image blocks with a text placeholder:

```
(image omitted: model does not support images)
```

For tool results:
```
(tool image omitted: model does not support images)
```

This means you can include images in your context even when switching between vision and non-vision models — the images are silently downgraded.

## Checking model vision support

```go
supportsImages := false
for _, input := range model.Input {
    if input == "image" {
        supportsImages = true
        break
    }
}
```

## Image size considerations

- Most providers have a maximum image size (usually 20 MB base64-encoded)
- Large images increase token usage (OpenAI: ~85 tokens per 512×512 tile)
- Consider resizing images before encoding to control costs
- PNG screenshots compress well; photos are better as JPEG
