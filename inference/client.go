package inference

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
)

// Client combines the native catalog runtime with identity-aware requests.
// Catalog operations and provider mutations delegate to the same embedded
// runtime; wrapping does not copy its locks, providers, or model store.
type Client struct {
	*goai.ModelRuntime
	identity *goai.ClientIdentity
}

func NewClient(models *goai.ModelRuntime, identity *goai.ClientIdentity) *Client {
	if models == nil {
		models = goai.NewModelRuntime(nil)
	}
	return &Client{ModelRuntime: models, identity: goai.MergeClientIdentity(identity)}
}

// WithClientIdentity adds fieldwise request defaults, preserving catalog/store
// identity and mutability. Per-request identity overrides these defaults.
func WithClientIdentity(client *Client, identity *goai.ClientIdentity) *Client {
	if client == nil {
		return NewClient(nil, identity)
	}
	return &Client{ModelRuntime: client.ModelRuntime, identity: goai.MergeClientIdentity(client.identity, identity)}
}

// Identity returns a detached snapshot of this client's request defaults.
func (c *Client) Identity() *goai.ClientIdentity { return goai.MergeClientIdentity(c.identity) }
func (c *Client) options(opts *StreamOptions) *StreamOptions {
	copy := StreamOptions{}
	if opts != nil {
		copy = *opts
	}
	copy.Identity = goai.MergeClientIdentity(c.identity, copy.Identity)
	return &copy
}
func (c *Client) Stream(ctx context.Context, model *Model, input *Context, opts *StreamOptions) <-chan Event {
	return goai.Stream(ctx, model, input, c.options(opts))
}
func (c *Client) Complete(ctx context.Context, model *Model, input *Context, opts *StreamOptions) (*Message, error) {
	return goai.Complete(ctx, model, input, c.options(opts))
}
func (c *Client) FetchDeferred(ctx context.Context, model *Model, handle goai.DeferredHandle, opts *StreamOptions) (*Message, error) {
	return goai.FetchDeferred(ctx, model, handle, c.options(opts))
}
func (c *Client) CancelDeferred(ctx context.Context, model *Model, handle goai.DeferredHandle, opts *StreamOptions) error {
	return goai.CancelDeferred(ctx, model, handle, c.options(opts))
}
func (c *Client) Classify(model *goai.ClassifierModel, input goai.ClassifierContext, opts *goai.ClassifierOptions) (*goai.ClassifierResult, error) {
	copy := goai.ClassifierOptions{}
	if opts != nil {
		copy = *opts
	}
	copy.Identity = goai.MergeClientIdentity(c.identity, copy.Identity)
	return goai.Classify(model, input, &copy)
}
func (c *Client) GenerateImages(model *images.ImagesModel, input images.ImagesContext, opts *images.ImagesOptions) (*images.AssistantImages, error) {
	copy := images.ImagesOptions{}
	if opts != nil {
		copy = *opts
	}
	copy.Identity = goai.MergeClientIdentity(c.identity, copy.Identity)
	return images.GenerateImages(model, input, &copy)
}
