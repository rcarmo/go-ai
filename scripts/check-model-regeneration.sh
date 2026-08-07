#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

source_models_ts="${PI_AI_MODELS_GENERATED_TS:-/workspace/tmp/pi-v0841/packages/ai/src/models.generated.ts}"
provider_data_dir="${PI_AI_MODEL_DATA_DIR:-/workspace/tmp/pi-ai-0.84.1-package/package/dist/providers/data}"

go_cmd="${GO:-go}"
tmp_root="${TMPDIR:-${GO_TMPDIR:-/tmp}}"
workdir="$(mktemp -d "${tmp_root%/}/go-ai-model-regen.XXXXXX")"
cleanup() {
  rm -rf "$workdir"
}
trap cleanup EXIT

generated="$workdir/models_generated.go"
want_norm="$workdir/want.go"
got_norm="$workdir/got.go"

if [[ ! -f "$source_models_ts" ]]; then
  echo "model regeneration source not found: $source_models_ts" >&2
  echo "Set PI_AI_MODELS_GENERATED_TS to the exact upstream src/models.generated.ts" >&2
  exit 1
fi
if [[ ! -d "$provider_data_dir" ]]; then
  echo "model provider data dir not found: $provider_data_dir" >&2
  echo "Set PI_AI_MODEL_DATA_DIR to the exact published provider data directory" >&2
  exit 1
fi

(
  cd "$repo_root"
  PI_AI_MODEL_DATA_DIR="$provider_data_dir" "$go_cmd" run ./scripts/generate-models.go -input "$source_models_ts" -output "$generated" >/dev/null
)
"$go_cmd" fmt "$generated" >/dev/null

normalize() {
  sed -E 's#^// Generated: .*#// Generated: <normalized>#' "$1"
}

normalize "$repo_root/models_generated.go" > "$want_norm"
normalize "$generated" > "$got_norm"

diff -u "$want_norm" "$got_norm" >/dev/null || {
  echo "models_generated.go does not match normalized regeneration from exact upstream artifacts" >&2
  echo "source: $source_models_ts" >&2
  echo "provider data: $provider_data_dir" >&2
  diff -u "$want_norm" "$got_norm" >&2 || true
  exit 1
}

echo "model regeneration metadata comparator passed"
