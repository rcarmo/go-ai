#!/usr/bin/env python3
"""Validate committed v0.87.0 release inventory files and hashes."""
from __future__ import annotations

import argparse
import hashlib
import pathlib
import shutil
import subprocess
import sys
import tempfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
FILES = {
    "changed-paths.txt": (127, "e6bd9733d8fff626838d386df8e6bb543d40d77af74340ee4f2f271b411b9828"),
    "changed-tests.txt": (82, "a12a1453c8fbabfd6902ced06304cbd2fa9f7d82ce89b91a1403366bc11fe5b2"),
    "test-corpus-150.txt": (150, "6e8f9cb169afe493113170bb9f451cd4d16d8a5735a18b22880146bda6a27246"),
}
TEST_CORPUS_BASENAME_HASH = "042cdfbc8cc089da71409e615fb54cfe7273a960f8e0d10e63e07584ae9f2e75"
CHANGED_TEST_BASENAME_HASH = "cb66d8f12cc4e23509e41e07c5bdddf546d961e731751254f01fbf0989676040"


def sha256(path: pathlib.Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def sha256_text(text: str) -> str:
    return hashlib.sha256(text.encode("utf-8")).hexdigest()


def fail(message: str) -> int:
    print(f"v0.87.0 inventory validation failed: {message}", file=sys.stderr)
    return 1


def validate(root: pathlib.Path = ROOT) -> int:
    docs = root / "docs" / "v0870"
    for name, (want_count, want_hash) in FILES.items():
        path = docs / name
        if not path.is_file():
            return fail(f"missing {path}")
        rows = [line for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]
        got_hash = sha256(path)
        if len(rows) != want_count:
            return fail(f"{name} row count {len(rows)}, want {want_count}")
        if got_hash != want_hash:
            return fail(f"{name} sha256 {got_hash}, want {want_hash}")
    corpus_rows = [line for line in (docs / "test-corpus-150.txt").read_text(encoding="utf-8").splitlines() if line.strip()]
    if len(set(corpus_rows)) != len(corpus_rows):
        return fail("test-corpus-150.txt contains duplicates")
    if not all(path.startswith("packages/ai/test/") and path.endswith(".test.ts") for path in corpus_rows):
        return fail("test-corpus-150.txt contains non-test paths")
    basename_hash = sha256_text("\n".join(sorted(pathlib.PurePosixPath(row).name for row in corpus_rows)) + "\n")
    if basename_hash != TEST_CORPUS_BASENAME_HASH:
        return fail(f"test corpus basename hash {basename_hash}, want {TEST_CORPUS_BASENAME_HASH}")
    changed_rows = [line for line in (docs / "changed-tests.txt").read_text(encoding="utf-8").splitlines() if line.strip()]
    changed_basename_hash = sha256_text("\n".join(sorted(pathlib.PurePosixPath(row).name for row in changed_rows)) + "\n")
    if changed_basename_hash != CHANGED_TEST_BASENAME_HASH:
        return fail(f"changed-test basename hash {changed_basename_hash}, want {CHANGED_TEST_BASENAME_HASH}")
    print("v0.87.0 inventory validation passed")
    for name in FILES:
        path = docs / name
        print(f"{name}: {len([l for l in path.read_text(encoding='utf-8').splitlines() if l.strip()])} rows, sha256 {sha256(path)}")
    return 0


def self_test() -> int:
    with tempfile.TemporaryDirectory(prefix="go-ai-v0870-inventory-test-") as td:
        tmp = pathlib.Path(td) / "repo"
        shutil.copytree(ROOT, tmp, ignore=shutil.ignore_patterns(".git", "artifacts"))
        if validate(tmp) != 0:
            return fail("clean copied inventory did not validate before corruption")
        target = tmp / "docs" / "v0870" / "changed-paths.txt"
        target.write_text(target.read_text(encoding="utf-8") + "\npackages/ai/extra.ts\n", encoding="utf-8")
        proc = subprocess.run([sys.executable, str(tmp / "scripts" / "validate-v0870-inventory.py")], cwd=tmp, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if proc.returncode == 0:
            return fail("corrupted changed-paths inventory unexpectedly validated")
        shutil.rmtree(tmp)
        shutil.copytree(ROOT, tmp, ignore=shutil.ignore_patterns(".git", "artifacts"))
        target = tmp / "docs" / "v0870" / "test-corpus-150.txt"
        data = target.read_text(encoding="utf-8")
        target.write_text(data.replace("packages/ai/test/abort.test.ts", "packages/ai/test/abort-corrupt.test.ts", 1), encoding="utf-8")
        proc = subprocess.run([sys.executable, str(tmp / "scripts" / "validate-v0870-inventory.py")], cwd=tmp, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if proc.returncode == 0:
            return fail("corrupted test corpus unexpectedly validated")
    print("v0.87.0 inventory negative self-test passed")
    return 0


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args(argv)
    return self_test() if args.self_test else validate(ROOT)


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
