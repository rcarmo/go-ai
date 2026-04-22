// Logging — centralized, pluggable, zero-allocation by default.
//
// go-ai uses a simple leveled logger interface that can be replaced
// by the host application. The default logger discards everything.
// Set a logger with SetLogger() at program startup.
//
// This avoids importing log/slog (Go 1.21+) as a hard dependency
// while remaining trivially adaptable to slog, zerolog, zap, etc.
package goai

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync/atomic"
)

// LogLevel controls which messages are emitted.
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
	LogLevelOff
)

// Logger is the interface go-ai uses for all diagnostic output.
type Logger interface {
	Debug(msg string, keysAndValues ...interface{})
	Info(msg string, keysAndValues ...interface{})
	Warn(msg string, keysAndValues ...interface{})
	Error(msg string, keysAndValues ...interface{})
}

// --- Default: discard logger ---

type discardLogger struct{}

func (discardLogger) Debug(string, ...interface{}) {}
func (discardLogger) Info(string, ...interface{})  {}
func (discardLogger) Warn(string, ...interface{})  {}
func (discardLogger) Error(string, ...interface{}) {}

// --- Simple stderr logger ---

type simpleLogger struct {
	level  LogLevel
	logger *log.Logger
}

func (l *simpleLogger) Debug(msg string, kv ...interface{}) {
	if l.level <= LogLevelDebug {
		l.emit("DEBUG", msg, kv)
	}
}
func (l *simpleLogger) Info(msg string, kv ...interface{}) {
	if l.level <= LogLevelInfo {
		l.emit("INFO", msg, kv)
	}
}
func (l *simpleLogger) Warn(msg string, kv ...interface{}) {
	if l.level <= LogLevelWarn {
		l.emit("WARN", msg, kv)
	}
}
func (l *simpleLogger) Error(msg string, kv ...interface{}) {
	if l.level <= LogLevelError {
		l.emit("ERROR", msg, kv)
	}
}

func (l *simpleLogger) emit(level, msg string, kv []interface{}) {
	if len(kv) == 0 {
		l.logger.Printf("[go-ai] %s %s", level, msg)
		return
	}
	attrs := ""
	for i := 0; i+1 < len(kv); i += 2 {
		attrs += fmt.Sprintf(" %v=%v", kv[i], kv[i+1])
	}
	l.logger.Printf("[go-ai] %s %s%s", level, msg, attrs)
}

// NewSimpleLogger creates a leveled logger that writes to w.
func NewSimpleLogger(w io.Writer, level LogLevel) Logger {
	return &simpleLogger{
		level:  level,
		logger: log.New(w, "", log.LstdFlags),
	}
}

// NewStderrLogger creates a logger that writes to stderr at the given level.
func NewStderrLogger(level LogLevel) Logger {
	return NewSimpleLogger(os.Stderr, level)
}

// --- Global logger ---

var globalLogger atomic.Pointer[Logger]

func init() {
	l := Logger(discardLogger{})
	globalLogger.Store(&l)
}

// SetLogger replaces the global logger. Call at program startup.
// Pass nil to restore the discard logger.
func SetLogger(l Logger) {
	if l == nil {
		l = discardLogger{}
	}
	globalLogger.Store(&l)
}

// GetLogger returns the current global logger.
func GetLogger() Logger {
	return *globalLogger.Load()
}

// Package-level convenience for internal use.
func logDebug(msg string, kv ...interface{}) { GetLogger().Debug(msg, kv...) }
func logInfo(msg string, kv ...interface{})  { GetLogger().Info(msg, kv...) }
func logWarn(msg string, kv ...interface{})  { GetLogger().Warn(msg, kv...) }
func logError(msg string, kv ...interface{}) { GetLogger().Error(msg, kv...) }
