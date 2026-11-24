package durable

import (
	"reflect"
	"strings"
	"testing"
)

// codemode.test.ts:540–564. The original clears its journal entries before
// replay. Here both live nested calls are admitted, but left blocked, so the
// native journal has a real initial watermark and no confirmed deliveries.
func TestSourceV160CodemodePublishedWorkerBatchRetryFence(t *testing.T) {
	for _, tc := range []struct {
		name, code, diagnostic string
	}{
		{
			// L558 and L561–562 use the unchanged script. Native also
			// records issued identities, so it rejects call 1 before the
			// source's count-only diagnostic at L560 can be selected.
			name: "changed-prefix",
			code: `const k = load("k");
			tools.echo({ text: k ?? "a" });
			if (k) tools.echo({ text: "new" });
			await tools.hold({});`,
			diagnostic: "sandbox call replay divergence: call 1 is tools.echo",
		},
		{
			// Supplemental L560 projection: keep calls 1 and 2 identical
			// and append the fresh echo in the same initial drain. Await
			// it so a wrongly admitted call cannot be lost to guest exit.
			name: "matching-prefix-extra-call",
			code: `const k = load("k");
			tools.echo({ text: "a" });
			const hold = tools.hold({});
			if (k) await tools.echo({ text: "new" });
			await hold;`,
			diagnostic: "its first run made 3 calls, where it had made 2",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runV160PublishedWorkerNestedReplay(t, tc.code, workerReplayChangesV160{
				pauseEcho: true,
				guestStore: func(run int32, store JSON) JSON {
					if run == 2 {
						return JSON{"k": "x"}
					}
					return store
				},
			})
			if got.firstIssued == nil || *got.firstIssued != 2 || len(got.beforeClose) != 0 {
				t.Fatalf("live journal must have a two-call watermark and no deliveries: count=%v deliveries=%+v", got.firstIssued, got.beforeClose)
			}
			if !got.isError || !strings.Contains(got.text, tc.diagnostic) {
				t.Errorf("replay error=%t, text=%q; want %q", got.isError, got.text, tc.diagnostic)
			}
			if got.nested != 2 || !reflect.DeepEqual(got.echoes, map[string]int{"a": 1}) {
				t.Errorf("nested tasks = %d, echo effects = %v; want exactly 2 and map[a:1]", got.nested, got.echoes)
			}
			if got.starts != 2 || got.replies != 0 {
				t.Errorf("guest starts=%d, guest replies=%d; want two guests and no deliveries", got.starts, got.replies)
			}
		})
	}
}
