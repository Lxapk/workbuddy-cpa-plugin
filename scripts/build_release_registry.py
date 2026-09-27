#!/usr/bin/env python3
"""Build registry.json for a GitHub-release plugin store.

The plugin store validates the manifest before it will list or install anything,
and the github-release form requires ``release_tag``: internal/pluginstore
Validate() returns "missing required field release-tag" without it, and then the
plugin is invisible in the store. It also cross-checks that the tag resolves to
the declared version, so both fields are emitted from the same input.

Why github-release rather than direct: a direct plan pins a sha256 for each
artifact, so the published registry is only valid for the exact bytes CI happened
to produce. Re-running the build for the same tag yields a different archive (Go
embeds build metadata), and the store then rejects the download with "artifact
checksum mismatch" — the plugin silently fails to install while the registry still
looks correct. Handing the tag to the store instead lets it resolve the release
itself, which stays correct across rebuilds.

Usage:
  python3 build_release_registry.py \
      --id workbuddy --name WorkBuddy \
      --version 0.13.38 \
      --repo https://github.com/Lxapk/workbuddy-cpa-plugin \
      --out registry.json
"""

import argparse
import json

SCHEMA_VERSION_V2 = 2

DESCRIPTION = (
    "把 WorkBuddy 账号反代为 CPA 的 OpenAI 兼容接口："
    "账号轮换、模型输出、签到与积分管理，"
    "并支持国内版成长任务、猫猫旅行与打卡福利的自动完成。"
)


def build_plugin(args: argparse.Namespace) -> dict:
    # The store normalises a leading "v" before comparing, so both forms work;
    # keep the tag exactly as it appears on the release for clarity.
    release_tag = args.tag or f"v{args.version}"
    plugin = {
        "id": args.id,
        "name": args.name,
        "description": DESCRIPTION,
        "author": args.author,
        "version": args.version,
        "release_tag": release_tag,
        "repository": args.repo,
        "license": "MIT",
        "install": {"type": "github-release"},
    }
    if args.logo:
        plugin["logo"] = args.logo
    if args.homepage:
        plugin["homepage"] = args.homepage
    return plugin


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--id", required=True)
    parser.add_argument("--name", required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--repo", required=True)
    parser.add_argument("--tag", default="")
    parser.add_argument("--author", default="BlackHawk")
    parser.add_argument("--logo", default="")
    parser.add_argument("--homepage", default="")
    parser.add_argument("--out", required=True)
    args = parser.parse_args()

    registry = {
        "schema_version": SCHEMA_VERSION_V2,
        "plugins": [build_plugin(args)],
    }
    with open(args.out, "w", encoding="utf-8") as handle:
        json.dump(registry, handle, ensure_ascii=False, indent=2)
        handle.write("\n")
    print(f"wrote {args.out} (version={args.version}, release_tag={registry['plugins'][0]['release_tag']})")


if __name__ == "__main__":
    main()
