#!/usr/bin/env python3
"""Validate registry.json before committing or publishing.

Written after two store-side failures that a local check would have caught:

  * a placeholder sha256 ("PENDING") was pushed, and the store rejected the whole
    registry with "invalid sha256 length" — worse than a stale value, which only
    fails the checksum of one artifact;
  * a github-release manifest was published without release_tag, and the store
    refused it with "missing required field release-tag", leaving the plugin
    invisible with no visible error on the plugin page.

The checks mirror internal/pluginstore Validate() so the failure surfaces here
instead of in the store UI.

Checks:
  * schema_version and a non-empty plugins array
  * every plugin has a valid, non-empty version
  * install.type is one of direct / github-release
  * github-release: release_tag is present and resolves to the declared version
  * direct: every artifact has a 64-character lowercase hex sha256, and its URL
    mentions the declared version
"""
import json
import re
import sys

HEX64 = re.compile(r"^[0-9a-f]{64}$")
PLUGIN_VERSION = re.compile(r"^\d+\.\d+\.\d+$")


def normalize_version(value: str) -> str:
    return str(value or "").strip().lstrip("vV")


def check_github_release(where: str, plugin: dict, version: str) -> list[str]:
    problems: list[str] = []
    release_tag = str(plugin.get("release_tag") or "").strip()
    if not release_tag:
        # Without this the store returns "missing required field release-tag" and
        # the plugin never appears in the store.
        problems.append(f"{where}: release_tag is required for github-release installs")
        return problems
    tag_version = normalize_version(release_tag)
    if not PLUGIN_VERSION.match(tag_version):
        problems.append(f"{where}: release_tag {release_tag!r} does not resolve to a version")
    elif tag_version != normalize_version(version):
        problems.append(
            f"{where}: release_tag {release_tag!r} resolves version {tag_version!r}, "
            f"want {normalize_version(version)!r}"
        )
    return problems


def check_direct(where: str, plugin: dict, version: str) -> list[str]:
    problems: list[str] = []
    artifacts = (plugin.get("install") or {}).get("artifacts") or []
    if not artifacts:
        problems.append(f"{where}: direct install has no artifacts")
    for a_index, artifact in enumerate(artifacts):
        spot = f"{where}: artifacts[{a_index}]"
        digest = str(artifact.get("sha256") or "")
        if not HEX64.match(digest):
            if digest.isalpha() and digest.isupper():
                problems.append(f"{spot}: sha256 is a placeholder ({digest!r}); "
                                f"publish only after the CI artifact exists")
            else:
                problems.append(f"{spot}: invalid sha256 length ({len(digest)}), want 64 hex")
        url = str(artifact.get("url") or "")
        if version and version not in url:
            problems.append(f"{spot}: url does not mention version {version}")
    return problems


def main(path: str = "registry.json") -> int:
    with open(path, encoding="utf-8") as handle:
        doc = json.load(handle)

    problems: list[str] = []
    if not doc.get("schema_version"):
        problems.append("schema_version is missing")

    plugins = doc.get("plugins") or []
    if not plugins:
        problems.append("plugins array is empty")

    for index, plugin in enumerate(plugins):
        where = f"plugins[{index}]"
        if not str(plugin.get("id") or "").strip():
            problems.append(f"{where}: id is missing")
        version = str(plugin.get("version") or "").strip()
        if not version:
            problems.append(f"{where}: version is missing")
            continue
        if not PLUGIN_VERSION.match(normalize_version(version)):
            problems.append(f"{where}: invalid version {version!r}, want x.y.z")

        install_type = str((plugin.get("install") or {}).get("type") or "github-release").strip().lower()
        if install_type == "github-release":
            problems.extend(check_github_release(where, plugin, version))
        elif install_type == "direct":
            problems.extend(check_direct(where, plugin, version))
        else:
            problems.append(f"{where}: unsupported install type {install_type!r}")

    if problems:
        print("registry.json is not publishable:", file=sys.stderr)
        for problem in problems:
            print(f"  - {problem}", file=sys.stderr)
        return 1

    print("registry.json OK")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(*sys.argv[1:]))
