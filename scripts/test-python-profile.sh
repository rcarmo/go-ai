#!/usr/bin/env bash
# Profile every Python helper and its Python/Go-generator children.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/project-env.sh"
mkdir -p "$PROJECT_TMP_ROOT/evidence/helpers" "$PROJECT_TMP_ROOT/runs/helpers"
run=$(mktemp -d "$PROJECT_TMP_ROOT/evidence/helpers/run-XXXXXX")
scratch=$(mktemp -d "$PROJECT_TMP_ROOT/runs/helpers/run-XXXXXX")
mkdir -p "$PROJECT_TMP_ROOT/evidence/analysis"
summary="$PROJECT_TMP_ROOT/evidence/analysis/helpers-$(basename "$run").txt"
trap 'rm -rf -- "$scratch" "$run"' EXIT
export GO_AI_HELPER_PROFILE_ROOT="$run" TMPDIR="$scratch" TMP="$scratch" TEMP="$scratch" GOTMPDIR="$scratch"
GO=${GO:-go}
printf 'go=%s\nroot=%s\nscratch=%s\ncommand=' "$GO" "$PROJECT_TMP_ROOT" "$scratch" > "$run/invocation.txt"
printf '%q ' "$@" >> "$run/invocation.txt"
printf '\n' >> "$run/invocation.txt"
"$GO" version >> "$run/invocation.txt"
set +e
python3 "$(dirname "${BASH_SOURCE[0]}")/profile-python.py" "$@" > "$run/test.log" 2>&1
status=$?
set -e
cat "$run/test.log"
cp "$run/invocation.txt" "$summary"
grep -E 'passed|failed|Python analysis:|Traceback|Error' "$run/test.log" | head -25 >> "$summary" || true
analysis=0
shopt -s nullglob
for dir in "$run"/generator-*/; do
  [[ -s "$dir/cpu.pprof" && -s "$dir/heap.pprof" ]] || { analysis=1; continue; }
  for metric in alloc_space alloc_objects; do
    "$GO" tool pprof -top -cum "-$metric" "$dir/heap.pprof" > "$dir/$metric.txt" 2>&1 || analysis=1
  done
  "$GO" tool pprof -top -cum "$dir/cpu.pprof" > "$dir/cpu.txt" 2>&1 || analysis=1
  head -18 "$dir/alloc_space.txt"
  for metric in cpu alloc_space alloc_objects; do printf '\n%s\n' "$metric" >> "$summary"; head -14 "$dir/$metric.txt" >> "$summary"; done
  rm -rf -- "$dir"
done
printf '\ntest=%s\nanalysis=%s\nRaw helper captures/binaries/logs disposed.\n' "$status" "$analysis" >> "$summary"
echo "Helper analysis: $summary"
((status == 0 && analysis == 0))
