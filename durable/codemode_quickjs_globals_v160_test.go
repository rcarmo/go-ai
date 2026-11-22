package durable

import (
	"context"
	"testing"
	"time"
)

// sandbox.test.ts:506–523, through the public Go inline QuickJS sandbox. It
// shares the original script and exact result assertions with the portable
// worker test, but needs no Node process or external package installation.
func TestSourceV160CodemodeInlineNamespacedGlobals(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sandbox, err := NewQuickJSSandbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := sandbox.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	checkV160CodemodeNamespacedGlobals(t, ctx, sandbox, nil, false)
}
