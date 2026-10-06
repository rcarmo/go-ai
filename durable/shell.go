package durable

import "context"

// ExecutionError is the portable shell failure shape from the pinned contract.
// A nonzero command exit is a result, not a spawn/timeout/callback failure.
type ExecutionError struct {
	Code      string
	SpillPath string
	Cause     error
}

func (e *ExecutionError) Error() string { return "durable: shell " + e.Code }
func (e *ExecutionError) Unwrap() error { return e.Cause }

type ShellSpillOptions struct {
	AfterBytes int
	AfterLines int
}
type ShellOutputSkip struct {
	Bytes, Newlines int64
	EndsWithNewline bool
}
type ShellOutputInfo struct {
	Stream  string
	Skipped *ShellOutputSkip
}
type ShellOutputWindow struct {
	MaxBytes, MaxLines int
	MinIntervalMs      int64
	BytesPerSecond     int
}

// ArgvShell executes directly without an intervening shell. This additive
// capability preserves existing injected string-command shell implementations.
type ArgvShell interface {
	Shell
	ExecArgs(context.Context, []string, ShellExecOptions) (ShellExecResult, error)
}
type ShellExecOptions struct {
	Cwd          string
	Env          map[string]string
	InheritEnv   *bool    // nil defaults true
	Timeout      *float64 // seconds; nil has no timeout
	OnOutput     func(context.Context, string) error
	OnOutputInfo func(context.Context, string, ShellOutputInfo) error
	Window       *ShellOutputWindow
	Spill        *ShellSpillOptions
}
type ShellExecResult struct {
	ExitCode  int
	SpillPath string
}
type Shell interface {
	ExecutionEnvironment
	Exec(context.Context, string, ShellExecOptions) (ShellExecResult, error)
	Cleanup(context.Context) error
}
