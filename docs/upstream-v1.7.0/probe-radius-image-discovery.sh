#!/usr/bin/env bash
set -euo pipefail

cd "$(git -C "$(dirname -- "$0")" rev-parse --show-toplevel)"
# shellcheck source=/dev/null
source scripts/project-env.sh
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1 GOTOOLCHAIN=go1.27.3 GOPROXY=off GOSUMDB=off
scratch=$(mktemp -d "$TMPDIR/radius-image-discovery.XXXXXX")
trap 'rm -r -- "$scratch"' EXIT
exec 9>/tmp/go-ai/v160-focused-verify.lock
flock 9

# This proof is deliberately red on the text-only runtime. It does not
# turn the missing image into an accepted production contract.
file=oauth/radius_runtime_test.go
cp -- "$file" "$scratch/runtime_test.go"
cat >> "$scratch/runtime_test.go" <<'GO'

func TestRadiusRuntimeImageDiscoveryOracle(t *testing.T) {
    gateway, configs, imageRequests := newRadiusImageGateway(t)
    store := refreshRadiusImageFixture(t, gateway)
    entry, err := store.Read("radius-image-fixture")
    if err != nil || entry == nil || len(entry.Models) != 2 || configs.Load() != 1 || imageRequests.Load() != 0 {
        t.Fatalf("fixture did not reach expected cache boundary: entry=%+v config=%d image=%d err=%v", entry, configs.Load(), imageRequests.Load(), err)
    }
    rootFound := goai.GetImageModel("radius-image-fixture", "nano-banana") != nil
    imagesFound := images.GetImageModel("radius-image-fixture", "nano-banana") != nil
    if !rootFound && !imagesFound {
        t.Fatalf("typed Radius image missing after live refresh and fresh cache-only runtime: root=%t images=%t", rootFound, imagesFound)
    }
}
GO
jq -n --arg src "$PWD/$file" --arg dst "$scratch/runtime_test.go" '{Replace:{($src):$dst}}' > "$scratch/overlay.json"
status=0
unshare -Urn sh -ec 'ip link set lo up; exec "$@"' sh go test -overlay="$scratch/overlay.json" ./oauth -v \
  -run '^TestRadiusRuntimeImageDiscoveryOracle$' -count=1 -p=1 -timeout 45s > "$scratch/red.log" 2>&1 || status=$?
if [[ $status != 1 ]] || ! grep -F -- '--- FAIL: TestRadiusRuntimeImageDiscoveryOracle (' "$scratch/red.log" >/dev/null; then
  cat "$scratch/red.log" >&2
  exit 1
fi
grep -F 'typed Radius image missing after live refresh and fresh cache-only runtime: root=false images=false' "$scratch/red.log"
