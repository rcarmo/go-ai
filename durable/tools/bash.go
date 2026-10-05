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
)

const (
	DefaultBashTimeout = 120 * time.Second
	maxBashPrefixBytes = 8 << 10
	maxTimeoutSeconds  = 2_147_483_647.0 / 1000.0
)

var bashSchema = json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Bash command to execute"},"timeout":{"type":"number","description":"Timeout in seconds (optional, defaults to 120 seconds)","minimum":0},"cwd":{"type":"string","description":"Working directory to run the command in (relative or absolute)"}},"required":["command"],"additionalProperties":false}`)

// Bash returns a durable bash tool registration.
func Bash(env *Env) durable.ToolRegistration {
	return durable.ToolRegistration{
		Definition: goai.Tool{
			Name:        "bash",
			Description: "Execute a bash command in the current working directory. Returns combined stdout and stderr. Output is truncated to last 2000 lines or 50KB (whichever is hit first). Optionally provide a timeout in seconds and working directory.",
			Parameters:  bashSchema,
		},
		Implementation: "durable.tools.bash",
		Version:        1,
		ReplaySafe:     false,
		Execute: func(ctx context.Context, args durable.JSON, api *durable.ToolAPI) (durable.ToolResult, error) {
			env, err := resolveToolEnv(ctx, env, api)
			if err != nil {
				return durable.ToolResult{}, err
			}
			return executeBash(ctx, env, args, api)
		},
	}
}

func executeBash(ctx context.Context, env *Env, args durable.JSON, api *durable.ToolAPI) (durable.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return durable.ToolResult{}, err
	}
	command, ok := args["command"].(string)
	if !ok {
		return durable.ToolResult{}, errors.New("command must be a string")
	}
	if strings.TrimSpace(command) == "" {
		return durable.ToolResult{}, errors.New("command is required")
	}
	timeout, timeoutSeconds, err := bashTimeoutArg(args, "timeout", DefaultBashTimeout)
	if err != nil {
		return durable.ToolResult{}, err
	}
	cwd, err := bashCWD(env, args["cwd"])
	if err != nil {
		return durable.ToolResult{}, err
	}
	shell, err := bashShellPath()
	if err != nil {
		return durable.ToolResult{}, err
	}
	cmd := exec.Command(shell, "-c", command)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	spillPath, err := env.Spill(ctx, nil)
	if err != nil {
		return durable.ToolResult{}, err
	}
	spill, err := os.OpenFile(spillPath, os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return durable.ToolResult{}, err
	}
	defer spill.Close()
	keepSpill := false
	defer func() {
		if !keepSpill {
			os.Remove(spillPath)
		}
	}()
	capture := &bashCapture{}
	streamer := newBashStreamer(api)
	writer := &bashCaptureWriter{capture: capture, streamer: streamer, spill: spill}
	cmd.Stdout, cmd.Stderr = writer, writer
	// Bound pipe copying even if the shell exits with background descendants.
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		return durable.ToolResult{}, err
	}
	processDone := make(chan struct{})
	monitorDone := make(chan struct{})
	var stopErr error
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	go func() {
		defer close(monitorDone)
		select {
		case <-processDone:
		case <-ctx.Done():
			stopErr = fmt.Errorf("command aborted: %w", ctx.Err())
			killProcessGroup(cmd.Process.Pid)
		case <-timer.C:
			stopErr = fmt.Errorf("command timed out after %s: %w", formatTimeoutSeconds(timeoutSeconds), context.DeadlineExceeded)
			killProcessGroup(cmd.Process.Pid)
		}
	}()
	waitErr := cmd.Wait()
	close(processDone)
	<-monitorDone
	// Shell success must not leave a background process from this tool running.
	killProcessGroup(cmd.Process.Pid)
	result := capture.result(streamer.streamedBytes(), api != nil)
	if result.Details == nil {
		result.Details = durable.JSON{}
	}
	if err := spill.Sync(); err != nil {
		return result, err
	}
	if err := spill.Close(); err != nil {
		return result, err
	}
	if result.Details["truncated"] == true {
		keepSpill = true
		result.Details["fullOutputPath"] = spillPath
	}
	result.Details["cwd"] = cwd
	result.Details["timeoutSeconds"] = timeoutSeconds
	if cmd.ProcessState != nil {
		exitCode := cmd.ProcessState.ExitCode()
		if exitCode >= 0 {
			result.Details["exitCode"] = exitCode
		}
	}
	if stopErr != nil {
		return result, stopErr
	}
	if waitErr != nil {
		exitCode := -1
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		if exitCode >= 0 {
			return result, fmt.Errorf("command exited with code %d", exitCode)
		}
		return result, waitErr
	}
	return result, nil
}

func bashTimeoutArg(args durable.JSON, key string, def time.Duration) (time.Duration, float64, error) {
	value, ok := args[key]
	if !ok || value == nil {
		return def, def.Seconds(), nil
	}
	seconds, ok := asFloat(value)
	if !ok || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 {
		return 0, 0, errors.New("timeout must be a finite number of seconds")
	}
	if seconds > maxTimeoutSeconds {
		return 0, 0, fmt.Errorf("timeout must be <= %g seconds", maxTimeoutSeconds)
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

func bashCWD(env *Env, value any) (string, error) {
	var cwd string
	if value == nil {
		if env == nil {
			return "", errors.New("nil env")
		}
		var err error
		cwd, err = env.resolve(".")
		if err != nil {
			return "", err
		}
	} else {
		text, ok := value.(string)
		if !ok {
			return "", errors.New("cwd must be a string")
		}
		if text == "" {
			if env == nil {
				return "", errors.New("nil env")
			}
			var err error
			cwd, err = env.resolve(".")
			if err != nil {
				return "", err
			}
		} else {
			if env == nil {
				return "", errors.New("nil env")
			}
			var err error
			cwd, err = env.resolve(text)
			if err != nil {
				return "", err
			}
		}
	}
	info, err := os.Stat(cwd)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("working directory does not exist: %s", cwd)
		}
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("working directory is not a directory: %s", cwd)
	}
	return cwd, nil
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

func (c *bashCapture) result(streamed int, streamedPrefix bool) durable.ToolResult {
	c.mu.Lock()
	content := append([]byte(nil), c.buf...)
	totalBytes := c.totalBytes
	truncatedBytes := c.truncatedBytes
	truncatedLines := c.truncatedLines
	c.mu.Unlock()
	clippedForHarness := false
	if streamedPrefix {
		if !truncatedBytes && !truncatedLines {
			if streamed >= len(content) {
				content = nil
			} else if streamed > 0 {
				content = content[streamed:]
			}
		}
		maxTail := durable.MaxToolOutputBytes - streamed
		if maxTail < 0 {
			maxTail = 0
		}
		if len(content) > maxTail {
			content = append([]byte(nil), content[len(content)-maxTail:]...)
			clippedForHarness = true
		}
	}
	details := durable.JSON{
		"capturedBytes": totalBytes,
		"outputBytes":   len(content),
		"outputLines":   bashLineCount(content),
	}
	truncatedBy := bashTruncatedBy(truncatedBytes, truncatedLines, clippedForHarness)
	if truncatedBy != "" {
		details["truncated"] = true
		details["truncatedBy"] = truncatedBy
	}
	if clippedForHarness {
		details["harnessOutputLimit"] = durable.MaxToolOutputBytes
	}
	return durable.ToolResult{Content: string(content), Details: details}
}

func bashTruncatedBy(bytesExceeded, linesExceeded, harness bool) string {
	parts := make([]string, 0, 3)
	if linesExceeded {
		parts = append(parts, "lines")
	}
	if bytesExceeded {
		parts = append(parts, "bytes")
	}
	if harness {
		parts = append(parts, "toolLimit")
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
	mu        sync.Mutex
	api       *durable.ToolAPI
	remaining int
	streamed  int
	disabled  bool
}

func newBashStreamer(api *durable.ToolAPI) *bashStreamer {
	return &bashStreamer{api: api, remaining: maxBashPrefixBytes}
}

func (s *bashStreamer) write(p []byte) {
	if len(p) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.disabled || s.api == nil || s.remaining <= 0 {
		return
	}
	text := string(p)
	if len([]byte(text)) > s.remaining {
		text = truncateUTF8(text, s.remaining)
	}
	if text == "" {
		s.disabled = true
		return
	}
	if err := s.api.Output(text); err != nil {
		s.disabled = true
		return
	}
	bytes := len([]byte(text))
	s.streamed += bytes
	s.remaining -= bytes
	if s.remaining <= 0 {
		s.disabled = true
	}
}

func (s *bashStreamer) streamedBytes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.streamed
}

type bashCaptureWriter struct {
	capture  *bashCapture
	streamer *bashStreamer
	spill    *os.File
}

func (w *bashCaptureWriter) Write(p []byte) (int, error) {
	if w.spill != nil {
		if _, err := w.spill.Write(p); err != nil {
			return 0, err
		}
	}
	w.capture.append(p)
	w.streamer.write(p)
	return len(p), nil
}

func formatTimeoutSeconds(seconds float64) string {
	if math.Trunc(seconds) == seconds {
		return fmt.Sprintf("%.0f seconds", seconds)
	}
	return fmt.Sprintf("%g seconds", seconds)
}
