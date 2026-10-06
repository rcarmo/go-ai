//go:build unix

package tools

import (
	"context"
	"errors"
	"fmt"
	"github.com/rcarmo/go-ai/durable"
	"strings"
)

func executePortableBash(ctx context.Context, fs durable.FileSystem, shell durable.Shell, args durable.JSON, api *durable.ToolAPI) (durable.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return durable.ToolResult{}, err
	}
	command, ok := args["command"].(string)
	if !ok || strings.TrimSpace(command) == "" {
		return durable.ToolResult{}, errors.New("command must be a nonempty string")
	}
	_, seconds, err := bashTimeoutArg(args, "timeout", DefaultBashTimeout)
	if err != nil {
		return durable.ToolResult{}, err
	}
	cwd := fs.Cwd()
	if value, exists := args["cwd"]; exists && value != nil {
		path, ok := value.(string)
		if !ok {
			return durable.ToolResult{}, errors.New("cwd must be a string")
		}
		cwd, err = fs.AbsolutePath(ctx, path)
		if err != nil {
			return durable.ToolResult{}, err
		}
	}
	var capture *bashCapture
	if api == nil {
		capture = &bashCapture{}
	}
	streamer := newBashStreamer(api)
	var timeout *float64
	if seconds > 0 {
		timeout = &seconds
	}
	inherit := true
	if value, ok := args["inheritEnv"].(bool); ok {
		inherit = value
	}
	environment, _ := args["executionEnv"].(map[string]string)
	executed, err := shell.Exec(ctx, command, durable.ShellExecOptions{Cwd: cwd, Env: environment, InheritEnv: &inherit, Timeout: timeout, Spill: &durable.ShellSpillOptions{AfterBytes: MaxReadBytes, AfterLines: MaxReadLines}, OnOutput: func(_ context.Context, text string) error {
		if capture != nil {
			capture.append([]byte(text))
		}
		streamer.write([]byte(text))
		return nil
	}})
	// Harness output owns the decoded tail; standalone calls need their own
	// bounded capture. Omitted content inherits progress without copying it.
	result := durable.ToolResult{}
	if capture != nil {
		result = capture.result()
	}
	if result.Details == nil {
		result.Details = durable.JSON{}
	}
	result.Details["cwd"] = cwd
	if seconds > 0 {
		result.Details["timeoutSeconds"] = seconds
	}
	result.Details["exitCode"] = executed.ExitCode
	if executed.SpillPath != "" {
		result.Details["fullOutputPath"] = executed.SpillPath
		if err := reportBashSpill(api, &result, executed.SpillPath); err != nil {
			return result, err
		}
	}
	if err != nil {
		var failure *durable.ExecutionError
		if errors.As(err, &failure) {
			if failure.SpillPath != "" && executed.SpillPath == "" {
				result.Details["fullOutputPath"] = failure.SpillPath
				if reportErr := reportBashSpill(api, &result, failure.SpillPath); reportErr != nil {
					return result, reportErr
				}
			}
			switch failure.Code {
			case "timeout":
				return result, &bashCommandError{message: fmt.Sprintf("Command timed out after %s seconds", formatTimeoutSeconds(seconds)), cause: err}
			case "aborted":
				if ctx.Err() != nil {
					return result, err
				}
				return result, &bashCommandError{message: "Command aborted", cause: err}
			}
		}
		return result, err
	}
	if executed.ExitCode != 0 {
		//lint:ignore ST1005 Preserve reference model-visible diagnostic wording.
		return result, fmt.Errorf("Command exited with code %d", executed.ExitCode)
	}
	return result, nil
}
