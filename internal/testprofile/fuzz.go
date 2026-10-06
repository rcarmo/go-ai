// Package testprofile captures coordinator and worker profiles for fuzzing.
// The Go command disallows profile flags with -fuzz, so TestMain hooks use a
// command-scoped output directory and PID-specific files instead.
package testprofile

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
)

func Run(run func() int) int {
	dir := os.Getenv("GO_AI_FUZZ_PROFILE_DIR")
	if dir == "" {
		return run()
	}
	stem := filepath.Join(dir, fmt.Sprintf("process-%d", os.Getpid()))
	marker := stem + ".pending"
	fail := func(err error) int { fmt.Fprintf(os.Stderr, "fuzz profile: %v\n", err); return 1 }
	if err := os.WriteFile(marker, []byte("profile capture started\n"), 0600); err != nil {
		return fail(err)
	}
	cpu, err := os.Create(stem + ".cpu.pprof")
	if err != nil {
		return fail(err)
	}
	if err := pprof.StartCPUProfile(cpu); err != nil {
		cpu.Close()
		return fail(err)
	}
	code := run()
	pprof.StopCPUProfile()
	if err := cpu.Close(); err != nil {
		return fail(err)
	}
	heap, err := os.Create(stem + ".heap.pprof")
	if err != nil {
		return fail(err)
	}
	runtime.GC()
	if err := pprof.WriteHeapProfile(heap); err != nil {
		heap.Close()
		return fail(err)
	}
	if err := heap.Close(); err != nil {
		return fail(err)
	}
	if err := os.Remove(marker); err != nil {
		return fail(err)
	}
	return code
}
