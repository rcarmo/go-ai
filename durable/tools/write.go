package tools

import (
	"context"
	"encoding/json"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
)

var writeSchema = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to write (relative or absolute)"},"content":{"type":"string","description":"Content to write to the file"}},"required":["path","content"],"additionalProperties":false}`)

// Write returns a durable write tool registration.
func Write(env durable.FileSystem) durable.ToolRegistration {
	return durable.ToolRegistration{
		Definition: goai.Tool{
			Name:        "write",
			Description: "Write content to a file. Creates the file if it doesn't exist and overwrites if it does. Automatically creates parent directories.",
			Parameters:  writeSchema,
		},
		Implementation: "durable.tools.write",
		Version:        1,
		ReplaySafe:     false,
		Execute: func(ctx context.Context, args durable.JSON, api *durable.ToolAPI) (durable.ToolResult, error) {
			env, err := resolveToolEnv(ctx, env, api)
			if err != nil {
				return durable.ToolResult{}, err
			}
			path, ok := args["path"].(string)
			if !ok || path == "" {
				return durable.ToolResult{}, errors.New("path must be a string")
			}
			content, ok := args["content"].(string)
			if !ok {
				return durable.ToolResult{}, errors.New("content must be a string")
			}
			abs, err := resolveFSPath(ctx, env, path)
			if err != nil {
				return durable.ToolResult{}, err
			}
			if err := withFilesystemMutation(ctx, env, abs, func() error {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := env.WriteFile(ctx, abs, []byte(content)); err != nil {
					return err
				}
				return ctx.Err()
			}); err != nil {
				return durable.ToolResult{}, err
			}
			return durable.ToolResult{Content: "Successfully wrote to " + path, Details: durable.JSON{"path": path, "bytes": len([]byte(content))}}, nil
		},
	}
}
