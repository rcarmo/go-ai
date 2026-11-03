package goai_test

import (
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
)

// Official pi-ai v1.5.0 f9a26365 src/utils/version.ts; native platform fields
// remain Go-specific. This checks release metadata, not overall API parity.
func TestOfficial150DefaultIdentityVersion(t *testing.T) {
	if got := goai.GetClientVersion(nil); got != "1.5.0" {
		t.Errorf("default release version=%q want 1.5.0", got)
	}
	if got := goai.FormatUserAgent(nil); !strings.HasPrefix(got, "pi/1.5.0 (") {
		t.Errorf("default user agent=%q", got)
	}
	version := "app-version"
	if got := goai.GetClientVersion(&goai.ClientIdentity{Version: &version}); got != version {
		t.Errorf("custom version=%q", got)
	}
}
