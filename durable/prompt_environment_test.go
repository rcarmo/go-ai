package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestPromptRendererInvocationEnvironmentShownAndCommittedReader(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		renders := 0
		var escaped *InvocationReader
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			if input.SystemPrompt != "system\n\n<environment>\n/current\n</environment>" {
				t.Errorf("prompt capability result %q", input.SystemPrompt)
			}
			ch := make(chan goai.Event, 1)
			ch <- terminal("answer")
			close(ch)
			return ch
		})
		options.Sections = []PromptSection{{Key: "environment", Render: func(ctx context.Context, input PromptInput) (*string, error) {
			renders++
			escaped = input.Read
			if input.Env == nil || input.Env.Cwd() != "/current" || input.Read == nil {
				t.Error("prompt capabilities missing", input)
			}
			if _, err := input.Read.ContextView(ctx, input.Conversation, 0); err != nil {
				return nil, err
			}
			if renders == 2 && (len(input.Shown) != 1 || input.Shown[0].Key != "environment") {
				t.Error("shown sections missing", input.Shown)
			}
			text := input.Env.Cwd()
			return &text, nil
		}}}
		options.Env = func(context.Context, EnvTarget) (ExecutionEnvironment, error) {
			return testEnvironment{"/current"}, nil
		}
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		for i := 0; i < 2; i++ {
			sub, err := conversation.Submit(bg, Input{Content: "question"})
			if err != nil {
				t.Fatal(err)
			}
			waitSubmission(t, sub)
		}
		if _, err := escaped.ContextView(bg, conversation.ID(), 0); err != ErrSealed {
			t.Fatal("escaped prompt reader active", err)
		}
		if renders != 2 {
			t.Fatal(renders)
		}
	})
}
