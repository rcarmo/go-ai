package durable

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestReceiptDuration110ContributionAndOwnedProjection(t *testing.T) {
	duration := int64(0)
	for _, role := range []goai.Role{goai.RoleAssistant, goai.RoleToolResult} {
		r, err := contributionReceipt(goai.Message{Role: role, Content: []goai.ContentBlock{{Type: "text", Text: "result"}}, DurationMs: &duration}, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		if r.DurationMs == nil || *r.DurationMs != 0 {
			t.Fatal("explicit zero lost", r)
		}
		owned, err := copyMessages([]MessageReceipt{r}, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		*r.DurationMs = 123
		if owned[0].DurationMs == nil || *owned[0].DurationMs != 0 {
			t.Fatal("receipt alias", owned)
		}
		m := receiptMessage(owned[0])
		*m.DurationMs = 321
		if *owned[0].DurationMs != 0 {
			t.Fatal("projection alias", owned)
		}
	}
}
