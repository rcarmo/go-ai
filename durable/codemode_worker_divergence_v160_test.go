package durable

import (
	"context"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

// codemode.test.ts:276–291 (L288–289). The source changes the saved count
// to 7. Native SQLite forbids changing a confirmed count, so a live guest
// issues seven real calls. Only the live guest input is perturbed; the
// stored program and replay both use the unchanged script.
func TestSourceV160CodemodePublishedWorkerReplayCountDivergence(t *testing.T) {
	const source = `await tools.echo({ text: "a" }); await tools.hold({});`
	got := runV160PublishedWorkerNestedReplay(t, source, workerReplayChangesV160{
		guestCode: func(run int32, code string) string {
			if run != 1 {
				return code
			}
			return strings.Replace(code, `await tools.hold({});`, `
const barrier = tools.hold({});
for (let i = 0; i < 5; i++) tools.hold({});
await barrier;`, 1)
		},
	})
	if len(got.beforeClose) != 1 || got.beforeClose[0].IssuedAfter == nil || *got.beforeClose[0].IssuedAfter != 7 {
		t.Fatalf("live run must commit result 1 with seven issued calls: %+v", got.beforeClose)
	}
	if !got.isError || !strings.Contains(got.text, "after result 1 it had made 2 calls, where it had made 7") {
		t.Errorf("replay error=%t, text=%q; want exact count diagnostic fragment", got.isError, got.text)
	}
	// Supplemental: no repeat of the original echo. Already admitted holds
	// may independently recover; their count is not a fresh-admission check.
	if got.starts != 2 || got.replies != 2 || !reflect.DeepEqual(got.echoes, map[string]int{"a": 1}) {
		t.Errorf("guest starts=%d, guest replies=%d, echo effects=%v; want two guests, two echo deliveries and one settled echo", got.starts, got.replies, got.echoes)
	}
}

// codemode.test.ts:312–323 (L320–321). Instead of overwriting the recorded
// arguments, let the live guest really call next with "other". The replay
// uses the original next() and must not consume the mismatched reply.
func TestSourceV160CodemodePublishedWorkerReplayGlobalArgumentDivergence(t *testing.T) {
	const source = `await next(); await tools.hold({});`
	var calls atomic.Int32
	got := runV160PublishedWorkerNestedReplay(t, source, workerReplayChangesV160{
		guestCode: func(run int32, code string) string {
			if run == 1 {
				return strings.Replace(code, `next()`, `next("other")`, 1)
			}
			return code
		},
		globals: []CodemodeGlobal{{Name: "next", Version: 1, Execute: func(_ context.Context, arg any) (any, error) {
			calls.Add(1)
			if arg != "other" {
				t.Errorf("host argument = %#v; want other", arg)
			}
			return 1, nil
		}}},
	})
	if len(got.beforeClose) != 1 {
		t.Fatalf("live run must commit only the next delivery: %+v", got.beforeClose)
	}
	if call := got.beforeClose[0].Call; !call.Global || call.Name != "next" || call.UndefinedArgument || call.Arguments["args"] != "other" {
		t.Fatalf("live run must commit the explicit next argument: %+v", call)
	}
	if !got.isError || !strings.Contains(got.text, "call 1 is next") {
		t.Errorf("replay error=%t, text=%q; want exact global diagnostic fragment", got.isError, got.text)
	}
	if got.starts != 2 || got.replies != 1 || calls.Load() != 1 {
		t.Errorf("guest starts=%d, guest replies=%d, global calls=%d; want two guests and only the original reply and effect", got.starts, got.replies, calls.Load())
	}
}
