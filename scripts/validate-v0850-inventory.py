#!/usr/bin/env python3
"""Validate committed v0.85.0 release inventory files and hashes.

This is intentionally self-contained for clean clones/CI: it validates the
committed canonical files under docs/v0850/ and can run a negative corruption
self-test without access to /workspace/tmp upstream checkouts.
"""
from __future__ import annotations

import argparse
import hashlib
import pathlib
import shutil
import subprocess
import sys
import tempfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
DOCS = ROOT / "docs" / "v0850"
FILES = {
    "changed-paths.txt": (51, "db461a56838926cf60d4ae0196ed98fcc215616dacff013ad8c235bb8ad9b83f"),
    "changed-tests.txt": (29, "0b58c13688745fd74837bcefb868d2f5064649dcb4c57a5e134e08be0fd9d711"),
    "test-corpus-142.txt": (142, "56f8742065a4ad01d73e5aee53035324f2e7333a735222ab15db870819e29065"),
}


def sha256(path: pathlib.Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def fail(message: str) -> int:
    print(f"v0.85.0 inventory validation failed: {message}", file=sys.stderr)
    return 1


def validate(root: pathlib.Path = ROOT) -> int:
    docs = root / "docs" / "v0850"
    for name, (want_count, want_hash) in FILES.items():
        path = docs / name
        if not path.is_file():
            return fail(f"missing {path}")
        raw = path.read_text(encoding="utf-8")
        rows = [line for line in raw.splitlines() if line.strip()]
        got_count = len(rows)
        got_hash = sha256(path)
        if got_count != want_count:
            return fail(f"{name} row count {got_count}, want {want_count}")
        if got_hash != want_hash:
            return fail(f"{name} sha256 {got_hash}, want {want_hash}")
    paths = (docs / "changed-paths.txt").read_text(encoding="utf-8").splitlines()
    if len(set(paths)) != len(paths):
        return fail("changed-paths.txt contains duplicates")
    if any("packages/ai/test/" in row for row in paths[:0]):
        return fail("unreachable guard")
    corpus_paths = [line.replace("packages/ai/", "") for line in (docs / "test-corpus-142.txt").read_text(encoding="utf-8").splitlines() if line.strip()]
    if len(set(corpus_paths)) != len(corpus_paths):
        return fail("test-corpus-142.txt contains duplicates")
    if not all(path.startswith("test/") and path.endswith(".test.ts") for path in corpus_paths):
        return fail("test-corpus-142.txt contains non-test paths")
    print("v0.85.0 inventory validation passed")
    for name in FILES:
        path = docs / name
        print(f"{name}: {len([l for l in path.read_text(encoding='utf-8').splitlines() if l.strip()])} rows, sha256 {sha256(path)}")
    return 0


def self_test() -> int:
    with tempfile.TemporaryDirectory(prefix="go-ai-v0850-inventory-test-") as td:
        tmp = pathlib.Path(td) / "repo"
        shutil.copytree(ROOT, tmp, ignore=shutil.ignore_patterns(".git", "artifacts"))
        if validate(tmp) != 0:
            return fail("clean copied inventory did not validate before corruption")
        target = tmp / "docs" / "v0850" / "changed-paths.txt"
        target.write_text(target.read_text(encoding="utf-8") + "\nM\tpackages/ai/extra.ts\n", encoding="utf-8")
        proc = subprocess.run([sys.executable, str(tmp / "scripts" / "validate-v0850-inventory.py")], cwd=tmp, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if proc.returncode == 0:
            return fail("corrupted changed-paths inventory unexpectedly validated")
        target = tmp / "docs" / "v0850" / "test-corpus-142.txt"
        data = target.read_text(encoding="utf-8")
        target.write_text(data.replace("packages/ai/test/abort.test.ts", "packages/ai/test/abort-corrupt.test.ts", 1), encoding="utf-8")
        proc = subprocess.run([sys.executable, str(tmp / "scripts" / "validate-v0850-inventory.py")], cwd=tmp, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if proc.returncode == 0:
            return fail("corrupted test corpus inventory unexpectedly validated")
    print("v0.85.0 inventory negative self-test passed")
    return 0


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args(argv)
    if args.self_test:
        return self_test()
    return validate(ROOT)


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
