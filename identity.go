package goai

import (
	"encoding/json"
	"fmt"
	"net/url"
	"runtime"
	"strings"
	"unicode/utf8"
)

// PiAIVersion is the upstream API version implemented by this module.
const PiAIVersion = "1.4.0"

// UserAgentMode represents the JSON union "short", "detailed", or {custom:string}.
// A zero value selects the detailed mode. Custom takes precedence over Mode.
type UserAgentMode struct {
	Mode   string
	Custom *string
}

func (m UserAgentMode) MarshalJSON() ([]byte, error) {
	if m.Custom != nil {
		return json.Marshal(struct {
			Custom string `json:"custom"`
		}{*m.Custom})
	}
	mode := m.Mode
	if mode == "" {
		mode = "detailed"
	}
	if mode != "short" && mode != "detailed" {
		return nil, fmt.Errorf("invalid user-agent mode %q", mode)
	}
	return json.Marshal(mode)
}
func (m *UserAgentMode) UnmarshalJSON(data []byte) error {
	var mode string
	if err := json.Unmarshal(data, &mode); err == nil {
		if mode != "short" && mode != "detailed" {
			return fmt.Errorf("invalid user-agent mode %q", mode)
		}
		*m = UserAgentMode{Mode: mode}
		return nil
	}
	var object struct {
		Custom *string `json:"custom"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	if object.Custom == nil {
		return fmt.Errorf("custom user-agent requires a string")
	}
	*m = UserAgentMode{Custom: object.Custom}
	return nil
}

type ClientAttribution struct {
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	Categories []string `json:"categories,omitempty"`
}

// ClientIdentity is persistable request identity. AttributionSet distinguishes
// explicit null (disable inherited attribution) from an omitted attribution.
type ClientIdentity struct {
	UserAgent      *UserAgentMode     `json:"userAgent,omitempty"`
	Attribution    *ClientAttribution `json:"attribution,omitempty"`
	AttributionSet bool               `json:"-"`
	Version        *string            `json:"version,omitempty"`
}

func (i ClientIdentity) MarshalJSON() ([]byte, error) {
	type plain ClientIdentity
	if i.AttributionSet && i.Attribution == nil {
		return json.Marshal(struct {
			plain
			Attribution any `json:"attribution"`
		}{plain: plain(i)})
	}
	return json.Marshal(plain(i))
}
func (i *ClientIdentity) UnmarshalJSON(data []byte) error {
	type plain ClientIdentity
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	_, p.AttributionSet = fields["attribution"]
	*i = ClientIdentity(p)
	return nil
}

type ClientIdentityTarget struct {
	Provider string
	BaseURL  string
}

// MergeClientIdentity applies defined fields from left to right, returning a
// detached result. Explicit attribution null clears an inherited attribution.
func MergeClientIdentity(sources ...*ClientIdentity) *ClientIdentity {
	var out *ClientIdentity
	for _, s := range sources {
		if s == nil {
			continue
		}
		if out == nil {
			out = &ClientIdentity{}
		}
		if s.UserAgent != nil {
			m := *s.UserAgent
			if m.Custom != nil {
				v := *m.Custom
				m.Custom = &v
			}
			out.UserAgent = &m
		}
		if s.Version != nil {
			v := *s.Version
			out.Version = &v
		}
		if s.Attribution != nil || s.AttributionSet {
			out.AttributionSet = true
			out.Attribution = nil
			if s.Attribution != nil {
				a := *s.Attribution
				a.Categories = append([]string(nil), a.Categories...)
				out.Attribution = &a
			}
		}
	}
	return out
}
func GetClientVersion(identity *ClientIdentity) string {
	if identity != nil && identity.Version != nil {
		return *identity.Version
	}
	return PiAIVersion
}
func FormatUserAgent(identity *ClientIdentity) string {
	if identity != nil && identity.UserAgent != nil {
		m := identity.UserAgent
		if m.Custom != nil {
			return *m.Custom
		}
		if m.Mode == "short" {
			return "pi"
		}
	}
	return fmt.Sprintf("pi/%s (%s; go/%s; %s)", GetClientVersion(identity), runtime.GOOS, strings.TrimPrefix(runtime.Version(), "go"), runtime.GOARCH)
}
func identityHostMatches(baseURL, host string) bool {
	u, err := url.Parse(baseURL)
	return err == nil && u.Hostname() == host
}
func identityIsOpenCode(t ClientIdentityTarget) bool {
	return t.Provider == "opencode" || t.Provider == "opencode-go" || identityHostMatches(t.BaseURL, "opencode.ai")
}
func GetAttributionHeaders(target ClientIdentityTarget, identity *ClientIdentity) map[string]string {
	out := map[string]string{}
	if identity == nil || identity.Attribution == nil {
		return out
	}
	a := identity.Attribution
	switch {
	case target.Provider == "openrouter" || strings.Contains(target.BaseURL, "openrouter.ai"):
		out["HTTP-Referer"], out["X-OpenRouter-Title"] = a.URL, a.Name
		if len(a.Categories) > 0 {
			out["X-OpenRouter-Categories"] = strings.Join(a.Categories, ",")
		}
	case target.Provider == "nvidia" || identityHostMatches(target.BaseURL, "integrate.api.nvidia.com"):
		name := a.Name
		if len(name) > 0 {
			_, size := utf8.DecodeRuneInString(name)
			name = strings.ToUpper(name[:size]) + name[size:]
		}
		out["X-BILLING-INVOKE-ORIGIN"] = name
	case identityIsOpenCode(target):
		out["x-opencode-client"] = a.Name
	}
	return out
}
func GetOpenCodeSessionHeaders(target ClientIdentityTarget, sessionID string) map[string]string {
	if sessionID != "" && identityIsOpenCode(target) {
		return map[string]string{"x-opencode-session": sessionID}
	}
	return nil
}
func GetIdentityHeaders(target ClientIdentityTarget, identity *ClientIdentity) map[string]string {
	h := GetAttributionHeaders(target, identity)
	h["User-Agent"] = FormatUserAgent(identity)
	return h
}
func RequestIdentityHeaders(model *Model, opts *StreamOptions) map[string]string {
	var identity *ClientIdentity
	if opts != nil {
		identity = opts.Identity
	}
	return GetIdentityHeaders(ClientIdentityTarget{Provider: string(model.Provider), BaseURL: model.BaseURL}, identity)
}
