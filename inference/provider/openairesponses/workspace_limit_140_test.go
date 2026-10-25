package openairesponses

import (
	"strings"
	"testing"
)

func TestWorkspaceLimit140Guidance(t *testing.T) {
	for _, tc := range []struct{ message, hint string }{
		{`OpenAI API error (429): {"code":"subscription_sharing_workplace_individual_usage_limit_exceeded"}`, chatGPTWorkspaceLimitHint},
		{`OpenAI API error (429): {"code":"subscription_sharing_usage_limit_exceeded"}`, chatGPTUsageURL},
	} {
		got := addChatGPTUsageGuidance(tc.message)
		if !strings.Contains(got, tc.hint) || !strings.HasPrefix(got, tc.message) {
			t.Fatalf("missing guidance or original error: %s", got)
		}
		if twice := addChatGPTUsageGuidance(got); twice != got {
			t.Fatalf("duplicated guidance: %s", twice)
		}
	}
	if got := addChatGPTUsageGuidance("ordinary error"); got != "ordinary error" {
		t.Fatal(got)
	}
}
