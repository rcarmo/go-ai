#!/usr/bin/env python3
"""Self-tests for scripts/normalize-sbom.py SBOM identity rewriting."""
from __future__ import annotations

import json
import pathlib
import subprocess
import sys
import tempfile
from copy import deepcopy

ROOT = pathlib.Path(__file__).resolve().parents[1]
NORMALIZER = ROOT / "scripts" / "normalize-sbom.py"
ROOT_MODULE = "github.com/rcarmo/go-ai"
REVISION = "abc123def456"
FULL_VCS_REVISION = "abc123def4567890abc123def4567890abc123de"
OLD_REF = "pkg:golang/github.com/rcarmo/go-ai@old?type=module"


def fixture(*, properties: list[dict[str, str]] | None = None) -> dict:
    component = {
        "type": "library",
        "name": ROOT_MODULE,
        "version": "old",
        "bom-ref": OLD_REF,
        "purl": "pkg:golang/github.com/rcarmo/go-ai@old?goarch=amd64&goos=linux&type=module",
    }
    if properties is not None:
        component["properties"] = properties
    return {
        "bomFormat": "CycloneDX",
        "specVersion": "1.6",
        "serialNumber": "urn:uuid:not-stable",
        "metadata": {"timestamp": "not-stable", "component": component},
        "components": [],
        "dependencies": [{"ref": OLD_REF, "dependsOn": [OLD_REF]}],
    }


def run_normalizer(data: dict, *args: str) -> subprocess.CompletedProcess[str]:
    with tempfile.TemporaryDirectory() as td:
        path = pathlib.Path(td) / "sbom.cdx.json"
        path.write_text(json.dumps(data), encoding="utf-8")
        result = subprocess.run(
            [sys.executable, str(NORMALIZER), str(path), REVISION, *args],
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        if result.returncode == 0:
            result.stdout += "\n__JSON__" + path.read_text(encoding="utf-8")
        return result


def output_doc(result: subprocess.CompletedProcess[str]) -> dict:
    marker = "\n__JSON__"
    if marker not in result.stdout:
        raise AssertionError(f"normalizer did not return JSON marker: stdout={result.stdout!r} stderr={result.stderr!r}")
    return json.loads(result.stdout.split(marker, 1)[1])


def assert_root_revision(data: dict, expected: str) -> None:
    component = data["metadata"]["component"]
    props = component.get("properties")
    values = [prop.get("value") for prop in props if isinstance(prop, dict) and prop.get("name") == "vcs.revision"]
    if values != [expected]:
        raise AssertionError(f"vcs.revision values {values!r}, want {[expected]!r}")
    if component["version"] != REVISION:
        raise AssertionError(f"version {component['version']!r}, want {REVISION!r}")
    if f"{ROOT_MODULE}@{REVISION}" not in component["bom-ref"]:
        raise AssertionError(f"bom-ref not rewritten: {component['bom-ref']!r}")
    if f"{ROOT_MODULE}@{REVISION}" not in component["purl"]:
        raise AssertionError(f"purl not rewritten: {component['purl']!r}")
    deps = data["dependencies"]
    if deps[0]["ref"] != component["bom-ref"] or deps[0]["dependsOn"] != [component["bom-ref"]]:
        raise AssertionError(f"dependency refs not rewritten: {deps!r}")


def expect_fail(name: str, data: dict, *args: str) -> None:
    result = run_normalizer(data, *args)
    if result.returncode == 0:
        raise AssertionError(f"{name} unexpectedly passed: {result.stdout}")
    print(f"normalizer negative self-test passed: {name}")


def main() -> int:
    inserted = run_normalizer(fixture(), "--vcs-revision", FULL_VCS_REVISION)
    if inserted.returncode != 0:
        print(inserted.stdout, inserted.stderr, file=sys.stderr)
        raise AssertionError("normalizer failed to insert vcs.revision")
    assert_root_revision(output_doc(inserted), FULL_VCS_REVISION)
    print("normalizer insertion self-test passed")

    replaced_fixture = fixture(properties=[
        {"name": "vcs.revision", "value": "0" * 40},
        {"name": "vcs.revision", "value": "1" * 40},
        {"name": "other", "value": "kept"},
    ])
    replaced = run_normalizer(deepcopy(replaced_fixture), "--vcs-revision", FULL_VCS_REVISION)
    if replaced.returncode != 0:
        print(replaced.stdout, replaced.stderr, file=sys.stderr)
        raise AssertionError("normalizer failed to replace vcs.revision")
    out = output_doc(replaced)
    assert_root_revision(out, FULL_VCS_REVISION)
    other = [prop for prop in out["metadata"]["component"].get("properties", []) if prop.get("name") == "other"]
    if other != [{"name": "other", "value": "kept"}]:
        raise AssertionError(f"non-vcs property was not preserved: {other!r}")
    print("normalizer replacement self-test passed")

    expect_fail("truncated vcs revision", fixture(), "--vcs-revision", FULL_VCS_REVISION[:12])
    expect_fail("malformed vcs revision", fixture(), "--vcs-revision", "z" * 40)
    print("SBOM normalizer self-tests passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
