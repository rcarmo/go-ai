#!/usr/bin/env bash
# RED acceptance gate until source generation/tool publication contracts land.
# A nonzero test result is a real open deviation, never a release pass.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
cd "$root"
source scripts/project-env.sh
export CGO_ENABLED=0 GOMAXPROCS=2
scratch=$(mktemp -d "$PROJECT_TMP_ROOT/runs/builtin-publications-XXXXXX")
trap 'rm -rf -- "$scratch"' EXIT
printf '{"Replace":{"%s":"%s"}}\n' \
  "$root/durable/zz_builtin_publication_contract_v150_test.go" \
  "$root/durable/testdata/upstream-main/pending/builtin_publication_contract_v150_test.go" > "$scratch/overlay.json"
make test PACKAGES=./durable TEST_FLAGS="-overlay=$scratch/overlay.json -run TestPendingBuiltinCanonicalPublicationsV150 -count=1 -p 1 -timeout 120s"
