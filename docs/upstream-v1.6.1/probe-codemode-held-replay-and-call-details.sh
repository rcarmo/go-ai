#!/usr/bin/env bash
set -euo pipefail

cd "$(git -C "$(dirname -- "$0")" rev-parse --show-toplevel)"
# shellcheck source=/dev/null
source scripts/project-env.sh
: "${GOAI_CODEMODE_V161_PACKAGE_ROOT:?use the verified v1.6.1 portable package root}"
export GOAI_CODEMODE_V161_PACKAGE_ROOT
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1 GOTOOLCHAIN=go1.27.3 GOPROXY=off GOSUMDB=off
scratch=$(mktemp -d "$TMPDIR/codemode-held-replay.XXXXXX")
trap 'rm -r -- "$scratch"' EXIT
exec 9>/tmp/go-ai/v160-focused-verify.lock
flock 9

for tag in details admission priority; do
  case $tag in
    details)
      file=durable/codemode_call_details.go
      sed 's/"status": "cancelled"/"status": "ok"/' "$file" > "$scratch/$tag.go"
      test='^TestSourceV161CodemodePublishedWorkerCallDetails$'
      wanted='Name:hold Args:{"id":9} Status:ok' ;;
    admission)
      file=durable/codemode_parallel.go
      # Scratch-only: let the first new fixture echo run before the second
      # mismatch. The selected test registers only local fake tools.
      sed '/replayMismatch = mismatch/a\                                if old.Global \&\& !call.Global { _, err := r.executeCodemodeCall(linked, key, start, call, globals, paid); if err != nil { return CodemodeResult{}, err } }' "$file" > "$scratch/$tag.go"
      test='^TestSourceV161CodemodePublishedWorkerHeldBatchBeforeLaunch$/^grouped$'
      wanted='echo effects=map[new:1], nested tasks=2' ;;
    priority)
      file=durable/codemode_parallel.go
      sed 's/if !old.Global {/if !old.Global \&\& replayMismatch == "" {/' "$file" > "$scratch/$tag.go"
      test='^TestSourceV161CodemodePublishedWorkerHeldBatchBeforeLaunch$/^grouped$'
      wanted='call 1 is tools.echo' ;;
  esac
  if cmp -s "$file" "$scratch/$tag.go"; then echo "Control did not change $file" >&2; exit 1; fi
  jq -n --arg src "$PWD/$file" --arg dst "$scratch/$tag.go" '{Replace:{($src):$dst}}' > "$scratch/$tag.overlay.json"
  status=0
  unshare -Urn go test -overlay="$scratch/$tag.overlay.json" ./durable -v -run "$test" -count=1 -p=1 -timeout 45s \
    > "$scratch/$tag.log" 2>&1 || status=$?
  if [[ $status != 1 ]] || ! grep -F -- "$wanted" "$scratch/$tag.log"; then
    cat "$scratch/$tag.log" >&2
    exit 1
  fi
done
