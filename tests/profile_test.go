package goai_test

import (
	"github.com/rcarmo/go-ai/internal/testprofile"
	"os"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(testprofile.Run(m.Run)) }
