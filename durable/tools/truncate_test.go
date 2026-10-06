package tools

import "testing"

func TestTruncateHeadReferenceTrailingNewlineEqualityAndZeroLimits(t *testing.T) {
	lines, bytes := 2, 3
	for _, tc := range []struct {
		input, output string
		truncated     bool
		reason        string
		count         int
	}{{"a\nb", "a\nb", false, "", 2}, {"a\nb\n", "a\nb", true, "bytes", 2}, {"a\nb\nc", "a\nb", true, "lines", 2}, {"abc\n", "abc", true, "bytes", 1}, {"abcd", "", true, "bytes", 0}, {"", "", false, "", 0}} {
		got := TruncateHead(tc.input, TruncationOptions{MaxLines: &lines, MaxBytes: &bytes})
		reason := ""
		if got.TruncatedBy != nil {
			reason = *got.TruncatedBy
		}
		if got.Content != tc.output || got.Truncated != tc.truncated || reason != tc.reason || got.OutputLines != tc.count {
			t.Fatal(tc, got)
		}
	}
	zero := 0
	if got := TruncateHead("a", TruncationOptions{MaxBytes: &zero}); got.Content != "" || !got.FirstLineExceedsLimit {
		t.Fatal(got)
	}
	if got := TruncateHead("a\n"); got.Content != "a\n" || got.TotalLines != 1 || got.Truncated {
		t.Fatal(got)
	}
}

func TestTruncateHeadReferenceUTF8CountsAndCompleteLines(t *testing.T) {
	lines, bytes := 10, 100
	result := TruncateHead("aé🙂\nb", TruncationOptions{MaxLines: &lines, MaxBytes: &bytes})
	if result.Truncated || result.TotalBytes != 9 || result.OutputBytes != 9 {
		t.Fatal(result)
	}
	bytes = 4
	result = TruncateHead("éé\nabc", TruncationOptions{MaxLines: &lines, MaxBytes: &bytes})
	if result.LastLinePartial {
		t.Fatal("head must retain complete lines", result)
	}
	if result.Content != "éé" || result.OutputBytes != 4 || result.FirstLineExceedsLimit || result.TruncatedBy == nil || *result.TruncatedBy != "bytes" {
		t.Fatal(result)
	}
	bytes = 3
	result = TruncateHead("éé\nabc", TruncationOptions{MaxLines: &lines, MaxBytes: &bytes})
	if result.Content != "" || !result.FirstLineExceedsLimit {
		t.Fatal(result)
	}
	for _, tc := range []struct {
		bytes int
		text  string
	}{{1023, "1023B"}, {1536, "1.5KB"}, {3 * 1024 * 1024, "3.0MB"}} {
		if got := formatSize(tc.bytes); got != tc.text {
			t.Fatal(tc, got)
		}
	}
}
