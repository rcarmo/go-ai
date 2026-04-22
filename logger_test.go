package goai_test

import (
	"bytes"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestDiscardLoggerDefault(t *testing.T) {
	// Default logger should be discard — no panics
	logger := goai.GetLogger()
	logger.Debug("test")
	logger.Info("test")
	logger.Warn("test")
	logger.Error("test")
}

func TestSimpleLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := goai.NewSimpleLogger(&buf, goai.LogLevelDebug)

	logger.Debug("debug msg", "key", "value")
	logger.Info("info msg")
	logger.Warn("warn msg", "count", 42)
	logger.Error("error msg")

	output := buf.String()
	if !strings.Contains(output, "[go-ai] DEBUG debug msg key=value") {
		t.Errorf("missing debug line in: %s", output)
	}
	if !strings.Contains(output, "[go-ai] INFO info msg") {
		t.Errorf("missing info line in: %s", output)
	}
	if !strings.Contains(output, "[go-ai] WARN warn msg count=42") {
		t.Errorf("missing warn line in: %s", output)
	}
	if !strings.Contains(output, "[go-ai] ERROR error msg") {
		t.Errorf("missing error line in: %s", output)
	}
}

func TestLogLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := goai.NewSimpleLogger(&buf, goai.LogLevelWarn)

	logger.Debug("should not appear")
	logger.Info("should not appear")
	logger.Warn("should appear")
	logger.Error("should appear")

	output := buf.String()
	if strings.Contains(output, "should not appear") {
		t.Errorf("debug/info should be filtered: %s", output)
	}
	if !strings.Contains(output, "WARN") || !strings.Contains(output, "ERROR") {
		t.Errorf("warn/error should appear: %s", output)
	}
}

func TestSetLogger(t *testing.T) {
	var buf bytes.Buffer
	goai.SetLogger(goai.NewSimpleLogger(&buf, goai.LogLevelInfo))
	defer goai.SetLogger(nil) // restore discard

	// Trigger logging via the internal convenience functions (tested indirectly via Stream)
	goai.GetLogger().Info("custom logger works")

	if !strings.Contains(buf.String(), "custom logger works") {
		t.Errorf("custom logger not used: %s", buf.String())
	}
}

func TestSetLoggerNil(t *testing.T) {
	goai.SetLogger(nil) // should restore discard, not panic
	goai.GetLogger().Error("this should not panic")
}
