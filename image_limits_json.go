package goai

import "encoding/json"

func (r *ModelImageResizeOptions) UnmarshalJSON(data []byte) error {
	type plain ModelImageResizeOptions
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	present := func(key string) bool { raw, ok := fields[key]; return ok && string(raw) != "null" }
	value.MaxWidthSet = value.MaxWidthSet || present("maxWidth")
	value.MaxHeightSet = value.MaxHeightSet || present("maxHeight")
	value.MaxBytesSet = value.MaxBytesSet || present("maxBytes")
	*r = ModelImageResizeOptions(value)
	return nil
}
