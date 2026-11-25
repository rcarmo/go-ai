package durable

import (
	"context"
	"testing"
	"time"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

const v160NonStringMessageScript = `const error = new Error('x'); error.message = 42; throw error;`

func checkV160NonStringErrorMessage(t *testing.T, ok bool, err *quickjs.Error) {
	t.Helper()
	if ok || err == nil || err.Kind != quickjs.ErrorScript || err.Name != "Error" || err.Message != "42" {
		t.Errorf("numeric message: ok=%v error=%+v; want script Error with message 42", ok, err)
	}
}

// Source: pi v1.6.0 packages/codemode/test/sandbox.test.ts:804–808.
func TestSourceV160CodemodePublishedNonStringErrorMessage(t *testing.T) {
	root := officialWorkerPackageRootV160(t)
	for _, placement := range []string{"inline", "portable", "node-worker"} {
		t.Run(placement, func(t *testing.T) {
			got := runV160PublishedGlobalFixture(t, root, placement, v160NonStringMessageScript, "error")
			checkV160NonStringErrorMessage(t, got.OK, got.Error)
		})
	}
}

func TestV160CodemodeNativeNonStringErrorMessage(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	engine, err := quickjs.NewEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	got := engine.Execute(ctx, v160NonStringMessageScript, quickjs.Options{})
	checkV160NonStringErrorMessage(t, got.OK, got.Error)
}

const v160ThrownObjectScript = `throw { code: 7 }`

func checkV160ThrownObject(t *testing.T, ok bool, err *quickjs.Error) {
	t.Helper()
	if ok || err == nil || err.Kind != quickjs.ErrorScript || err.Message != `{"code":7}` {
		t.Errorf("thrown object: ok=%v error=%+v; want the exact script diagnostic", ok, err)
	}
}

// Source: pi v1.6.0 packages/codemode/test/sandbox.test.ts:263–267.
func TestSourceV160CodemodePublishedThrownObject(t *testing.T) {
	root := officialWorkerPackageRootV160(t)
	for _, placement := range []string{"inline", "portable", "node-worker"} {
		t.Run(placement, func(t *testing.T) {
			got := runV160PublishedGlobalFixture(t, root, placement, v160ThrownObjectScript, "error")
			checkV160ThrownObject(t, got.OK, got.Error)
		})
	}
}

func TestV160CodemodeNativeThrownObject(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	engine, err := quickjs.NewEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	got := engine.Execute(ctx, v160ThrownObjectScript, quickjs.Options{})
	checkV160ThrownObject(t, got.OK, got.Error)
}
