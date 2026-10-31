package google

import "encoding/json"

// Distinguish an explicitly empty text part from a function/image/signature-only
// part. Decoding through a non-method alias avoids recursion and a second parse.
func (p *geminiPart) UnmarshalJSON(data []byte) error {
	type plain geminiPart
	var wire struct {
		plain
		Text *string `json:"text"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*p = geminiPart(wire.plain)
	if wire.Text != nil {
		p.Text = *wire.Text
		p.TextPresent = true
	}
	return nil
}

// A signed empty text/thinking part must carry text:"" on the wire. Other
// part kinds must not gain a text field alongside functionCall/inlineData.
func (p geminiPart) MarshalJSON() ([]byte, error) {
	type plain geminiPart
	if !p.TextPresent {
		return json.Marshal(plain(p))
	}
	return json.Marshal(struct {
		plain
		Text string `json:"text"`
	}{plain: plain(p), Text: p.Text})
}
