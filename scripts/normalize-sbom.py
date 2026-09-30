#!/usr/bin/env python3
"""Normalize CycloneDX SBOM JSON for stable artifact hashing."""
from __future__ import annotations
import argparse
import json
import pathlib
import re
import sys

ROOT_MODULE = "github.com/rcarmo/go-ai"
VCS_REVISION_PROPERTY = "vcs.revision"
FULL_REVISION_RE = re.compile(r"^[0-9a-f]{40}$")


def replace_purl_version(value: str, version: str) -> str:
    if ROOT_MODULE not in value:
        return value
    base, sep, qualifiers = value.partition("?")
    if "@" in base:
        prefix = base.rsplit("@", 1)[0]
    else:
        prefix = base
    out = f"{prefix}@{version}"
    if sep:
        out += sep + qualifiers
    return out


def rewrite_dependency_refs(dependencies: object, old_root_ref: str | None, new_root_ref: str) -> None:
    if not isinstance(dependencies, list):
        return
    for dep in dependencies:
        if not isinstance(dep, dict):
            continue
        if dep.get("ref") == old_root_ref or (isinstance(dep.get("ref"), str) and ROOT_MODULE in dep["ref"]):
            dep["ref"] = new_root_ref
        depends_on = dep.get("dependsOn")
        if isinstance(depends_on, list):
            for index, ref in enumerate(depends_on):
                if ref == old_root_ref or (isinstance(ref, str) and ROOT_MODULE in ref):
                    depends_on[index] = new_root_ref


def set_component_property(component: dict, name: str, value: str) -> None:
    props = component.get("properties")
    if not isinstance(props, list):
        props = []
    props = [prop for prop in props if not (isinstance(prop, dict) and prop.get("name") == name)]
    props.append({"name": name, "value": value})
    component["properties"] = props


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("sbom", type=pathlib.Path)
    parser.add_argument("version", help="root component version for SBOM identity")
    parser.add_argument("--vcs-revision", required=True, help="full 40-character lowercase Git revision to embed as root component property")
    return parser.parse_args(argv[1:])


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    path = args.sbom
    version = args.version
    if args.vcs_revision and not FULL_REVISION_RE.fullmatch(args.vcs_revision):
        print(f"vcs revision must be a full lowercase 40-character Git SHA: {args.vcs_revision!r}", file=sys.stderr)
        return 1
    data = json.loads(path.read_text(encoding="utf-8"))
    data.pop("serialNumber", None)
    metadata = data.get("metadata") or {}
    metadata.pop("timestamp", None)
    component = metadata.get("component")
    if isinstance(component, dict):
        old_root_ref = component.get("bom-ref") if isinstance(component.get("bom-ref"), str) else None
        component["version"] = version
        if isinstance(component.get("bom-ref"), str):
            component["bom-ref"] = replace_purl_version(component["bom-ref"], version)
        if isinstance(component.get("purl"), str):
            component["purl"] = replace_purl_version(component["purl"], version)
        if args.vcs_revision:
            set_component_property(component, VCS_REVISION_PROPERTY, args.vcs_revision)
        new_root_ref = component.get("bom-ref")
        if isinstance(new_root_ref, str):
            rewrite_dependency_refs(data.get("dependencies"), old_root_ref, new_root_ref)
    data["metadata"] = metadata
    path.write_text(json.dumps(data, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
