package mistral

import (
	"bytes"
	"encoding/json"
	"math"

	"github.com/rcarmo/go-ai/internal/jsonparse"
)

// Mistral permits string fragments or JSON values. Keep this opt-in local;
// ArgumentText remains a strict string-only domain for other providers.
type mistralArgumentText struct {
	text    string
	present bool
}

func (s *mistralArgumentText) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '"' {
		var text jsonparse.ArgumentText
		if err := json.Unmarshal(raw, &text); err != nil {
			return err
		}
		*s = mistralArgumentText{text: string(text), present: true}
		return nil
	}
	value, err := jsonparse.ParseJSONValue(string(raw), false)
	if err != nil {
		return err
	}
	// The source uses JSON.stringify(arguments || {}), after JS JSON decoding.
	falsy := value == nil
	switch v := value.(type) {
	case bool:
		falsy = !v
	case float64:
		falsy = v == 0 || math.IsNaN(v)
	}
	if falsy {
		value = map[string]any{}
	}
	encoded, err := jsonparse.MarshalParsedJSONValue(value)
	if err != nil {
		return err
	}
	*s = mistralArgumentText{text: string(encoded), present: true}
	return nil
}
