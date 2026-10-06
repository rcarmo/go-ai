//go:build unix

package tools

import (
	"context"
	"encoding/json"
	"errors"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
)

type PowerShellOptions struct {
	CommandPrefix string
	Prepare       func(context.Context, *BashExecution, *durable.ToolAPI) error
	Programs      []string // nil defaults to pwsh, powershell; empty tries no program
}

var powershellSchema = json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"timeout":{"type":"number"}},"required":["command"],"additionalProperties":false}`)

// PowerShell runs direct argv through an injected ArgvShell. Local Unix hosts
// need an installed PowerShell program; no executable is downloaded by this tool.
func PowerShell(env durable.FileSystem, options ...PowerShellOptions) durable.ToolRegistration {
	config := PowerShellOptions{}
	if len(options) > 0 {
		config = options[0]
	}
	programs := append([]string{}, config.Programs...)
	if config.Programs == nil {
		programs = []string{"pwsh", "powershell"}
	}
	return durable.ToolRegistration{
		Definition:     goai.Tool{Name: "powershell", Description: "Execute a PowerShell command. Returns combined stdout and stderr, retaining the last 2000 lines or 50KB. Optionally provide a timeout in seconds.", Parameters: powershellSchema},
		Implementation: "durable.tools.powershell", Version: 1, ReplaySafe: false,
		OutputLimits: &durable.ToolOutputLimits{MaxBytes: MaxReadBytes, MaxLines: MaxReadLines, Retain: "tail"},
		Execute: func(ctx context.Context, args durable.JSON, api *durable.ToolAPI) (durable.ToolResult, error) {
			if _, _, err := bashTimeoutArg(args, "timeout", DefaultBashTimeout); err != nil {
				return durable.ToolResult{}, err
			}
			fs, err := resolveToolEnv(ctx, env, api)
			if err != nil {
				return durable.ToolResult{}, err
			}
			shell, ok := fs.(durable.ArgvShell)
			if !ok {
				return durable.ToolResult{}, errors.New("environment has no argv shell capability")
			}
			command, ok := args["command"].(string)
			if !ok {
				return durable.ToolResult{}, errors.New("command must be a string")
			}
			execution := BashExecution{Command: command, Cwd: fs.Cwd(), Env: map[string]string{}, InheritEnv: true}
			if config.CommandPrefix != "" {
				execution.Command = config.CommandPrefix + "\n" + execution.Command
			}
			if config.Prepare != nil {
				if err := config.Prepare(ctx, &execution, api); err != nil {
					return durable.ToolResult{}, err
				}
			}
			prepared := make(durable.JSON, len(args)+3)
			for k, v := range args {
				prepared[k] = v
			}
			prepared["command"], prepared["cwd"], prepared["executionEnv"], prepared["inheritEnv"] = execution.Command, execution.Cwd, execution.Env, execution.InheritEnv
			return executePortableCommand(ctx, fs, prepared, api, func(ctx context.Context, command string, opts durable.ShellExecOptions) (durable.ShellExecResult, error) {
				var result durable.ShellExecResult
				var err error = errors.New("no PowerShell program to run")
				for _, program := range programs {
					argv := []string{program, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", "try { [Console]::OutputEncoding=[System.Text.Encoding]::UTF8 } catch {}\n" + command}
					result, err = shell.ExecArgs(ctx, argv, opts)
					var failure *durable.ExecutionError
					if err == nil || !errors.As(err, &failure) || failure.Code != "spawn_error" {
						break
					}
				}
				return result, err
			})
		},
	}
}
