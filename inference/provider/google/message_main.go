package google

import "encoding/json"

// A signed empty text/thinking part must carry text:"" on the wire. Other
// part kinds must not gain a text field alongside functionCall/inlineData.
func (p geminiPart) MarshalJSON() ([]byte, error) {
	type plain geminiPart
	if !p.ForceEmptyText {
		return json.Marshal(plain(p))
	}
	return json.Marshal(struct {
		plain
		Text string `json:"text"`
	}{plain: plain(p), Text: p.Text})
}
