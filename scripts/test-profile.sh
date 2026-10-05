#!/usr/bin/env bash
# Run each package separately: Go cannot write distinct CPU profiles for ./...
# in one invocation. Analysis is attempted after both passing and failing tests.
set -u
set -o pipefail
GO=${GO:-go}
PROFILE_ROOT=${PROFILE_ROOT:-artifacts/profiles}
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
printf 'Profiles and analysis: %s\n' "$run"
printf '%s\n' "$run" > "$PROFILE_ROOT/latest-run.txt"
printf 'go=%s\n' "$GO" > "$run/invocation.txt"
"$GO" version >> "$run/invocation.txt" 2>&1
printf 'package patterns:' >> "$run/invocation.txt"; printf ' %q' "${patterns[@]}" >> "$run/invocation.txt"
printf '\nflags:' >> "$run/invocation.txt"; printf ' %q' "${flags[@]}" >> "$run/invocation.txt"; printf '\n' >> "$run/invocation.txt"
if ! "$GO" list -f '{{.ImportPath}}|{{if or .TestGoFiles .XTestGoFiles}}tests{{else}}build-only{{end}}' "${patterns[@]}" > "$run/packages.txt" 2> "$run/list.log"; then
  cat "$run/list.log" >&2; exit 1
fi
printf 'package\ttest_status\tanalysis_status\n' > "$run/status.tsv"
if [[ -n ${COVERAGE_FILE:-} ]]; then : > "$run/coverage.out"; fi
failed=0
while IFS='|' read -r package kind; do
  [[ -n $package ]] || continue
  # Full import path avoids collisions between root/provider package names.
  out="$run/$package"
  mkdir -p "$out"
  args=(test "$package" -count=1 -timeout=6m "${flags[@]}" "-cpuprofile=$out/cpu.pprof" "-memprofile=$out/heap.pprof" "-o=$out/test.bin")
  if [[ -n ${COVERAGE_FILE:-} ]]; then args+=("-coverprofile=$out/coverage.out"); fi
  printf '%q ' "$GO" "${args[@]}" > "$out/command.txt"; printf '\n' >> "$out/command.txt"
  "$GO" "${args[@]}" > "$out/test.log" 2>&1
  status=$?
  cat "$out/test.log"
  if [[ $kind == tests && $allow_empty == 0 ]] && grep -q '\[no tests to run\]' "$out/test.log"; then
    echo 'No tests matched; refusing a false verification pass.' >&2
    status=1
  fi
  analysis=0
  if [[ $kind == tests ]]; then
    for metric in alloc_space alloc_objects; do
      if [[ -s $out/heap.pprof && -s $out/test.bin ]]; then
        "$GO" tool pprof -top "-$metric" "$out/test.bin" "$out/heap.pprof" > "$out/$metric.txt" 2>&1 || analysis=1
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
  ((status == 0 && analysis == 0)) || failed=1
done < "$run/packages.txt"
if [[ -n ${COVERAGE_FILE:-} ]]; then
  if [[ ! -s $run/coverage.out ]]; then printf 'mode: set\n' > "$run/coverage.out"; fi
  cp "$run/coverage.out" "$COVERAGE_FILE" || failed=1
fi
printf 'Result: %s; profiles: %s\n' "$failed" "$run"
exit "$failed"
