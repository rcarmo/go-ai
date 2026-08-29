#!/usr/bin/env python3
"""Negative self-tests for scripts/validate-sbom.py."""
from __future__ import annotations
import hashlib
import json
import pathlib
import subprocess
import sys
import tempfile
from copy import deepcopy

ROOT = pathlib.Path(__file__).resolve().parents[1]
VALIDATOR = ROOT / "scripts" / "validate-sbom.py"
REVISION = "abc123def456"
ROOT_REF = "pkg:golang/github.com/rcarmo/go-ai@v0.0.0?type=module"


def write_case(base: pathlib.Path, data: dict, *, checksum: bool = True) -> tuple[pathlib.Path, pathlib.Path]:
    sbom = base / "sbom.cdx.json"
    sha = base / "sbom.cdx.json.sha256"
    raw = json.dumps(data, sort_keys=True, separators=(",", ":")).encode() + b"\n"
    sbom.write_bytes(raw)
    if checksum:
        sha.write_text(hashlib.sha256(raw).hexdigest() + "  sbom.cdx.json\n", encoding="utf-8")
    else:
        sha.write_text("0" * 64 + "  sbom.cdx.json\n", encoding="utf-8")
    return sbom, sha


def valid_doc() -> dict:
    return {
        "bomFormat": "CycloneDX",
        "specVersion": "1.6",
        "metadata": {
            "component": {
                "type": "library",
                "name": "github.com/rcarmo/go-ai",
                "version": REVISION,
                "bom-ref": ROOT_REF,
            }
        },
        "components": [
            {"type": "library", "name": "github.com/aws/aws-sdk-go-v2/config", "version": "v1.32.16", "bom-ref": "pkg:golang/github.com/aws/aws-sdk-go-v2/config@v1.32.16?type=module"},
            {"type": "library", "name": "github.com/aws/aws-sdk-go-v2/service/bedrockruntime", "version": "v1.50.5", "bom-ref": "pkg:golang/github.com/aws/aws-sdk-go-v2/service/bedrockruntime@v1.50.5?type=module"},
            {"type": "library", "name": "github.com/coder/websocket", "version": "v1.8.14", "bom-ref": "pkg:golang/github.com/coder/websocket@v1.8.14?type=module"},
        ],
        "dependencies": [
            {
                "ref": ROOT_REF,
                "dependsOn": [
                    "pkg:golang/github.com/aws/aws-sdk-go-v2/config@v1.32.16?type=module",
                    "pkg:golang/github.com/aws/aws-sdk-go-v2/service/bedrockruntime@v1.50.5?type=module",
                    "pkg:golang/github.com/coder/websocket@v1.8.14?type=module",
                ],
            }
        ],
    }


def run_validator(sbom: pathlib.Path, sha: pathlib.Path, expected: str = REVISION) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [sys.executable, str(VALIDATOR), str(sbom), str(sha), "--expected-revision", expected],
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )


def expect_fail(name: str, data: dict | bytes, mutate_checksum: bool = True, expected: str = REVISION) -> None:
    with tempfile.TemporaryDirectory() as td:
        base = pathlib.Path(td)
        if isinstance(data, bytes):
            sbom = base / "sbom.cdx.json"
            sha = base / "sbom.cdx.json.sha256"
            sbom.write_bytes(data)
            sha.write_text(hashlib.sha256(data).hexdigest() + "  sbom.cdx.json\n", encoding="utf-8")
        else:
            sbom, sha = write_case(base, data, checksum=mutate_checksum)
        result = run_validator(sbom, sha, expected)
        if result.returncode == 0:
            raise AssertionError(f"{name} unexpectedly passed: {result.stdout}")
        print(f"negative self-test passed: {name}")


def main() -> int:
    with tempfile.TemporaryDirectory() as td:
        base = pathlib.Path(td)
        sbom, sha = write_case(base, valid_doc())
        ok = run_validator(sbom, sha)
        if ok.returncode != 0:
            print(ok.stdout, ok.stderr, file=sys.stderr)
            raise AssertionError("valid fixture failed")
        print("positive self-test passed")

    bad_revision = deepcopy(valid_doc())
    bad_revision["metadata"]["component"]["version"] = "deadbeefdead"
    expect_fail("revision tamper with recomputed checksum", bad_revision)

    expect_fail("checksum mismatch", valid_doc(), mutate_checksum=False)

    expect_fail("malformed JSON", b"{not-json}\n")

    path_leak = deepcopy(valid_doc())
    path_leak["metadata"]["tools"] = [{"name": "/workspace/secret-tool"}]
    expect_fail("local path leak", path_leak)

    empty_graph = deepcopy(valid_doc())
    empty_graph["dependencies"] = []
    expect_fail("empty dependency graph", empty_graph)

    missing_root_ref = deepcopy(valid_doc())
    missing_root_ref["dependencies"] = [{"ref": "pkg:golang/example.com/other@v0.0.0", "dependsOn": []}]
    expect_fail("missing root dependency ref", missing_root_ref)

    print("SBOM validator self-tests passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
