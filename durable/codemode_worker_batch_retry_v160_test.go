package durable

import (
	"context"
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

// codemode.test.ts:632–661. Replay the exact script against the original
// immutable start/journal; only the test guest's replay input gets k=true.
func TestSourceV160CodemodePublishedWorkerHeldBatchBeforeLaunch(t *testing.T) {
	const source = `const k = load("k");
		if (k) tools.echo({ text: "new" }); else forever();
		await tools.hold({ id: k ? 2 : 1 });`
	got := runV160PublishedWorkerNestedReplay(t, source, workerReplayChangesV160{
		guestStore: workerReplayStoreWithKV160,
		globals: []CodemodeGlobal{{Name: "forever", Version: 1, Execute: func(ctx context.Context, _ any) (any, error) {
			<-ctx.Done()
			return nil, nil
		}}},
	})
	if got.firstIssued == nil || *got.firstIssued != 2 || len(got.beforeClose) != 0 {
		t.Fatalf("live journal must have 2 issued calls and no deliveries: count=%v deliveries=%+v", got.firstIssued, got.beforeClose)
	}
	if !got.isError { // L656
		t.Errorf("replay accepted the changed batch: %q", got.text)
	}
	if len(got.echoes) != 0 || got.nested != 1 { // L658–659
		t.Errorf("echo effects=%v, nested tasks=%d; want no echo and only the original hold", got.echoes, got.nested)
	}
	// L657 intentionally differs: the pinned call-1 identity is enough to
	// reject, and the tool label excludes literal arguments.
	if !strings.Contains(got.text, "call 1 is tools.echo(…), but was forever()") || strings.Contains(got.text, `"new"`) {
		t.Errorf("replay diagnostic=%q; want first target mismatch without arguments", got.text)
	}
	if got.starts != 2 || got.replies != 0 {
		t.Errorf("guest starts=%d, guest deliveries=%d; want 2 starts and 0 deliveries", got.starts, got.replies)
	}
}

// codemode.test.ts:325–352. Use the original script for the error, effect and
// task-count assertions. The replay input changes only in the test guest;
// the saved start and journal remain sealed.
func TestSourceV160CodemodePublishedWorkerPostResultAdmissionFence(t *testing.T) {
	for _, tc := range []struct {
		name, source, diagnostic string
	}{
		{
			name: "changed-next-call",
			source: `await tools.echo({ text: "a" });
			const extra = load("k") ?? 0;
			for (let i = 0; i < extra; i++) tools.echo({ text: "extra" + i });
			await tools.hold({});`,
			// Native replay rejects the earlier issued identity mismatch.
			diagnostic: "call 2 is tools.echo",
		},
		{
			name: "matching-prefix-extra-calls",
			// Supplemental L348 projection. Retain the issued hold in
			// slot 2, then add three fresh calls before the next drain.
			source: `await tools.echo({ text: "a" });
			const hold = tools.hold({});
			const extra = load("k") ?? 0;
			await Promise.all(Array.from({ length: extra }, (_, i) => tools.echo({ text: "extra" + i })));
			await hold;`,
			diagnostic: "after result 1 it had made 5 calls, where it had made 2",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runV160PublishedWorkerNestedReplay(t, tc.source, workerReplayChangesV160{
				guestStore: func(run int32, store JSON) JSON {
					if run == 2 {
						return JSON{"k": 3}
					}
					return store
				},
			})
			if len(got.beforeClose) != 1 || got.beforeClose[0].Call.Name != "echo" || got.beforeClose[0].IssuedAfter == nil || *got.beforeClose[0].IssuedAfter != 2 {
				t.Fatalf("live journal must confirm echo and its two-call watermark: %+v", got.beforeClose)
			}
			if !got.isError || !strings.Contains(got.text, tc.diagnostic) {
				t.Errorf("replay error=%t, text=%q; want %q", got.isError, got.text, tc.diagnostic)
			}
			if !reflect.DeepEqual(got.echoes, map[string]int{"a": 1}) || got.nested != 2 { // L349–350
				t.Errorf("echo effects=%v, nested tasks=%d; want map[a:1] and exactly 2 tasks", got.echoes, got.nested)
			}
			if got.starts != 2 || got.replies != 2 {
				t.Errorf("guest starts=%d, guest deliveries=%d; want 2 starts and only the live/replayed echo a deliveries", got.starts, got.replies)
			}
		})
	}
}
