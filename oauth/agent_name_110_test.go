package oauth

import (
	"net/url"
	"testing"
)

func TestChatGPT110AgentNameOmittedExplicitAndEmpty(t *testing.T) {
	provider := NewOpenAIChatGPTProvider(nil)
	for _, name := range []*string{nil, func() *string { x := "my-host"; return &x }(), func() *string { x := ""; return &x }()} {
		raw, err := provider.authorizationURL("b6305a47-9a75-42e9-9fc4-486f11af1801", "http://localhost:1455/auth/callback", "state", "challenge", "nonce", name)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		want := openAIChatGPTAgentNameHint
		if name != nil {
			want = *name
		}
		if got := u.Query().Get("agent_name_hint"); got != want {
			t.Fatal("name override lost", got, want)
		}
		if u.Query().Get("state") != "state" || u.Query().Get("code_challenge") != "challenge" {
			t.Fatal("OAuth authority fields changed", u)
		}
	}
}
