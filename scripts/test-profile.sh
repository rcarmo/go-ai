#!/usr/bin/env bash
# Run each package separately: Go cannot write distinct CPU profiles for ./...
# in one invocation. Analyse passing/failing runs, save compact conclusions,
# then immediately dispose raw profiles, binaries, logs and scratch.
set -u
set -o pipefail
GO=${GO:-go}
source "$(dirname "${BASH_SOURCE[0]}")/project-env.sh" || exit 2
patterns=()
flags=()
allow_empty=0
while (($#)); do
  if [[ $1 == -- ]]; then shift; flags=("$@"); break; fi
  patterns+=("$1"); shift
done
((${#patterns[@]})) || patterns=(./...)
for flag in "${flags[@]}"; do
  case "$flag" in -bench|-bench=*|-fuzz|-fuzz=*) allow_empty=1 ;; esac
  case "$flag" in
    -cpuprofile*|-memprofile*|-memprofilerate*|-o|-o=*|-outputdir*|-coverprofile*)
      echo "Profile/binary paths are managed by test-profile.sh; use PROFILE_ROOT or COVERAGE_FILE" >&2; exit 2 ;;
  esac
done
mkdir -p "$PROFILE_ROOT" || exit 1
run=$(mktemp -d "$PROFILE_ROOT/run-$(date -u +%Y%m%dT%H%M%SZ)-XXXXXX") || exit 1
run=$(cd "$run" && pwd)
scratch_root="$PROJECT_TMP_ROOT/runs/tests"
project_path_usable "$scratch_root" || exit 2
mkdir -p "$scratch_root"
scratch=$(mktemp -d "$scratch_root/run-$(date -u +%Y%m%dT%H%M%SZ)-XXXXXX") || exit 1
export TMPDIR="$scratch" TMP="$scratch" TEMP="$scratch" GOTMPDIR="$scratch"
# Scratch is disposable; binary/profile/log evidence stays in the separate run.
summary_root="$PROJECT_TMP_ROOT/evidence/analysis"
mkdir -p "$summary_root"
summary="$summary_root/$(basename "$run").txt"
trap 'rm -rf -- "$scratch" "$run"' EXIT
printf 'Profiles and analysis: %s\n' "$run"
printf '%s\n' "$summary" > "$summary_root/latest-summary.txt"
printf 'go=%s\nproject_tmp_root=%s\nscratch=%s\nGOCACHE=%s\nGOMODCACHE=%s\n' "$GO" "$PROJECT_TMP_ROOT" "$scratch" "$GOCACHE" "$GOMODCACHE" > "$run/invocation.txt"
"$GO" version >> "$run/invocation.txt" 2>&1
printf 'package patterns:' >> "$run/invocation.txt"; printf ' %q' "${patterns[@]}" >> "$run/invocation.txt"
printf '\nflags:' >> "$run/invocation.txt"; printf ' %q' "${flags[@]}" >> "$run/invocation.txt"; printf '\n' >> "$run/invocation.txt"
printf 'heap_sampling=Go default runtime.MemProfileRate (512 KiB unless overridden by test)\n' >> "$run/invocation.txt"
git rev-parse HEAD >> "$run/invocation.txt" 2>/dev/null || true
git status --porcelain > "$run/source-status.txt" 2>/dev/null || true
cp "$run/invocation.txt" "$summary"
if ! "$GO" list -f '{{.ImportPath}}|{{if or .TestGoFiles .XTestGoFiles}}tests{{else}}build-only{{end}}' "${patterns[@]}" > "$run/packages.txt" 2> "$run/list.log"; then
  cat "$run/list.log" | tee -a "$summary" >&2
  printf 'Build/list failed; profiles unavailable.\n' >> "$summary"
  exit 1
fi
printf 'package\ttest_status\tanalysis_status\n' > "$run/status.tsv"
if [[ -n ${COVERAGE_FILE:-} ]]; then : > "$run/coverage.out"; fi
failed=0
fuzz_mode=0
for flag in "${flags[@]}"; do case "$flag" in -fuzz|-fuzz=*) fuzz_mode=1;; esac; done
while IFS='|' read -r package kind; do
  [[ -n $package ]] || continue
  # Full import path avoids collisions between root/provider package names.
  out="$run/$package"
  mkdir -p "$out"
  args=(test "$package" -count=1 -timeout=6m "${flags[@]}" "-o=$out/test.bin")
  if ((fuzz_mode == 0)); then args+=("-cpuprofile=$out/cpu.pprof" "-memprofile=$out/heap.pprof"); fi
  if [[ -n ${COVERAGE_FILE:-} ]]; then args+=("-coverprofile=$out/coverage.out"); fi
  printf '%q ' "$GO" "${args[@]}" > "$out/command.txt"; printf '\n' >> "$out/command.txt"
  if ((fuzz_mode)); then
    GO_AI_FUZZ_PROFILE_DIR="$out" "$GO" "${args[@]}" > "$out/test.log" 2>&1
  else
    "$GO" "${args[@]}" > "$out/test.log" 2>&1
  fi
  status=$?
  cat "$out/test.log"
  if [[ $kind == tests && $allow_empty == 0 ]] && grep -q '\[no tests to run\]' "$out/test.log"; then
    echo 'No tests matched; refusing a false verification pass.' >&2
    status=1
  fi
  analysis=0
  if ((fuzz_mode)) && [[ $kind == tests ]]; then
    shopt -s nullglob
    heaps=("$out"/process-*.heap.pprof)
    cpus=("$out"/process-*.cpu.pprof)
    pending=("$out"/process-*.pending)
    if (( ${#heaps[@]} == 0 || ${#cpus[@]} != ${#heaps[@]} || ${#pending[@]} != 0 )); then
      echo 'Fuzz process profiles incomplete: coordinator/worker exited before capture finished' | tee "$out/profile-error.txt"
      analysis=1
    fi
    if (( ${#heaps[@]} > 0 )); then
      "$GO" tool pprof -proto "$out/test.bin" "${heaps[@]}" > "$out/heap.pprof" 2> "$out/heap-merge.log" || analysis=1
    fi
    if (( ${#cpus[@]} > 0 )); then
      "$GO" tool pprof -proto "$out/test.bin" "${cpus[@]}" > "$out/cpu.pprof" 2> "$out/cpu-merge.log" || analysis=1
    fi
    shopt -u nullglob
  fi
  if [[ $kind == tests ]]; then
    for metric in alloc_space alloc_objects; do
      if [[ -s $out/heap.pprof && -s $out/test.bin ]]; then
        "$GO" tool pprof -top -cum "-$metric" "$out/test.bin" "$out/heap.pprof" > "$out/$metric.txt" 2>&1 || analysis=1
      else
        printf 'Profile unavailable (build failure, process crash or interrupted run).\n' > "$out/$metric.txt"
        analysis=1
      fi
    done
    if [[ -s $out/cpu.pprof && -s $out/test.bin ]]; then
      "$GO" tool pprof -top -cum "$out/test.bin" "$out/cpu.pprof" > "$out/cpu.txt" 2>&1 || analysis=1
    else
      printf 'CPU profile unavailable (build failure, process crash or interrupted run).\n' > "$out/cpu.txt"
      analysis=1
    fi
    {
      printf 'Package: %s\nTest exit: %s\nAnalysis exit: %s\n\n' "$package" "$status" "$analysis"
      for report in alloc_space alloc_objects cpu; do
        printf '%s\n' "=== $report ==="; head -22 "$out/$report.txt"; printf '\n'
      done
      printf 'Compare matching workload, toolchain and flags before attributing changes.\n'
      printf 'Review the leading allocation/CPU call chains; record measured reductions or why no safe change applies.\n'
    } > "$out/analysis.txt"
    cat "$out/analysis.txt"
  fi
  if [[ -n ${COVERAGE_FILE:-} && -s $out/coverage.out ]]; then
    if [[ ! -s $run/coverage.out ]]; then head -1 "$out/coverage.out" > "$run/coverage.out"; fi
    if [[ $(head -1 "$run/coverage.out") != $(head -1 "$out/coverage.out") ]]; then
      echo 'Incompatible coverage modes across packages' >&2; analysis=1
    else
      tail -n +2 "$out/coverage.out" >> "$run/coverage.out"
    fi
  fi
  printf '%s\t%s\t%s\n' "$package" "$status" "$analysis" >> "$run/status.tsv"
  {
    printf '\nPackage: %s; test=%s analysis=%s\n' "$package" "$status" "$analysis"
    grep -E '^ok[[:space:]]|^--- FAIL|^FAIL|^#|no tests to run|build failed' "$out/test.log" | head -12 || true
    if [[ $kind == tests ]]; then
      for metric in cpu alloc_space alloc_objects; do
        printf '\n%s\n' "$metric"; head -14 "$out/$metric.txt"
      done
      printf 'Inspect application chains separately from runtime/test overhead; unlike workloads are not a speedup comparison.\n'
    fi
  } >> "$summary"
  rm -rf -- "$out" # Per-package analysis finished: no raw capture retention.
  ((status == 0 && analysis == 0)) || failed=1
done < "$run/packages.txt"
if [[ -n ${COVERAGE_FILE:-} ]]; then
  if [[ ! -s $run/coverage.out ]]; then printf 'mode: set\n' > "$run/coverage.out"; fi
  cp "$run/coverage.out" "$COVERAGE_FILE" || failed=1
fi
printf '\nResult: %s. Raw profiles/binaries/logs disposed after analysis.\n' "$failed" >> "$summary"
printf 'Result: %s; analysis summary: %s (raw profiles removed)\n' "$failed" "$summary"
exit "$failed"
