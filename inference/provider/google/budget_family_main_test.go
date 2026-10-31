package google

// Source-derived cases from getGoogleBudget in both adapters at 81f3cd41.
// Family substring matching is case-sensitive; custom budgets precede it.
import (
	"fmt"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestMainSourceGoogleBudgetFamilyCase(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		for _, tc := range []struct {
			id   string
			want int
		}{
			{"gemini-2.5-pro", 32768}, {"gemini-2.5-flash-lite", 24576}, {"gemini-2.5-flash", 24576},
			{"GEMINI-2.5-PRO", -1}, {"GEMINI-2.5-FLASH-LITE", -1}, {"GEMINI-2.5-FLASH", -1},
			{"gemini-2.5-Pro", -1}, {"gemini-2.5-Flash-lite", -1}, {"gemini-2.5-Flash", -1},
			{"CUSTOM-2.5-pro", 32768},
		} {
			t.Run(fmt.Sprint(api)+"/"+tc.id, func(t *testing.T) {
				got := captureUpstreamGoogleThinking(t, upstreamGoogleThinkingModel(api, tc.id, nil), "high", nil)
				if got["thinkingBudget"] != float64(tc.want) || got["includeThoughts"] != true {
					t.Fatalf("thinking %#v want budget %d", got, tc.want)
				}
			})
		}
		t.Run(fmt.Sprint(api)+"/custom overrides unknown family", func(t *testing.T) {
			zero := 0
			got := captureUpstreamGoogleThinking(t, upstreamGoogleThinkingModel(api, "GEMINI-2.5-PRO", nil), "high", &goai.ThinkingBudgets{High: &zero})
			if got["thinkingBudget"] != float64(0) {
				t.Fatal(got)
			}
		})
	}
}
