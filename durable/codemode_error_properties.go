package durable

import "encoding/json"

// codemodeDeliveryError carries journal-owned rejection properties through the
// native QuickJS callback boundary without changing the host error message.
type codemodeDeliveryError struct {
	message    string
	properties json.RawMessage
}

func (e *codemodeDeliveryError) Error() string                        { return e.message }
func (e *codemodeDeliveryError) ErrorPropertiesJSON() json.RawMessage { return e.properties }
func codemodeRejectedDelivery(delivery CodemodeDelivery) error {
	if delivery.Properties == nil {
		return &codemodeDeliveryError{message: delivery.Error}
	}
	raw, err := json.Marshal(delivery.Properties)
	if err != nil {
		return err
	}
	return &codemodeDeliveryError{message: delivery.Error, properties: raw}
}
