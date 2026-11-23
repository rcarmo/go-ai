#!/usr/bin/env bash
set -euo pipefail

if [[ $# != 2 ]]; then
  printf 'Usage: bash %s <pinned-git-mirror> <user-npm-cacache>\n' "$0" >&2
  exit 1
fi
mirror=$(realpath -- "$1")
cache=$(realpath -- "$2")
here=$(cd -- "$(dirname -- "$0")" && pwd -P)
cd "$(git -C "$here" rev-parse --show-toplevel)"
# shellcheck source=/dev/null
source scripts/project-env.sh
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1
export GOTOOLCHAIN=go1.27.3 GOPROXY=off GOSUMDB=off GOWORK=off GOVCS='*:off'

pin=faac585884ae2c013a6b1400f3e9e77f10b34bac
engine=internal/quickjs/engine.go
asset=internal/quickjs/guest/quickjs.wasm
source_test=packages/codemode/test/sandbox.test.ts
receipt="$here/quickjs-wasm-load.json"
scratch=$(mktemp -d "$TMPDIR/native-quickjs-wasm-load.XXXXXX")
trap 'rm -r -- "$scratch"' EXIT

sha256() { sha256sum -- "$1" | cut -d' ' -f1; }
wasm_receipt() {
  jq -n --arg sha "$(sha256 "$1")" --argjson bytes "$(wc -c < "$1")" \
    '{compiles:1,bytes:$bytes,sha256:$sha}'
}

# Verify the retained archive against the pinned lock and the previous load receipt.
jq -e --arg pin "$pin" --arg test "$source_test" \
  '.pin==$pin and .selectedOriginal.test==$test and .selectedOriginal.passed==1' "$receipt" > "$scratch/oracle.check"
git -C "$mirror" show "$pin:package-lock.json" > "$scratch/lock.json"
git -C "$mirror" show "$pin:$source_test" > "$scratch/original.ts"
[[ $(sha256 "$scratch/original.ts") == "$(jq -er '.selectedOriginal.sha256' "$receipt")" ]]
read -r version url integrity < <(jq -er '.packages["node_modules/quickjs-wasi"] | [.version,.resolved,.integrity] | @tsv' "$scratch/lock.json")
[[ $version == 3.6.2 && $url == "$(jq -er '.archive.url' "$receipt")" && $integrity == "$(jq -er '.archive.integrity' "$receipt")" ]]
key=$(jq -er '.archive.cacheKey' "$receipt")
[[ $key =~ ^content-v2/sha512/[0-9a-f]{2}/[0-9a-f]{2}/[0-9a-f]{124}$ ]]
archive="$cache/$key"
[[ $(sha256 "$archive") == "$(jq -er '.archive.sha256' "$receipt")" ]]
[[ sha512-$(openssl dgst -sha512 -binary "$archive" | openssl base64 -A) == "$integrity" ]]
tar -xOzf "$archive" -- package/quickjs.wasm > "$scratch/archive.wasm"
[[ $(sha256 "$scratch/archive.wasm") == "$(jq -er '.compiled.sha256' "$receipt")" ]]
[[ $(wc -c < "$scratch/archive.wasm") == "$(jq -er '.compiled.bytes' "$receipt")" ]]

# Use the real NewEngine call path and compiler. The overlay only adds this
# observer and redirects the single CompileModule call through it.
anchor=$'\tcompiled, err := r.CompileModule(ctx, quickjsWasm)'
[[ $(grep -Fxc -- "$anchor" "$engine") == 1 ]]
sed 's/compiled, err := r\.CompileModule(ctx, quickjsWasm)/compiled, err := auditNativeQuickJSCompile(ctx, r, quickjsWasm)/' "$engine" > "$scratch/engine.go"
cat > "$scratch/audit.go" <<'GO'
package quickjs

import (
	"bytes"
	"context"

	"github.com/tetratelabs/wazero"
)

var auditNativeQuickJSInputs [][]byte

func auditNativeQuickJSCompile(ctx context.Context, r wazero.Runtime, wasm []byte) (wazero.CompiledModule, error) {
	input := bytes.Clone(wasm)
	compiled, err := r.CompileModule(ctx, wasm)
	if err == nil {
		auditNativeQuickJSInputs = append(auditNativeQuickJSInputs, input)
	}
	return compiled, err
}
GO
cat > "$scratch/audit_test.go" <<'GO'
package quickjs

import (
	"context"
	"os"
	"testing"
)

// Native translation of the pinned inline sandbox JSON round-trip case.
func TestNativeQuickJSWASMLoad(t *testing.T) {
	ctx := context.Background()
	e, err := NewEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := e.Close(ctx); err != nil {
			t.Error(err)
		}
	})

	for i, tc := range []struct {
		code string
		want string
	}{
		{"return { a: 1, b: [true, 'x'] }", `{"a":1,"b":[true,"x"]}`},
		{"return 'plain'", `"plain"`},
		{"", ""},
	} {
		result := e.Execute(ctx, tc.code, Options{})
		if !result.OK || result.Error != nil || string(result.Value) != tc.want ||
			(result.Value == nil) != (tc.want == "") || len(result.Output) != 0 || len(result.Calls) != 0 {
			t.Fatalf("execution %d = %+v (value %q), want success with value %q and no output or calls", i, result, result.Value, tc.want)
		}
	}
	path := os.Getenv("GO_AI_AUDIT_WASM")
	if path == "" {
		if got := len(auditNativeQuickJSInputs); got != 0 {
			t.Fatalf("unobserved baseline unexpectedly recorded %d compiler calls", got)
		}
		return
	}
	if got := len(auditNativeQuickJSInputs); got != 1 {
		t.Fatalf("successful compiler calls = %d, want 1", got)
	}
	if err := os.WriteFile(path, auditNativeQuickJSInputs[0], 0o600); err != nil {
		t.Fatal(err)
	}
}
GO

# The baseline uses untouched engine and asset files. The control appends a
# valid WASM custom section to the overlaid embedded file. All three runs use
# the same translated behaviour test.
cp -- "$asset" "$scratch/control.wasm"
printf '\000\007\005audit\000' >> "$scratch/control.wasm"
for scenario in baseline original control; do
  jq -n --arg root "$PWD" --arg scratch "$scratch" --arg engine "$engine" --arg asset "$asset" \
    --arg scenario "$scenario" '{Replace:{
      ($root+"/internal/quickjs/native_wasm_audit.go"):($scratch+"/audit.go"),
      ($root+"/internal/quickjs/native_wasm_audit_test.go"):($scratch+"/audit_test.go")
    } + (if $scenario=="baseline" then {} else {($root+"/"+$engine):($scratch+"/engine.go")} end)
      + (if $scenario=="control" then {($root+"/"+$asset):($scratch+"/control.wasm")} else {} end)}' \
    > "$scratch/$scenario.overlay.json"
done

exec 9>/tmp/go-ai/v160-focused-verify.lock
flock 9
[[ $(go env GOVERSION) == go1.27.3 ]]
module=github.com/tetratelabs/wazero
wazero=$(go list -mod=readonly -m -json "$module" | jq -er --arg module "$module" \
  'select(.Path==$module and .Version=="v1.12.0" and .Replace==null) | .Version')
for scenario in baseline original control; do
  observed="$scratch/$scenario.observed.wasm"
  if [[ $scenario == baseline ]]; then observed=''; fi
  if ! GO_AI_AUDIT_WASM="$observed" go test -mod=readonly \
    -overlay "$scratch/$scenario.overlay.json" -run '^TestNativeQuickJSWASMLoad$' \
    -count=1 -timeout=110s -json ./internal/quickjs > "$scratch/$scenario.jsonl"; then
    cat "$scratch/$scenario.jsonl" >&2
    exit 1
  fi
  jq -es '[.[] | select(.Test=="TestNativeQuickJSWASMLoad" and .Action=="pass")] | length==1' \
    "$scratch/$scenario.jsonl" > "$scratch/$scenario.pass"
done
platform="$(go env GOOS)/$(go env GOARCH)"
flock -u 9

cmp -- "$scratch/archive.wasm" "$scratch/original.observed.wasm"
cmp -- "$scratch/control.wasm" "$scratch/control.observed.wasm"
if cmp -s -- "$scratch/archive.wasm" "$scratch/control.observed.wasm"; then
  printf 'Control unexpectedly matched the archive bytes\n' >&2
  exit 1
fi
[[ $(sha256 "$scratch/control.observed.wasm") != "$(sha256 "$scratch/archive.wasm")" ]]

jq -n --arg pin "$pin" --arg platform "$platform" --arg wazero "$wazero" \
  --arg source "$engine" --arg sourceSha "$(sha256 "$engine")" \
  --arg asset "$asset" --arg archiveSha "$(sha256 "$archive")" \
  --argjson original "$(wasm_receipt "$scratch/original.observed.wasm")" \
  --argjson control "$(wasm_receipt "$scratch/control.observed.wasm")" \
  --argjson upstream "$(jq -c '.selectedOriginal' "$receipt")" '
  {format:1,pin:$pin,go:"go1.27.3",cgoEnabled:false,platform:$platform,wazero:$wazero,
    oracle:$upstream,archive:{version:"3.6.2",member:"package/quickjs.wasm",sha256:$archiveSha},
    native:{test:"TestNativeQuickJSWASMLoad",source:$source,sourceSha256:$sourceSha,
      asset:$asset,api:"NewEngine -> wazero.Runtime.CompileModule -> Engine.Execute",
      baselineExecutions:3,executions:3,compiled:$original,provenance:"archive-byte-match"},
    control:{change:"append-valid-custom-section-to-overlaid-embedded-wasm",
      executions:3,compiled:$control,provenance:"archive-byte-and-digest-mismatch"}}'
