package images

import "encoding/json"

// Image contexts/results share the source TextContent | ImageContent union.
// Retain the selected arm's required strings, including empty values, while
// keeping any other explicitly populated fields available to callers.
func marshalImageContent(kind, text, data, mimeType string) ([]byte, error) {
	var textValue, dataValue, mimeValue *string
	if kind == "text" || text != "" {
		textValue = &text
	}
	if kind == "image" || data != "" {
		dataValue = &data
	}
	if kind == "image" || mimeType != "" {
		mimeValue = &mimeType
	}
	return json.Marshal(struct {
		Type     string  `json:"type"`
		Text     *string `json:"text,omitempty"`
		Data     *string `json:"data,omitempty"`
		MimeType *string `json:"mimeType,omitempty"`
	}{kind, textValue, dataValue, mimeValue})
}

func (m ImageInput) MarshalJSON() ([]byte, error) {
	return marshalImageContent(m.Type, m.Text, m.Data, m.MimeType)
}

func (m ImageOutput) MarshalJSON() ([]byte, error) {
	return marshalImageContent(m.Type, m.Text, m.Data, m.MimeType)
}
