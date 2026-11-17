package durable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/rcarmo/go-ai/internal/jsonparse"
)

// A Tool3 execute checkpoint is a source tool-argument domain, not arbitrary
// checkpoint JSON. Qualify it through the surrounding typed task record.
func canonicalToolArgumentsRaw(raw []byte, base int, typ reflect.Type) (rangeRawField, bool, error) {
	if typ != reflect.TypeFor[Task]() && typ != reflect.TypeFor[TaskRecord]() {
		return rangeRawField{}, false, nil
	}
	fields, err := rangeObject(raw, base)
	if err != nil {
		return rangeRawField{}, false, err
	}
	field := func(fields []rangeRawField, name string) rangeRawField {
		for _, f := range fields {
			if strings.EqualFold(f.name, name) {
				return f
			}
		}
		return rangeRawField{}
	}
	kind := field(fields, "kind")
	var name string
	if len(kind.raw) == 0 || json.Unmarshal(kind.raw, &name) != nil || name != "pi.tool" {
		return rangeRawField{}, false, nil
	}
	version, state := field(fields, "version"), field(fields, "state")
	if typ == reflect.TypeFor[Task]() {
		execution := field(fields, "execution")
		fields, err = rangeObject(execution.raw, execution.start)
		if err != nil {
			return rangeRawField{}, false, err
		}
		tag := field(fields, "tag")
		var mode string
		if json.Unmarshal(tag.raw, &mode) != nil || mode != nativeTaskTag {
			return rangeRawField{}, false, nil
		}
		native := field(fields, "native")
		fields, err = rangeObject(native.raw, native.start)
		if err != nil {
			return rangeRawField{}, false, err
		}
		version, state = field(fields, "version"), field(fields, "state")
	}
	var revision uint64
	if json.Unmarshal(version.raw, &revision) != nil || revision != 3 {
		return rangeRawField{}, false, nil
	}
	fields, err = rangeObject(state.raw, state.start)
	if err != nil {
		return rangeRawField{}, false, err
	}
	checkpoint := field(fields, "checkpoint")
	fields, err = rangeObject(checkpoint.raw, checkpoint.start)
	if err != nil {
		return rangeRawField{}, false, err
	}
	phase := field(fields, "phase")
	var at string
	if json.Unmarshal(phase.raw, &at) != nil || at != "execute" {
		return rangeRawField{}, false, nil
	}
	arguments := field(fields, "arguments")
	return arguments, len(arguments.raw) > 0, nil
}

// json.Decoder replaces isolated surrogates. Restore only the qualified Tool3
// argument subtree from its original bytes, before native validation/freezing.
func restoreCanonicalToolArguments(raw []byte, target any) error {
	if !bytes.Contains(raw, []byte(`\ud`)) && !bytes.Contains(raw, []byte(`\uD`)) {
		return nil
	}
	var walk func(reflect.Value, []byte) error
	walk = func(value reflect.Value, raw []byte) error {
		for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
			if value.IsNil() {
				return nil
			}
			value = value.Elem()
		}
		if !value.IsValid() {
			return nil
		}
		arguments, qualified, err := canonicalToolArgumentsRaw(raw, 0, value.Type())
		if err != nil {
			return err
		}
		if qualified {
			parsed, err := jsonparse.ParseJSONValue(string(arguments.raw), true)
			if err != nil {
				return err
			}
			object, ok := parsed.(map[string]any)
			if !ok {
				return reject("tool execute arguments must be object")
			}
			switch value.Type() {
			case reflect.TypeFor[Task]():
				task := value.Interface().(Task)
				taskCheckpointMap(task.Execution.Native.State.Checkpoint)["arguments"] = sourceToolArguments(object)
			case reflect.TypeFor[TaskRecord]():
				task := value.Interface().(TaskRecord)
				taskCheckpointMap(task.State.Checkpoint)["arguments"] = sourceToolArguments(object)
			}
		}
		switch value.Kind() {
		case reflect.Struct:
			fields, err := rangeObject(raw, 0)
			if err != nil {
				return err
			}
			for i := 0; i < value.NumField(); i++ {
				sf := value.Type().Field(i)
				if sf.PkgPath != "" {
					continue
				}
				name := strings.Split(sf.Tag.Get("json"), ",")[0]
				if name == "-" {
					continue
				}
				if sf.Anonymous && name == "" {
					if err := walk(value.Field(i), raw); err != nil {
						return err
					}
					continue
				}
				if name == "" {
					name = sf.Name
				}
				for _, field := range fields {
					if strings.EqualFold(field.name, name) {
						if err := walk(value.Field(i), field.raw); err != nil {
							return err
						}
					}
				}
			}
		case reflect.Slice, reflect.Array:
			if value.Type().Elem().Kind() == reflect.Uint8 {
				return nil
			}
			var items []json.RawMessage
			if err := json.Unmarshal(raw, &items); err != nil {
				return err
			}
			for i, item := range items {
				if i < value.Len() {
					if err := walk(value.Index(i), item); err != nil {
						return err
					}
				}
			}
		case reflect.Map:
			// Dynamic JSON bags have no typed-task authority. Typed storage maps do.
			if value.Type().Elem().Kind() == reflect.Interface {
				return nil
			}
			fields, err := rangeObject(raw, 0)
			if err != nil {
				return err
			}
			for _, key := range value.MapKeys() {
				for _, field := range fields {
					if field.name == fmt.Sprint(key.Interface()) {
						if err := walk(value.MapIndex(key), field.raw); err != nil {
							return err
						}
					}
				}
			}
		}
		return nil
	}
	return walk(reflect.ValueOf(target), raw)
}
