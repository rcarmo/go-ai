#!/usr/bin/env bash
set -euo pipefail

cd "$(git -C "$(dirname -- "$0")" rev-parse --show-toplevel)"
# shellcheck source=/dev/null
source scripts/project-env.sh
: "${GOAI_CODEMODE_V161_PACKAGE_ROOT:?use the verified v1.6.1 portable package root}"
export GOAI_CODEMODE_V161_PACKAGE_ROOT
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1 GOTOOLCHAIN=go1.27.3 GOPROXY=off GOSUMDB=off
scratch=$(mktemp -d "$TMPDIR/codemode-store-replay.XXXXXX")
trap 'rm -r -- "$scratch"' EXIT
exec 9>/tmp/go-ai/v160-focused-verify.lock
flock 9

for tag in diagnostic store; do
  case $tag in
    diagnostic)
      file=durable/codemode_drain_commit.go
      sed 's/where it went on before/scratch omitted the prior continuation/' "$file" > "$scratch/$tag.go"
      test=TestSourceV161CodemodePublishedWorkerRejectsEarlyStoreReplay
      wanted='want exact initial early-return diagnostic' ;;
    store)
      file=durable/codemode_store_parts.go
      sed 's/maps.Copy(values, writes)/maps.Copy(values, JSON{})/' "$file" > "$scratch/$tag.go"
      test=TestV161CodemodePublishedWorkerAdmittedStoreReplay
      wanted='committed conversation store = map[]; want map[x:1]' ;;
  esac
  if cmp -s "$file" "$scratch/$tag.go"; then echo "Control did not change $file" >&2; exit 1; fi
  jq -n --arg src "$PWD/$file" --arg dst "$scratch/$tag.go" '{Replace:{($src):$dst}}' > "$scratch/$tag.overlay.json"
  status=0
  unshare -Urn go test -overlay="$scratch/$tag.overlay.json" ./durable -v -run "^$test$" -count=1 -p=1 -timeout 45s \
    > "$scratch/$tag.log" 2>&1 || status=$?
  if [[ $status != 1 ]] || ! grep -F -- "$wanted" "$scratch/$tag.log"; then
    cat "$scratch/$tag.log" >&2
    exit 1
  fi
done
