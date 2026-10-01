package goai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
)

// ClassifierQuestion describes one judgement about structured state. Criteria is
// map[string]string for choice, []string for score (lowest first), or
// ClassifierBoolCriteria for bool. JSON decoding restores those concrete types.
type ClassifierQuestion struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria"`
}

// ClassifierBoolCriteria gives the meanings of the two boolean outcomes.
type ClassifierBoolCriteria struct {
	True  string `json:"true"`
	False string `json:"false"`
}

// ClassifierContext uses a JSON object as state, including nested objects,
// arrays, numbers, booleans and null values. String-only state is not supported.
type ClassifierContext struct {
	State     map[string]any                `json:"state"`
	Questions map[string]ClassifierQuestion `json:"questions"`
}

func (c ClassifierContext) MarshalJSON() ([]byte, error) {
	type plain ClassifierContext
	if c.State == nil {
		return nil, fmt.Errorf("classifier state must be a JSON object")
	}
	if c.Questions == nil {
		return nil, fmt.Errorf("classifier questions must be a JSON object")
	}
	// Marshal first to catch cycles before recursively checking the JSON domain.
	data, err := json.Marshal(plain(c))
	if err != nil {
		return nil, err
	}
	if err := validateClassifierState(reflect.ValueOf(c.State)); err != nil {
		return nil, err
	}
	return data, nil
}

func (c *ClassifierContext) UnmarshalJSON(data []byte) error {
	type plain ClassifierContext
	var decoded plain
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if decoded.State == nil {
		return fmt.Errorf("classifier state must be a JSON object")
	}
	if decoded.Questions == nil {
		return fmt.Errorf("classifier questions must be a JSON object")
	}
	if err := validateClassifierState(reflect.ValueOf(decoded.State)); err != nil {
		return err
	}
	*c = ClassifierContext(decoded)
	return nil
}

func (q ClassifierQuestion) MarshalJSON() ([]byte, error) {
	criteria, err := q.normalizedCriteria()
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Type         string `json:"type"`
		Instructions string `json:"instructions"`
		Criteria     any    `json:"criteria"`
	}{q.Type, q.Instructions, criteria})
}

func (q *ClassifierQuestion) UnmarshalJSON(data []byte) error {
	var decoded struct {
		Type         string          `json:"type"`
		Instructions *string         `json:"instructions"`
		Criteria     json.RawMessage `json:"criteria"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.Instructions == nil {
		return fmt.Errorf("classifier question requires instructions")
	}
	next := ClassifierQuestion{Type: decoded.Type, Instructions: *decoded.Instructions, Criteria: decoded.Criteria}
	criteria, err := next.normalizedCriteria()
	if err != nil {
		return err
	}
	next.Criteria = criteria
	*q = next
	return nil
}

func (q ClassifierQuestion) normalizedCriteria() (any, error) {
	data, err := json.Marshal(q.Criteria)
	if err != nil {
		return nil, err
	}
	switch q.Type {
	case "choice":
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
			return nil, fmt.Errorf("choice criteria must be an object of label meanings")
		}
		criteria := make(map[string]string, len(raw))
		for key, value := range raw {
			meaning, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("choice criterion %q must be a string", key)
			}
			criteria[key] = meaning
		}
		return criteria, nil
	case "score":
		var raw []any
		if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
			return nil, fmt.Errorf("score criteria must be an array of level meanings")
		}
		criteria := make([]string, len(raw))
		for index, value := range raw {
			meaning, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("score criterion %d must be a string", index)
			}
			criteria[index] = meaning
		}
		return criteria, nil
	case "bool":
		var criteria struct {
			True  *string `json:"true"`
			False *string `json:"false"`
		}
		if err := json.Unmarshal(data, &criteria); err != nil || criteria.True == nil || criteria.False == nil {
			return nil, fmt.Errorf("bool criteria must contain true and false meanings")
		}
		return ClassifierBoolCriteria{True: *criteria.True, False: *criteria.False}, nil
	default:
		return nil, fmt.Errorf("unknown classifier question type %q", q.Type)
	}
}

// ClassifierAnswer is the discriminator-based union returned by Classify. Its
// JSON shape includes all required fields for its type, even when they are zero.
type ClassifierAnswer struct {
	Type          string
	Choice        string
	Probabilities map[string]float64
	Confidence    float64
	Score         float64
	Probability   float64
}

func (a ClassifierAnswer) MarshalJSON() ([]byte, error) {
	switch a.Type {
	case "choice":
		if a.Probabilities == nil {
			return nil, fmt.Errorf("choice answer probabilities must be a JSON object")
		}
		return json.Marshal(struct {
			Type          string             `json:"type"`
			Choice        string             `json:"choice"`
			Probabilities map[string]float64 `json:"probabilities"`
			Confidence    float64            `json:"confidence"`
		}{a.Type, a.Choice, a.Probabilities, a.Confidence})
	case "score":
		return json.Marshal(struct {
			Type       string  `json:"type"`
			Score      float64 `json:"score"`
			Confidence float64 `json:"confidence"`
		}{a.Type, a.Score, a.Confidence})
	case "bool":
		return json.Marshal(struct {
			Type        string  `json:"type"`
			Probability float64 `json:"probability"`
		}{a.Type, a.Probability})
	default:
		return nil, fmt.Errorf("unknown classifier answer type %q", a.Type)
	}
}

func (a *ClassifierAnswer) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	typeName, _ := raw["type"].(string)
	// System One uses noul on its wire; the public JSON contract uses bool.
	if typeName == "bool" {
		raw["type"], raw["noul"] = "noul", raw["probability"]
	}
	answers, err := parseClassifierAnswers("classifier", map[string]any{"answer": raw}, ClassifierContext{Questions: map[string]ClassifierQuestion{"answer": {Type: typeName}}})
	if err != nil {
		return err
	}
	*a = answers["answer"]
	return nil
}

func finiteClassifierNumber(number float64) bool {
	return !math.IsNaN(number) && !math.IsInf(number, 0)
}

// Only JSON values belong in state. In particular, encoding/json would silently
// coerce structs, byte slices and integer-keyed maps to unrelated JSON shapes.
func validateClassifierState(value reflect.Value) error {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		return validateClassifierState(value.Elem())
	}
	if value.Type() == reflect.TypeFor[json.Number]() {
		number, err := value.Interface().(json.Number).Float64()
		if err != nil || !finiteClassifierNumber(number) {
			return fmt.Errorf("classifier state number must be finite")
		}
		return nil
	}
	switch value.Kind() {
	case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return nil
	case reflect.Float32, reflect.Float64:
		if finiteClassifierNumber(value.Float()) {
			return nil
		}
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			break
		}
		iterator := value.MapRange()
		for iterator.Next() {
			if err := validateClassifierState(iterator.Value()); err != nil {
				return err
			}
		}
		return nil
	case reflect.Array, reflect.Slice:
		if value.Kind() == reflect.Slice && value.Type().Elem().Kind() == reflect.Uint8 {
			break
		}
		for index := 0; index < value.Len(); index++ {
			if err := validateClassifierState(value.Index(index)); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("classifier state contains non-JSON value %s", value.Type())
}
