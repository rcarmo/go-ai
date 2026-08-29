#!/usr/bin/env python3
"""Verify docs/scripts do not reference moved root black-box tests as root files."""
from __future__ import annotations
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
SEARCH_ROOTS = ["docs", "scripts", "README.md", "AGENTS.md", "RELEASE.md", "Makefile", ".github"]
ALLOW = {
    "root-goai-test-files-before.txt",  # external audit scratch name if copied into docs accidentally; not a repo file.
}


def moved_test_names() -> list[str]:
    tests_dir = ROOT / "tests"
    return sorted(p.name for p in tests_dir.glob("*_test.go") if p.is_file())


def iter_files() -> list[pathlib.Path]:
    out: list[pathlib.Path] = []
    for item in SEARCH_ROOTS:
        path = ROOT / item
        if path.is_dir():
            out.extend(p for p in path.rglob("*") if p.is_file())
        elif path.is_file():
            out.append(path)
    return sorted(out)


def main() -> int:
    moved = moved_test_names()
    if not moved:
        print("no moved tests found under tests/", file=sys.stderr)
        return 1
    pattern = re.compile(r"(?<![A-Za-z0-9_./-])(" + "|".join(re.escape(name) for name in moved) + r")\b")
    failures: list[str] = []
    for path in iter_files():
        rel = path.relative_to(ROOT)
        if rel.name in ALLOW:
            continue
        text = path.read_text(encoding="utf-8", errors="ignore")
        for line_no, line in enumerate(text.splitlines(), 1):
            for match in pattern.finditer(line):
                prefix = line[max(0, match.start() - 32):match.start()]
                if prefix.endswith("tests/") or prefix.endswith("inference/provider/anthropic/") or prefix.endswith("inference/provider/openairesponses/"):
                    continue
                failures.append(f"{rel}:{line_no}: stale moved-test reference {match.group(1)!r}")
        if re.search(r"go test\s+\.\s+-run", text):
            for line_no, line in enumerate(text.splitlines(), 1):
                if re.search(r"go test\s+\.\s+-run", line):
                    failures.append(f"{rel}:{line_no}: stale focused command should target ./tests or ./...")
    if failures:
        print("moved-test reference verification failed:", file=sys.stderr)
        for failure in failures:
            print(failure, file=sys.stderr)
        return 1
    print(f"moved-test reference verification passed for {len(moved)} moved tests")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
