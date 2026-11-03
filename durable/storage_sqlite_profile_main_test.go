package durable

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"testing"
)

// Only the explicit profiling wrapper enables child captures. Ordinary runs
// must not create profiles, including these process-exit/ownership fixtures.
func sqliteChildProfileDirectory(t *testing.T) string {
	t.Helper()
	if os.Getenv("GO_AI_PROFILE_CHILDREN") != "1" {
		return ""
	}
	return t.TempDir()
}

// The crash helper bypasses Go's test-runner finalisation with os.Exit, so it
// must flush its optional captures before deliberately terminating the process.
func startSQLiteCrashProfiles(t *testing.T, dir string) func() {
	t.Helper()
	if dir == "" || os.Getenv("GO_AI_PROFILE_CHILDREN") != "1" {
		return func() {}
	}
	cpu, err := os.Create(filepath.Join(dir, "cpu.pprof"))
	if err != nil {
		t.Fatal(err)
	}
	if err := pprof.StartCPUProfile(cpu); err != nil {
		_ = cpu.Close()
		t.Fatal(err)
	}
	return func() {
		pprof.StopCPUProfile()
		if err := cpu.Close(); err != nil {
			t.Fatal(err)
		}
		runtime.GC()
		heap, err := os.Create(filepath.Join(dir, "heap.pprof"))
		if err != nil {
			t.Fatal(err)
		}
		writeErr := pprof.WriteHeapProfile(heap)
		closeErr := heap.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatalf("child heap profile: write=%v close=%v", writeErr, closeErr)
		}
	}
}

func analyseSQLiteChildProfiles(t *testing.T, dir string) {
	t.Helper()
	if dir == "" {
		return
	}
	// This is the parent's owned TempDir; keep captures only through analysis.
	defer os.RemoveAll(dir)
	for _, metric := range []struct{ flag, file string }{
		{"-alloc_space", "heap.pprof"},
		{"-alloc_objects", "heap.pprof"},
		{"-cum", "cpu.pprof"},
	} {
		output, err := exec.Command("go", "tool", "pprof", "-top", metric.flag, os.Args[0], filepath.Join(dir, metric.file)).CombinedOutput()
		if err != nil {
			t.Fatalf("child profile analysis %s: %v\n%s", metric.flag, err, output)
		}
	}
}

func TestSQLiteChildProfilesRequireExplicitOptIn(t *testing.T) {
	for _, value := range []string{"", "0", "true", "1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("GO_AI_PROFILE_CHILDREN", value)
			dir := sqliteChildProfileDirectory(t)
			if (dir != "") != (value == "1") {
				t.Fatalf("profile directory=%q opt-in=%q", dir, value)
			}
			if dir != "" {
				files, err := os.ReadDir(dir)
				if err != nil || len(files) != 0 || !filepath.IsAbs(dir) {
					t.Fatalf("configuration created capture data: files=%v err=%v", files, err)
				}
			}
		})
	}
}
