package responsestream

import "encoding/json"

// MessageTextSignature records the provider item identity for subsequent replay.
func MessageTextSignature(raw []byte) string {
	var item struct {
		ID    string `json:"id"`
		Phase string `json:"phase"`
	}
	_ = json.Unmarshal(raw, &item)
	payload := struct {
		V     int    `json:"v"`
		ID    string `json:"id"`
		Phase string `json:"phase,omitempty"`
	}{1, item.ID, item.Phase}
	encoded, _ := json.Marshal(payload)
	return string(encoded)
}

// ParseTextSignature accepts version-one signatures and legacy plain item IDs.
// Unknown phases are deliberately excluded from outbound replay.
func ParseTextSignature(signature string) (id, phase string) {
	if len(signature) > 0 && signature[0] == '{' {
		var parsed struct {
			V     float64 `json:"v"`
			ID    *string `json:"id"`
			Phase any     `json:"phase"`
		}
		if json.Unmarshal([]byte(signature), &parsed) == nil && parsed.V == 1 && parsed.ID != nil {
			phase, _ = parsed.Phase.(string)
			if phase != "commentary" && phase != "final_answer" {
				phase = ""
			}
			return *parsed.ID, phase
		}
	}
	return signature, ""
}
func IsFinalAnswer(raw []byte) bool {
	var item struct {
		Phase string `json:"phase"`
	}
	return json.Unmarshal(raw, &item) == nil && item.Phase == "final_answer"
}
