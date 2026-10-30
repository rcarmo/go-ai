package responsestream

import (
	"encoding/json"
	"strings"
)

// MatchesItem retains existing bare-item fixtures but does not finalize an
// active tool using a completion explicitly declaring another item kind.
func MatchesItem(raw []byte, kind string) bool {
	var item struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return false
	}
	return item.Type == "" || item.Type == kind
}

// FinalText projects ordinary final-item text. Missing content retains native
// compatibility with providers that send only deltas. Present content is
// authoritative, including explicitly empty message content.
func FinalText(raw []byte) (text, thinking string, hasText bool) {
	type part struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Refusal string `json:"refusal"`
	}
	var item struct {
		Type    string  `json:"type"`
		Content *[]part `json:"content"`
		Summary *[]part `json:"summary"`
	}
	if json.Unmarshal(raw, &item) != nil {
		return "", "", false
	}
	if item.Type == "message" && item.Content != nil {
		var b strings.Builder
		for _, p := range *item.Content {
			if p.Type == "output_text" {
				b.WriteString(p.Text)
			} else {
				b.WriteString(p.Refusal)
			}
		}
		return b.String(), "", true
	}
	if item.Type == "reasoning" {
		join := func(parts *[]part) string {
			if parts == nil {
				return ""
			}
			values := make([]string, len(*parts))
			for i, p := range *parts {
				values[i] = p.Text
			}
			return strings.Join(values, "\n\n")
		}
		thinking = join(item.Summary)
		if thinking == "" {
			thinking = join(item.Content)
		}
	}
	return "", thinking, false
}
