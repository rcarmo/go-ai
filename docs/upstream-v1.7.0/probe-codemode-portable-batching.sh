#!/usr/bin/env bash
set -euo pipefail

cd "$(git -C "$(dirname -- "$0")" rev-parse --show-toplevel)"
# shellcheck source=/dev/null
source scripts/project-env.sh
: "${GOAI_CODEMODE_V170_PACKAGE_ROOT:?use the verified v1.7.0 portable package root}"
export GOAI_CODEMODE_V170_PACKAGE_ROOT
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1 GOTOOLCHAIN=go1.27.3 GOPROXY=off GOSUMDB=off
scratch=$(mktemp -d "$TMPDIR/codemode-portable-batching.XXXXXX")
trap 'rm -r -- "$scratch"' EXIT
exec 9>/tmp/go-ai/v160-focused-verify.lock
flock 9

# Change only the Go projection of the final console item. The verified
# published guest and observed message grouping stay the same.
file=durable/codemode_quickjs_output.go
sed 's/strings.Join(console, "\\n")/strings.Join(console[:len(console)-1], "\\n")/' "$file" > "$scratch/drop-last.go"
if cmp -s "$file" "$scratch/drop-last.go"; then echo "Control did not change $file" >&2; exit 1; fi
jq -n --arg src "$PWD/$file" --arg dst "$scratch/drop-last.go" '{Replace:{($src):$dst}}' > "$scratch/overlay.json"
status=0
unshare -Urn go test -overlay="$scratch/overlay.json" ./durable -v -run '^TestSourceV170CodemodePublishedPortableOutputBatching$' \
  -count=1 -p=1 -timeout 45s > "$scratch/control.log" 2>&1 || status=$?
if [[ $status != 1 ]] || ! grep -F 'Go output projection differs from the exact console block' "$scratch/control.log"; then
  cat "$scratch/control.log" >&2
  exit 1
fi
grep -E '5000 ordered console items in [1-6] exchanges' "$scratch/control.log"

# Change only a disposable copy of the publisher's flush policy. Keep all
# console lines and make the live subprocess exceed the exchange bound.
mkdir "$scratch/guest"
cp -a -- "$GOAI_CODEMODE_V170_PACKAGE_ROOT/node_modules" "$scratch/guest/"
guest_file="$scratch/guest/node_modules/@earendil-works/pi-codemode/dist/runtime/remote.js"
[[ $(grep -Fxc 'const FLUSH_ITEMS = 1_000;' "$guest_file") == 1 ]]
sed -i 's/^const FLUSH_ITEMS = 1_000;$/const FLUSH_ITEMS = 500;/' "$guest_file"
status=0
GOAI_CODEMODE_V170_PACKAGE_ROOT="$scratch/guest" unshare -Urn go test ./durable -v \
  -run '^TestSourceV170CodemodePublishedPortableOutputBatching$' -count=1 -p=1 -timeout 45s \
  > "$scratch/flush-control.log" 2>&1 || status=$?
if [[ $status != 1 ]] || ! grep -F 'worker produced 5000 items in 11 exchanges; want 5000 in at most six' "$scratch/flush-control.log"; then
  cat "$scratch/flush-control.log" >&2
  exit 1
fi
