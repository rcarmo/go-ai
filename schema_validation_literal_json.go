package goai

import (
	"encoding/json"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// Literal diagnostics use JS JSON spelling and source member order, not Go's
// sorted-map/HTML-escaped encoding. Ordering is not part of value equality.
func (v *toolSchemaValidator) literalJSON(value interface{}) string {
	orders := &toolSchemaResources{orders: make(map[uintptr][]string)}
	if v.resources != nil {
		orders.orders = v.resources.literalOrders
	} else if len(v.schemaRaw) > 0 {
		orders.readOrder(v.root, v.schemaRaw)
	}
	var out strings.Builder
	quote := func(s string) {
		out.WriteByte('"')
		for _, r := range s {
			switch r {
			case '"', '\\':
				out.WriteByte('\\')
				out.WriteRune(r)
			case '\b':
				out.WriteString(`\b`)
			case '\f':
				out.WriteString(`\f`)
			case '\n':
				out.WriteString(`\n`)
			case '\r':
				out.WriteString(`\r`)
			case '\t':
				out.WriteString(`\t`)
			default:
				if r < 32 {
					out.WriteString(`\u00`)
					out.WriteByte("0123456789abcdef"[r>>4])
					out.WriteByte("0123456789abcdef"[r&15])
				} else {
					out.WriteRune(r)
				}
			}
		}
		out.WriteByte('"')
	}
	var write func(interface{})
	write = func(value interface{}) {
		switch value := value.(type) {
		case nil:
			out.WriteString("null")
		case bool:
			out.WriteString(strconv.FormatBool(value))
		case string:
			quote(value)
		case float64:
			if math.IsInf(value, 0) || math.IsNaN(value) {
				out.WriteString("null")
			} else {
				out.WriteString(schemaFloatString(value))
			}
		case int:
			out.WriteString(strconv.Itoa(value))
		case int64:
			out.WriteString(strconv.FormatInt(value, 10))
		case json.Number:
			s, err := schemaDecimalString(value)
			if err != nil {
				out.WriteString("null")
			} else {
				out.WriteString(s)
			}
		case []interface{}:
			out.WriteByte('[')
			for i, item := range value {
				if i > 0 {
					out.WriteByte(',')
				}
				write(item)
			}
			out.WriteByte(']')
		case map[string]interface{}:
			out.WriteByte('{')
			for i, key := range orders.keys(value) {
				if i > 0 {
					out.WriteByte(',')
				}
				quote(key)
				out.WriteByte(':')
				write(value[key])
			}
			out.WriteByte('}')
		default:
			out.WriteString("null")
		}
	}
	write(value)
	return out.String()
}

func (v *toolSchemaValidator) literalEnumList(values []interface{}) string {
	if len(values) <= 20 {
		return v.literalJSON(values)
	}
	return strings.TrimSuffix(v.literalJSON(values[:20]), "]") + ", ... and " + strconv.Itoa(len(values)-20) + " more]"
}

// Retain only data-object key order needed after resource indexing discards
// the full ordering table and original bytes. Literal IDs are never indexed.
func (r *toolSchemaResources) retainLiteralOrder(value interface{}) {
	switch value := value.(type) {
	case map[string]interface{}:
		if r.literalOrders == nil {
			r.literalOrders = make(map[uintptr][]string)
		}
		id := reflect.ValueOf(value).Pointer()
		if keys, ok := r.orders[id]; ok {
			r.literalOrders[id] = keys
		}
		for _, item := range value {
			r.retainLiteralOrder(item)
		}
	case []interface{}:
		for _, item := range value {
			r.retainLiteralOrder(item)
		}
	}
}
