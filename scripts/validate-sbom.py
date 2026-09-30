#!/usr/bin/env python3
"""Validate the generated CycloneDX SBOM has required Go module content."""
from __future__ import annotations
import argparse
import hashlib
import json
import pathlib
import re
import sys
from typing import Any

ROOT_MODULE = "github.com/rcarmo/go-ai"
VCS_REVISION_PROPERTY = "vcs.revision"
FULL_REVISION_RE = re.compile(r"^[0-9a-f]{40}$")
REQUIRED_DEPENDENCIES = {
    "github.com/aws/aws-sdk-go-v2/config",
    "github.com/aws/aws-sdk-go-v2/service/bedrockruntime",
    "github.com/coder/websocket",
}


def fail(msg: str) -> int:
    print(f"sbom validation failed: {msg}", file=sys.stderr)
    return 1


def component_name(component: dict[str, Any]) -> str | None:
    name = component.get("name")
    return name if isinstance(name, str) else None


def purl_version(value: str, root_module: str = ROOT_MODULE) -> str | None:
    if root_module not in value:
        return None
    base = value.split("?", 1)[0]
    if "@" not in base:
        return None
    return base.rsplit("@", 1)[1]


def root_ref_matches_version(ref: str, expected_version: str) -> bool:
    return ROOT_MODULE in ref and purl_version(ref) == expected_version


def component_property(component: dict[str, Any], name: str) -> str | None:
    props = component.get("properties")
    if not isinstance(props, list):
        return None
    values = [prop.get("value") for prop in props if isinstance(prop, dict) and prop.get("name") == name]
    if len(values) != 1 or not isinstance(values[0], str):
        return None
    return values[0]


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("sbom", type=pathlib.Path)
    parser.add_argument("checksum", type=pathlib.Path)
    parser.add_argument("--expected-revision", dest="expected_version", required=True, help="expected root component version (legacy name retained for callers)")
    parser.add_argument("--expected-vcs-revision", required=True, help="expected full lowercase 40-character VCS revision stored as root component property")
    args = parser.parse_args(argv[1:])

    sbom_path = args.sbom
    sha_path = args.checksum
    if not sbom_path.is_file():
        return fail(f"missing SBOM {sbom_path}")
    if not sha_path.is_file():
        return fail(f"missing checksum {sha_path}")
    raw = sbom_path.read_bytes()
    if b"/workspace" in raw or b"/home/" in raw:
        return fail("SBOM contains local absolute paths")
    got_sha = hashlib.sha256(raw).hexdigest()
    recorded = sha_path.read_text(encoding="utf-8").strip().split()[0]
    if got_sha != recorded:
        return fail(f"checksum mismatch: got {got_sha}, recorded {recorded}")
    try:
        data = json.loads(raw)
    except json.JSONDecodeError as exc:
        return fail(f"invalid JSON: {exc}")
    if data.get("bomFormat") != "CycloneDX":
        return fail("bomFormat is not CycloneDX")
    if not str(data.get("specVersion", "")).startswith("1."):
        return fail("missing CycloneDX specVersion")
    metadata = data.get("metadata") or {}
    component = metadata.get("component") or {}
    if not isinstance(component, dict):
        return fail("missing metadata component")
    if component_name(component) != ROOT_MODULE:
        return fail(f"unexpected root component name {component.get('name')!r}")
    if component.get("version") != args.expected_version:
        return fail(f"unexpected root component version {component.get('version')!r}, want {args.expected_version!r}")
    if not FULL_REVISION_RE.fullmatch(args.expected_vcs_revision):
        return fail(f"expected VCS revision must be a full lowercase 40-character Git SHA, got {args.expected_vcs_revision!r}")
    got_revision = component_property(component, VCS_REVISION_PROPERTY)
    if got_revision is None:
        return fail(f"missing unique root {VCS_REVISION_PROPERTY} property")
    if not FULL_REVISION_RE.fullmatch(got_revision):
        return fail(f"root {VCS_REVISION_PROPERTY} is not a full lowercase 40-character Git SHA: {got_revision!r}")
    if got_revision != args.expected_vcs_revision:
        return fail(f"unexpected root VCS revision {got_revision!r}, want {args.expected_vcs_revision!r}")
    if component.get("type") not in {"application", "library"}:
        return fail(f"unexpected root component type {component.get('type')!r}")
    root_ref = component.get("bom-ref")
    if not isinstance(root_ref, str) or not root_ref_matches_version(root_ref, args.expected_version):
        return fail(f"unexpected root component bom-ref {root_ref!r}; want {ROOT_MODULE}@{args.expected_version}")
    root_purl = component.get("purl")
    if not isinstance(root_purl, str) or not root_ref_matches_version(root_purl, args.expected_version):
        return fail(f"unexpected root component purl {root_purl!r}; want {ROOT_MODULE}@{args.expected_version}")
    components = data.get("components")
    if not isinstance(components, list) or not components:
        return fail("empty components list")
    names = {component_name(c) for c in components if isinstance(c, dict)}
    missing = sorted(REQUIRED_DEPENDENCIES - names)
    if missing:
        return fail("missing expected resolved dependencies: " + ", ".join(missing))
    deps = data.get("dependencies")
    if not isinstance(deps, list) or not deps:
        return fail("empty dependency graph")
    root_deps = [dep for dep in deps if isinstance(dep, dict) and isinstance(dep.get("ref"), str) and ROOT_MODULE in dep["ref"]]
    stale_root_deps = [dep.get("ref") for dep in root_deps if dep.get("ref") != root_ref]
    if stale_root_deps:
        return fail("stale root dependency refs: " + ", ".join(stale_root_deps))
    root_dep = next((dep for dep in root_deps if dep.get("ref") == root_ref), None)
    if root_dep is None:
        return fail(f"missing root dependency graph ref {root_ref!r}")
    depends_on = root_dep.get("dependsOn")
    if not isinstance(depends_on, list) or not depends_on:
        return fail("root dependency graph has empty dependsOn")
    for ref in depends_on:
        if isinstance(ref, str) and ROOT_MODULE in ref and ref != root_ref:
            return fail(f"root dependency graph contains stale root ref {ref!r}")
    for required in sorted(REQUIRED_DEPENDENCIES):
        if not any(isinstance(ref, str) and required in ref for ref in depends_on):
            return fail(f"root dependency graph missing dependency ref for {required}")
    suffix = f", vcs {args.expected_vcs_revision}" if args.expected_vcs_revision else ""
    print(f"SBOM valid: {len(components)} components, sha256 {got_sha}, version {args.expected_version}{suffix}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
