package durable

import goai "github.com/rcarmo/go-ai"

// Decode a bounded plain-JSON identity without teaching the durable JSON
// encoder to invoke arbitrary MarshalJSON hooks. Reject unknown request fields.
func requestIdentity(value JSON, limits Limits) (*goai.ClientIdentity, error) {
	if value == nil {
		return nil, nil
	}
	data, err := encodeBounded(value, limits, limits.MaxRecordBytes)
	if err != nil {
		return nil, err
	}
	var wire struct {
		UserAgent   any `json:"userAgent"`
		Attribution *struct {
			Name       *string  `json:"name"`
			URL        *string  `json:"url"`
			Categories []string `json:"categories"`
		} `json:"attribution"`
		Version *string `json:"version"`
	}
	if err = decodeStrict(data, limits, limits.MaxRecordBytes, &wire); err != nil {
		return nil, err
	}
	identity := &goai.ClientIdentity{Version: wire.Version}
	if _, present := value["version"]; present && wire.Version == nil {
		return nil, reject("invalid identity version")
	}
	if _, present := value["userAgent"]; present {
		switch mode := wire.UserAgent.(type) {
		case string:
			if mode != "short" && mode != "detailed" {
				return nil, reject("invalid identity user agent")
			}
			identity.UserAgent = &goai.UserAgentMode{Mode: mode}
		case map[string]any:
			custom, ok := mode["custom"].(string)
			if !ok || len(mode) != 1 {
				return nil, reject("invalid identity user agent")
			}
			identity.UserAgent = &goai.UserAgentMode{Custom: &custom}
		default:
			return nil, reject("invalid identity user agent")
		}
	}
	_, identity.AttributionSet = value["attribution"]
	if wire.Attribution != nil {
		if wire.Attribution.Name == nil || wire.Attribution.URL == nil {
			return nil, reject("invalid identity attribution")
		}
		identity.Attribution = &goai.ClientAttribution{Name: *wire.Attribution.Name, URL: *wire.Attribution.URL, Categories: wire.Attribution.Categories}
	}
	return identity, nil
}

func boundedIdentity(identity *goai.ClientIdentity, limits Limits) (*goai.ClientIdentity, error) {
	if identity == nil {
		return nil, nil
	}
	value := JSON{}
	if identity.Version != nil {
		value["version"] = *identity.Version
	}
	if identity.UserAgent != nil {
		if identity.UserAgent.Custom != nil {
			value["userAgent"] = JSON{"custom": *identity.UserAgent.Custom}
		} else {
			mode := identity.UserAgent.Mode
			if mode == "" {
				mode = "detailed"
			}
			value["userAgent"] = mode
		}
	}
	if identity.Attribution != nil {
		value["attribution"] = identity.Attribution
	} else if identity.AttributionSet {
		value["attribution"] = nil
	}
	return requestIdentity(value, limits)
}
