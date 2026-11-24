package durable_test

import (
	"os"
	"testing"

	"github.com/rcarmo/go-ai/durable"
	"github.com/rcarmo/go-ai/durable/tools"
)

// packages/durable/test/codemode.test.ts:1009–1041.
func TestSourceV160CodemodePublishedWorkerSpillPathAfterCrash(t *testing.T) {
	cwd := t.TempDir()
	spillRoot := t.TempDir()
	t.Setenv("TMPDIR", spillRoot)
	t.Setenv("TMP", spillRoot)
	// Use distinct production local filesystem capabilities with the same cwd.
	durable.CheckV160PublishedWorkerSpillReplay(t, tools.LocalEnv(cwd), tools.LocalEnv(cwd))
	dirs, err := os.ReadDir(spillRoot)
	if err != nil || len(dirs) != 1 || !dirs[0].IsDir() {
		t.Errorf("spill directories = %v, %v; want a single reused directory", dirs, err)
	}
}
