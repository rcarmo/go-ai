package faux

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
)

func TestFaux110PromptPartPrefixMatchesIndependentJoinedReference(t *testing.T) {
	parts := [][]string{nil, {""}, {"a"}, {"a", "b"}, {"a", "bc"}, {"a", ""}, {"😀", "x"}, {"😀", "y"}, {"abc", "def", "ghi"}}
	for _, a := range parts {
		for _, b := range parts {
			left, right := strings.Join(a, "\n\n"), strings.Join(b, "\n\n")
			i := 0
			for i < len(left) && i < len(right) && left[i] == right[i] {
				i++
			}
			// Use BMP/ascii or equal supplementary prefixes; truncate only complete
			// UTF-8 sequences for this independent textual reference.
			want := chars(left[:i])
			if got := commonPromptChars(a, b); got != want {
				t.Fatal(a, b, got, want)
			}
		}
	}
}
func TestFaux110SessionCacheIsolationDisabledAndChangedPrefix(t *testing.T) {
	api := goai.Api("faux-cache-110")
	r := Register(&Options{Api: string(api), TokensPerSecond: 1000000})
	defer goai.UnregisterApi(api)
	model := r.GetModel()
	ctx := &goai.Context{SystemPrompt: "long shared system prompt", Messages: []goai.Message{goai.UserMessage("first prompt")}}
	request := func(session string, retention goai.CacheRetention) *goai.Usage {
		var result *goai.Message
		for event := range r.stream(context.Background(), model, ctx, &goai.StreamOptions{SessionID: session, CacheRetention: retention}) {
			if done, ok := event.(*goai.DoneEvent); ok {
				result = done.Message
			}
		}
		if result == nil || result.Usage == nil {
			t.Fatal("usage missing")
		}
		return result.Usage
	}
	first := request("one", "")
	if first.CacheRead != 0 || first.CacheWrite == 0 {
		t.Fatal(first)
	}
	same := request("one", "")
	if same.CacheRead == 0 || same.CacheWrite != 0 {
		t.Fatal(same)
	}
	separate := request("two", "")
	if separate.CacheRead != 0 {
		t.Fatal("sessions mixed", separate)
	}
	disabled := request("one", goai.CacheRetentionNone)
	if disabled.CacheRead != 0 || disabled.CacheWrite != 0 || disabled.Input == 0 {
		t.Fatal(disabled)
	}
	noSession := request("", "")
	if noSession.CacheRead != 0 || noSession.CacheWrite != 0 {
		t.Fatal(noSession)
	}
	ctx.Messages = append(ctx.Messages, goai.UserMessage("second"))
	changed := request("one", "")
	if changed.CacheRead == 0 || changed.CacheWrite == 0 {
		t.Fatal(changed)
	}
}
