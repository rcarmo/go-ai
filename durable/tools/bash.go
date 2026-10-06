//go:build unix

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	DefaultBashTimeout = time.Duration(0) // Omitted timeout has no deadline.
	maxTimeoutSeconds  = 2_147_483_647.0 / 1000.0
)

var bashSchema = json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Bash command to execute"},"timeout":{"type":"number","description":"Timeout in seconds (optional, no default timeout)","minimum":0},"cwd":{"type":"string","description":"Working directory to run the command in (relative or absolute)"}},"required":["command"],"additionalProperties":false}`)

type bashCommandError struct {
	message string
	cause   error
}

func (err *bashCommandError) Error() string { return err.message }
func (err *bashCommandError) Unwrap() error { return err.cause }

type BashExecution struct {
	Command    string
	Cwd        string
	Env        map[string]string
	InheritEnv bool
}
type BashOptions struct {
	CommandPrefix string
	Prepare       func(context.Context, *BashExecution, *durable.ToolAPI) error
}

// Bash returns a durable bash registration. Options follow the reference factory.
func Bash(env durable.FileSystem, options ...BashOptions) durable.ToolRegistration {
	var config BashOptions
	if len(options) > 0 {
		config = options[0]
	}
	return durable.ToolRegistration{
		Definition: goai.Tool{
			Name:        "bash",
			Description: "Execute a bash command in the current working directory. Returns combined stdout and stderr. Output is truncated to last 2000 lines or 50KB (whichever is hit first). Optionally provide a timeout in seconds and working directory.",
			Parameters:  bashSchema,
		},
		Implementation: "durable.tools.bash",
		Version:        1,
		ReplaySafe:     false,
		OutputLimits:   &durable.ToolOutputLimits{MaxBytes: MaxReadBytes, MaxLines: MaxReadLines, Retain: "tail"},
		Execute: func(ctx context.Context, args durable.JSON, api *durable.ToolAPI) (durable.ToolResult, error) {
			// Reference validation runs before acquiring an environment or calling
			// Prepare; invalid arguments must cause neither host effect.
			if _, _, err := bashTimeoutArg(args, "timeout", DefaultBashTimeout); err != nil {
				return durable.ToolResult{}, err
			}
			fs, err := resolveToolEnv(ctx, env, api)
			if err != nil {
				return durable.ToolResult{}, err
			}
			execution := BashExecution{Cwd: fs.Cwd(), Env: map[string]string{}, InheritEnv: true}
			if cwd, exists := args["cwd"]; exists && cwd != nil {
				path, ok := cwd.(string)
				if !ok {
					return durable.ToolResult{}, errors.New("cwd must be a string")
				}
				execution.Cwd, err = fs.AbsolutePath(ctx, path)
				if err != nil {
					return durable.ToolResult{}, err
				}
			}
			execution.Command, _ = args["command"].(string)
			if config.CommandPrefix != "" {
				execution.Command = config.CommandPrefix + "\n" + execution.Command
			}
			if config.Prepare != nil {
				if err := config.Prepare(ctx, &execution, api); err != nil {
					return durable.ToolResult{}, err
				}
			}
			copy := make(durable.JSON, len(args)+3)
			for key, value := range args {
				copy[key] = value
			}
			copy["command"], copy["cwd"], copy["executionEnv"], copy["inheritEnv"] = execution.Command, execution.Cwd, execution.Env, execution.InheritEnv
			args = copy
			shell, ok := fs.(durable.Shell)
			if !ok {
				return durable.ToolResult{}, errors.New("environment has no shell capability")
			}
			return executePortableBash(ctx, fs, shell, args, api)
		},
	}
}

func bashTimeoutArg(args durable.JSON, key string, def time.Duration) (time.Duration, float64, error) {
	value, ok := args[key]
	if !ok || value == nil {
		return def, def.Seconds(), nil
	}
	seconds, ok := asFloat(value)
	if !ok || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 {
		//lint:ignore ST1005 Reference tool argument diagnostic.
		return 0, 0, errors.New("Invalid timeout: must be a finite number of seconds")
	}
	if seconds > maxTimeoutSeconds {
		//lint:ignore ST1005 Reference tool argument diagnostic.
		return 0, 0, fmt.Errorf("Invalid timeout: maximum is %g seconds", maxTimeoutSeconds)
	}
	return time.Duration(seconds * float64(time.Second)), seconds, nil
}

func asFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}

func bashShellPath() (string, error) {
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash", nil
	}
	path, err := exec.LookPath("bash")
	if err != nil {
		return "", errors.New("bash shell not found")
	}
	return path, nil
}

func killProcessGroup(pid int) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

type bashCapture struct {
	mu             sync.Mutex
	buf            []byte
	totalBytes     int
	truncatedBytes bool
	truncatedLines bool
}

func (c *bashCapture) append(p []byte) {
	if len(p) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totalBytes += len(p)
	c.buf = append(c.buf, p...)
	trimmed := false
	excess := bashLineCount(c.buf) - MaxReadLines
	for excess > 0 {
		idx := bytes.IndexByte(c.buf, '\n')
		if idx < 0 {
			break
		}
		c.buf = c.buf[idx+1:]
		c.truncatedLines = true
		trimmed = true
		excess--
	}
	if len(c.buf) > MaxReadBytes {
		c.buf = c.buf[len(c.buf)-MaxReadBytes:]
		c.truncatedBytes = true
		trimmed = true
		excess := bashLineCount(c.buf) - MaxReadLines
		for excess > 0 {
			idx := bytes.IndexByte(c.buf, '\n')
			if idx < 0 {
				break
			}
			c.buf = c.buf[idx+1:]
			c.truncatedLines = true
			excess--
		}
	}
	if trimmed || cap(c.buf) > MaxReadBytes*2 {
		c.buf = append([]byte(nil), c.buf...)
	}
}

func (c *bashCapture) result() durable.ToolResult {
	c.mu.Lock()
	content := append([]byte(nil), c.buf...)
	totalBytes := c.totalBytes
	truncatedBytes := c.truncatedBytes
	truncatedLines := c.truncatedLines
	c.mu.Unlock()
	// Tail retention can begin inside a multibyte character. Discard only
	// continuation bytes of that clipped character, as the Harness does.
	if truncatedBytes {
		for len(content) > 0 && !utf8.RuneStart(content[0]) {
			content = content[1:]
		}
	}
	details := durable.JSON{
		"capturedBytes": totalBytes,
		"outputBytes":   len(content),
		"outputLines":   bashLineCount(content),
	}
	truncatedBy := bashTruncatedBy(truncatedBytes, truncatedLines)
	if truncatedBy != "" {
		details["truncated"] = true
		details["truncatedBy"] = truncatedBy
	}
	return durable.ToolResult{Content: string(content), Details: details}
}

func bashTruncatedBy(bytesExceeded, linesExceeded bool) string {
	parts := make([]string, 0, 3)
	if linesExceeded {
		parts = append(parts, "lines")
	}
	if bytesExceeded {
		parts = append(parts, "bytes")
	}
	return strings.Join(parts, ",")
}

func bashLineCount(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	count := bytes.Count(content, []byte{'\n'})
	if content[len(content)-1] != '\n' {
		count++
	}
	return count
}

type bashStreamer struct {
	mu       sync.Mutex
	api      *durable.ToolAPI
	disabled bool
}

func newBashStreamer(api *durable.ToolAPI) *bashStreamer {
	return &bashStreamer{api: api}
}

func (s *bashStreamer) write(p []byte) {
	if len(p) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.disabled || s.api == nil {
		return
	}
	if err := s.api.OutputBytes(p); err != nil {
		s.disabled = true
		return
	}
}

func formatTimeoutSeconds(seconds float64) string {
	if math.Trunc(seconds) == seconds {
		return fmt.Sprintf("%.0f", seconds)
	}
	return fmt.Sprintf("%g", seconds)
}
