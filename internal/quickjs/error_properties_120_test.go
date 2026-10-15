package quickjs

import (
	"context"
	"encoding/json"
	"testing"
)

type propertyError120 struct{}

func (propertyError120) Error() string { return "denied" }
func (propertyError120) ErrorPropertiesJSON() json.RawMessage {
	return json.RawMessage(`{"structuredOutput":{"reason":"detail"},"message":"forged"}`)
}
func TestQuickJS120RejectionProperties(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	result := engine.Execute(context.Background(), `try{await tools.leaf({});throw Error('resolved')}catch(e){if(e.message!=='denied'||e.structuredOutput.reason!=='detail')throw Error('lost properties')};return 'ok'`, Options{Tools: []Tool{{Name: "leaf", JSName: "leaf", Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) { return nil, propertyError120{} }}}})
	if result.Error != nil {
		t.Fatal(result.Error)
	}
}
