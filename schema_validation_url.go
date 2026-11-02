package goai

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	whatwg "github.com/nlnwa/whatwg-url/url"
	"golang.org/x/net/idna"
)

// WHATWG non-strict UTS46 processing retains bidi, joiner and label checks.
// DNS length, STD3-only and hyphen restrictions are not URL host rules.

var schemaURLProfile = idna.New(idna.MapForLookup(), idna.BidiRule(), idna.VerifyDNSLength(false), idna.StrictDomainName(false), idna.ValidateLabels(true), idna.CheckHyphens(false), idna.CheckJoiners(true), idna.Transitional(false))

func schemaURLToASCII(domain string) (string, error) {
	ascii, err := schemaURLProfile.ToASCII(domain)
	if err != nil {
		return "", err
	}
	if ascii == "" {
		return "", fmt.Errorf("empty domain")
	}
	if strings.HasPrefix(ascii, ".") || strings.HasSuffix(ascii, ".") || strings.Contains(ascii, "..") {
		// x/net rejects decoded ASCII A-labels only when len(decoded)>0. Detect
		// the empty case after UTS46 mapping without rejecting ordinary empty
		// labels. Prefixing 'a' prevents ACE decoding; mapped 'axn--' is exact.
		mappedDots := strings.NewReplacer("\u3002", ".", "\uff0e", ".", "\uff61", ".").Replace(domain)
		for _, label := range strings.Split(mappedDots, ".") {
			mapped, e := schemaURLProfile.ToASCII("a" + label)
			if e == nil && mapped == "axn--" {
				return "", fmt.Errorf("empty A-label")
			}
		}
	}
	for _, c := range ascii {
		if whatwg.ForbiddenDomainCodePoint.Test(uint(c)) {
			return "", fmt.Errorf("forbidden domain code point")
		}
	}
	return ascii, nil
}

// parseSchemaURL parses without fetching. Validate the actual canonical host
// before shielding ACE labels from the parser's stricter second IDNA pass.
// Temporary labels preserve numeric-tail classification; restoration belongs
// to the particular URL object, including distinct base/reference hosts.
func parseSchemaURL(base, ref string) (*whatwg.Url, error) {
	var parser whatwg.Parser
	restore := make(map[*whatwg.Url]string)
	var hostErr error
	pre := func(u *whatwg.Url, input string) string {
		delete(restore, u)
		switch u.Scheme() {
		case "http", "https", "ws", "wss", "ftp", "file":
		default:
			return input
		}
		if input == "" || strings.HasPrefix(input, "[") {
			return input
		}
		domain, decodeErr := url.PathUnescape(input)
		if decodeErr != nil {
			hostErr = decodeErr
			return "\x00"
		}
		if !utf8.ValidString(domain) {
			hostErr = fmt.Errorf("invalid UTF-8 host")
			return "\x00"
		}
		ascii, err := schemaURLToASCII(domain)
		if err != nil {
			hostErr = err
			return "\x00"
		}
		parts := strings.Split(ascii, ".")
		changed := false
		for i, label := range parts {
			if strings.HasPrefix(label, "xn--") {
				parts[i] = "schema-idna"
				changed = true
			}
		}
		if changed {
			restore[u] = ascii
			return strings.Join(parts, ".")
		}
		return ascii
	}
	post := func(u *whatwg.Url, host string) string {
		if ascii, ok := restore[u]; ok {
			delete(restore, u)
			return ascii
		}
		return host
	}
	parser = whatwg.NewParser(whatwg.WithPreParseHostFunc(pre), whatwg.WithPostParseHostFunc(post))
	out, err := parser.ParseRef(base, ref)
	if hostErr != nil {
		return nil, hostErr
	}
	return out, err
}
