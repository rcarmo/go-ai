#!/usr/bin/env python3
"""Validate an upstream test manifest against an exact expected filename list.

Checks:
- each manifest table row has exactly four Markdown table cells after the row number;
- each row contains exactly one backticked `test/*.test.ts` path;
- row numbers are contiguous from 1;
- manifest path set equals the expected upstream test file set;
- no duplicate paths.
"""
from __future__ import annotations

import pathlib
import re
import sys
from collections import Counter

ROW_RE = re.compile(r"^\|\s*(\d+)\s*\|(.+)\|\s*$")
PATH_RE = re.compile(r"`(test/[^`]+\.test\.ts)`")


def expected_paths(path: pathlib.Path) -> list[str]:
    values = []
    for line in path.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line:
            continue
        p = line.replace("packages/ai/", "")
        if not p.startswith("test/"):
            p = "test/" + p
        values.append(p)
    return sorted(values)


def fail(message: str) -> int:
    print(f"manifest validation failed: {message}", file=sys.stderr)
    return 1


def main(argv: list[str]) -> int:
    if len(argv) != 3:
        print("usage: scripts/validate-test-manifest.py MANIFEST.md expected-test-files.txt", file=sys.stderr)
        return 2
    manifest = pathlib.Path(argv[1])
    expected_file = pathlib.Path(argv[2])
    expected = expected_paths(expected_file)
    expected_set = set(expected)

    rows: list[tuple[int, str]] = []
    for raw in manifest.read_text(encoding="utf-8").splitlines():
        m = ROW_RE.match(raw)
        if not m:
            continue
        row_no = int(m.group(1))
        cells = [cell.strip() for cell in raw.strip().strip("|").split("|")]
        if len(cells) != 4:
            return fail(f"row {row_no} has {len(cells)} cells, want 4: {raw}")
        paths = PATH_RE.findall(raw)
        if len(paths) != 1:
            return fail(f"row {row_no} has {len(paths)} test paths, want 1: {raw}")
        rows.append((row_no, paths[0]))

    if len(rows) != len(expected):
        return fail(f"row count {len(rows)}, want {len(expected)}")
    row_numbers = [n for n, _ in rows]
    want_numbers = list(range(1, len(expected) + 1))
    if row_numbers != want_numbers:
        return fail(f"row numbers are not contiguous 1..{len(expected)}")

    paths = [p for _, p in rows]
    counts = Counter(paths)
    dupes = sorted(p for p, count in counts.items() if count > 1)
    if dupes:
        return fail("duplicate paths: " + ", ".join(dupes[:10]))
    got_set = set(paths)
    missing = sorted(expected_set - got_set)
    extra = sorted(got_set - expected_set)
    if missing or extra:
        if missing:
            print("missing:", *missing, sep="\n  ", file=sys.stderr)
        if extra:
            print("extra:", *extra, sep="\n  ", file=sys.stderr)
        return fail("manifest path set differs from expected")

    changed_marked = sum(1 for line in manifest.read_text(encoding="utf-8").splitlines() if re.search(r"v\d+\.\d+\.\d+ changed", line))
    print(f"manifest rows: {len(rows)}")
    print(f"unique paths: {len(got_set)}")
    print(f"expected paths: {len(expected_set)}")
    print(f"changed-row markers: {changed_marked}")
    print("manifest validation passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
