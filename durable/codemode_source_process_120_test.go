package durable

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCodemode120OfficialSourceNumberComparison(t *testing.T) {
	root := os.Getenv("GOAI_WORKER_PACKAGE_ROOT")
	if root == "" {
		t.Skip("opt-in: exact pi-codemode@1.4.0 install required")
	}
	sources := []string{}
	for _, raw := range []string{"1", "1.0", "1.0000000000000001", "9007199254740991.1", "9007199254740992", "-0", "1e-9999", "1e9999", "1.5", "true", "null", `"1"`} {
		sources = append(sources, "// @options: {\"max_output_tokens\":"+raw+"}\nreturn 1")
	}
	sources = append(sources, "// @options: {\"timeout_ms\":false,\"timeout_ms\":2}\nreturn 1", "// @options: {\"max_output_tokens\":1e9999,\"max_output_tokens\":2}\nreturn 1", "// @options: {\"timeout_ms\":1}\r\nreturn 1")
	input, err := json.Marshal(sources)
	if err != nil {
		t.Fatal(err)
	}
	script := `import {pathToFileURL} from 'node:url';const {parseCodemodeSource}=await import(pathToFileURL(process.env.GOAI_WORKER_PACKAGE_ROOT+'/node_modules/@earendil-works/pi-codemode/dist/source.js'));let text='';for await(const chunk of process.stdin)text+=chunk;console.log(JSON.stringify(JSON.parse(text).map(source=>{try{const p=parseCodemodeSource(source);return {ok:true,code:p.code,timeout:p.options.timeoutMs??300000,tokens:p.options.maxOutputTokens??10000}}catch{return {ok:false}}})));`
	cmd := exec.Command("node", "--input-type=module", "-e", script)
	cmd.Stdin = strings.NewReader(string(input))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(err, string(output))
	}
	var expected []struct {
		OK      bool   `json:"ok"`
		Code    string `json:"code"`
		Timeout int64  `json:"timeout"`
		Tokens  int    `json:"tokens"`
	}
	if err = json.Unmarshal(output, &expected); err != nil {
		t.Fatal(err)
	}
	if len(expected) != len(sources) {
		t.Fatal("missing parser cases")
	}
	for i, source := range sources {
		got, err := ParseCodemodeSource(source)
		want := expected[i]
		if (err == nil) != want.OK || err == nil && (got.Code != want.Code || got.TimeoutMs != want.Timeout || got.MaxOutputTokens != want.Tokens) {
			t.Fatalf("source %q: Go %+v %v; official %+v", source, got, err, want)
		}
	}
}
