package durable

import (
	"context"
	"reflect"
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

const v160NonFiniteLogScript = `console.log("avg", 0 / 0, 1 / 0, -1 / 0, { a: 1 }); text(0 / 0);`
const v160NonFiniteConsoleText = `avg NaN Infinity -Infinity {"a":1}`

func checkV160ThrownNaN(t *testing.T, ok bool, err *quickjs.Error) {
	t.Helper()
	if ok || err == nil || err.Kind != quickjs.ErrorScript || err.Message != "NaN" { // L141
		t.Errorf("thrown NaN: ok=%v error=%+v; want script message NaN", ok, err)
	}
}

// Source: pi v1.6.0 packages/codemode/test/sandbox.test.ts:133–142.
func TestSourceV160CodemodePublishedNonFiniteLogging(t *testing.T) {
	root := officialWorkerPackageRootV160(t)
	want := []map[string]any{
		{"type": "text", "text": v160NonFiniteConsoleText, "console": true},
		{"type": "text", "text": "NaN"},
	}
	for _, placement := range []string{"inline", "portable", "node-worker"} {
		t.Run(placement, func(t *testing.T) {
			logged := runV160PublishedGlobalFixture(t, root, placement, v160NonFiniteLogScript, "error")
			if !logged.OK || !reflect.DeepEqual(logged.Output, want) { // L136
				t.Errorf("log ok=%v output=%+v; want exact non-finite logging", logged.OK, logged.Output)
			}
			thrown := runV160PublishedGlobalFixture(t, root, placement, "throw NaN", "error")
			checkV160ThrownNaN(t, thrown.OK, thrown.Error)
		})
	}
}

func TestV160CodemodeNativeNonFiniteLogging(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	engine, err := quickjs.NewEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	logged := engine.Execute(ctx, v160NonFiniteLogScript, quickjs.Options{})
	want := []quickjs.OutputItem{{Type: "console", Text: v160NonFiniteConsoleText}, {Type: "text", Text: "NaN"}}
	if !logged.OK || !reflect.DeepEqual(logged.Output, want) {
		t.Errorf("log ok=%v output=%+v; want exact non-finite logging", logged.OK, logged.Output)
	}
	thrown := engine.Execute(ctx, "throw NaN", quickjs.Options{})
	checkV160ThrownNaN(t, thrown.OK, thrown.Error)
}
