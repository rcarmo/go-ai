#!/usr/bin/env python3
"""Validate committed v0.99.1 release inventory files and hashes."""
from __future__ import annotations

import argparse
import hashlib
import json
import pathlib
import shutil
import subprocess
import sys
import tempfile

from project_temp import configure as configure_project_temp
configure_project_temp("validate-v0991-inventory")

ROOT = pathlib.Path(__file__).resolve().parents[1]
FILES = {
    "changed-paths.txt": (169, "086beb5b751f144f9e45034bfbb2b0f4e8a0d3a00f9e17ec1b073b3a8397221e"),
    "changed-tests.txt": (58, "d8eefa94ed87c03de0545965351de4d800cf76ce1db33b0f62fab0f9ab3c7acc"),
    "test-corpus-160.txt": (160, "7ad5f140edc5bc49a348b7b7e36ea266dd82a3075c23ad9997b6e8bc21992b06"),
}
CROSSWALKS = {
    "changed-paths-crosswalk.md": "changed-paths.txt",
    "changed-tests-crosswalk.md": "changed-tests.txt",
    "test-corpus-160-crosswalk.md": "test-corpus-160.txt",
}
FORBIDDEN_CROSSWALK_MARKERS = ("pending", "todo", "needs classification", "tbd")
SCHEMA_VERSION = 6
STRUCTURE_HASH = "58511a57fb2db5e984ee62857d8079aec6ff800e19226c327c118e7f57ea916b"


def sha256(path: pathlib.Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def fail(message: str) -> int:
    print(f"v0.99.1 inventory validation failed: {message}", file=sys.stderr)
    return 1



def parse_crosswalk(path: pathlib.Path) -> list[tuple[str, str, str]]:
    rows: list[tuple[str, str, str]] = []
    for line in path.read_text(encoding="utf-8").splitlines():
        stripped = line.strip()
        if not stripped.startswith("|") or stripped.startswith("|---") or stripped.startswith("| # "):
            continue
        cells = [cell.strip() for cell in stripped.strip("|").split("|")]
        if len(cells) != 4:
            raise ValueError(f"malformed crosswalk row: {line}")
        upstream = cells[1]
        if not (upstream.startswith("`") and upstream.endswith("`")):
            raise ValueError(f"crosswalk upstream cell must be backtick quoted: {line}")
        rows.append((upstream[1:-1].replace("\\|", "|"), cells[2], cells[3]))
    return rows

def validate(root: pathlib.Path = ROOT) -> int:
    docs = root / "docs" / "v0991"
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
    changed_paths = [line for line in (docs / "changed-paths.txt").read_text(encoding="utf-8").splitlines() if line.strip()]
    if len(set(changed_paths)) != len(changed_paths):
        return fail("changed-paths.txt contains duplicates")
    if changed_paths != sorted(changed_paths) or not all(path.startswith("packages/ai/") for path in changed_paths):
        return fail("changed-paths.txt must be sorted packages/ai/... paths")
    changed_tests = [line for line in (docs / "changed-tests.txt").read_text(encoding="utf-8").splitlines() if line.strip()]
    if len(set(changed_tests)) != len(changed_tests):
        return fail("changed-tests.txt contains duplicates")
    if changed_tests != sorted(changed_tests) or not all(path.startswith("packages/ai/test/") and path.endswith(".test.ts") for path in changed_tests):
        return fail("changed-tests.txt must be sorted packages/ai/test/*.test.ts paths")
    corpus_rows = [line for line in (docs / "test-corpus-160.txt").read_text(encoding="utf-8").splitlines() if line.strip()]
    if len(set(corpus_rows)) != len(corpus_rows):
        return fail("test-corpus-160.txt contains duplicates")
    if corpus_rows != sorted(corpus_rows):
        return fail("test-corpus-160.txt must be sorted")
    if not all("/" not in path and path.endswith(".test.ts") for path in corpus_rows):
        return fail("test-corpus-160.txt must contain basenames only")
    source_rows = {
        "changed-paths.txt": changed_paths,
        "changed-tests.txt": changed_tests,
        "test-corpus-160.txt": corpus_rows,
    }
    for crosswalk, source in CROSSWALKS.items():
        path = docs / crosswalk
        if not path.is_file():
            return fail(f"missing {path}")
        try:
            rows = parse_crosswalk(path)
        except ValueError as err:
            return fail(str(err))
        upstream = [row[0] for row in rows]
        if len(set(upstream)) != len(upstream):
            return fail(f"{crosswalk} contains duplicate upstream rows")
        if set(upstream) != set(source_rows[source]):
            missing = sorted(set(source_rows[source]) - set(upstream))[:3]
            extra = sorted(set(upstream) - set(source_rows[source]))[:3]
            return fail(f"{crosswalk} row set does not match {source}; missing={missing} extra={extra}")
        if upstream != source_rows[source]:
            return fail(f"{crosswalk} rows do not exactly match {source} order")
        for upstream_path, disposition, evidence in rows:
            haystack = f"{disposition} {evidence}".lower()
            if any(marker in haystack for marker in FORBIDDEN_CROSSWALK_MARKERS):
                return fail(f"{crosswalk} has unclassified row for {upstream_path}: {disposition} / {evidence}")
            if not disposition or not evidence:
                return fail(f"{crosswalk} has empty disposition/evidence for {upstream_path}")
    manifest_path = docs / "schema-v6-manifest.json"
    if not manifest_path.is_file():
        return fail(f"missing {manifest_path}")
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    if manifest.get("schemaVersion") != SCHEMA_VERSION:
        return fail(f"schemaVersion {manifest.get('schemaVersion')}, want {SCHEMA_VERSION}")
    if manifest.get("structureHash") != STRUCTURE_HASH:
        return fail(f"structureHash {manifest.get('structureHash')}, want {STRUCTURE_HASH}")
    print("v0.99.1 inventory validation passed")
    for name in FILES:
        path = docs / name
        print(f"{name}: {len([l for l in path.read_text(encoding='utf-8').splitlines() if l.strip()])} rows, sha256 {sha256(path)}")
    for crosswalk, source in CROSSWALKS.items():
        print(f"{crosswalk}: {len(parse_crosswalk(docs / crosswalk))} rows, source {source}")
    print(f"schemaVersion: {SCHEMA_VERSION}, structureHash: {STRUCTURE_HASH}")
    return 0


def self_test() -> int:
    with tempfile.TemporaryDirectory(prefix="go-ai-v0991-inventory-test-") as td:
        original = pathlib.Path(td) / "repo"
        shutil.copytree(ROOT, original, ignore=shutil.ignore_patterns(".git", "artifacts"))
        if validate(original) != 0:
            return fail("clean copied inventory did not validate before corruption")
        mutations = [
            ("changed-paths.txt", "packages/ai/extra.ts\n"),
            ("changed-tests.txt", "packages/ai/test/extra.test.ts\n"),
            ("test-corpus-160.txt", "extra.test.ts\n"),
        ]
        for relative, mutation in mutations:
            copy = pathlib.Path(td) / f"repo-{relative}"
            shutil.copytree(original, copy)
            target = copy / "docs" / "v0991" / relative
            target.write_text(target.read_text(encoding="utf-8") + mutation, encoding="utf-8")
            proc = subprocess.run([sys.executable, str(copy / "scripts" / "validate-v0991-inventory.py")], cwd=copy, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            if proc.returncode == 0:
                return fail(f"corrupted {relative} inventory unexpectedly validated")
        copy = pathlib.Path(td) / "repo-crosswalk"
        shutil.copytree(original, copy)
        crosswalk_path = copy / "docs" / "v0991" / "changed-tests-crosswalk.md"
        crosswalk_path.write_text(crosswalk_path.read_text(encoding="utf-8").replace(" | ported | ", " | pending | ", 1), encoding="utf-8")
        proc = subprocess.run([sys.executable, str(copy / "scripts" / "validate-v0991-inventory.py")], cwd=copy, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if proc.returncode == 0:
            return fail("pending crosswalk row unexpectedly validated")
        copy = pathlib.Path(td) / "repo-crosswalk-set"
        shutil.copytree(original, copy)
        crosswalk_path = copy / "docs" / "v0991" / "changed-paths-crosswalk.md"
        lines = crosswalk_path.read_text(encoding="utf-8").splitlines()
        body_indexes = [i for i, line in enumerate(lines) if line.startswith("| ") and not line.startswith("| # ") and not line.startswith("|---")]
        lines.pop(body_indexes[-1])
        crosswalk_path.write_text("\n".join(lines) + "\n", encoding="utf-8")
        proc = subprocess.run([sys.executable, str(copy / "scripts" / "validate-v0991-inventory.py")], cwd=copy, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if proc.returncode == 0:
            return fail("missing crosswalk row unexpectedly validated")
        copy = pathlib.Path(td) / "repo-crosswalk-duplicate"
        shutil.copytree(original, copy)
        crosswalk_path = copy / "docs" / "v0991" / "changed-tests-crosswalk.md"
        lines = crosswalk_path.read_text(encoding="utf-8").splitlines()
        body_indexes = [i for i, line in enumerate(lines) if line.startswith("| ") and not line.startswith("| # ") and not line.startswith("|---")]
        lines[body_indexes[1]] = lines[body_indexes[0]]
        crosswalk_path.write_text("\n".join(lines) + "\n", encoding="utf-8")
        proc = subprocess.run([sys.executable, str(copy / "scripts" / "validate-v0991-inventory.py")], cwd=copy, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if proc.returncode == 0:
            return fail("duplicate crosswalk row unexpectedly validated")
        copy = pathlib.Path(td) / "repo-schema"
        shutil.copytree(original, copy)
        manifest_path = copy / "docs" / "v0991" / "schema-v6-manifest.json"
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
        manifest["structureHash"] = "corrupt"
        manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
        proc = subprocess.run([sys.executable, str(copy / "scripts" / "validate-v0991-inventory.py")], cwd=copy, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if proc.returncode == 0:
            return fail("corrupted schema manifest unexpectedly validated")
    print("v0.99.1 inventory negative self-test passed")
    return 0


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args(argv)
    return self_test() if args.self_test else validate(ROOT)


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
