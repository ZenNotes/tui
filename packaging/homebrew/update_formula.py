#!/usr/bin/env python3
"""Pin the Homebrew formula to a published release's GitHub asset digests."""

import argparse
import json
from pathlib import Path
import re
import subprocess


def render_formula(formula, version, release):
    if release.get("tag_name") != f"v{version}" or release.get("draft") or release.get("prerelease"):
        raise ValueError(f"v{version} must be a published, stable release")

    for target in ("darwin_arm64", "darwin_amd64", "linux_arm64", "linux_amd64"):
        name = f"zn_{version}_{target}.tar.gz"
        assets = [asset for asset in release.get("assets", []) if asset["name"] == name]
        digest = assets[0].get("digest") if len(assets) == 1 else None
        if not isinstance(digest, str) or not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
            raise ValueError(f"missing asset or SHA-256 digest for {name}; wait for all release uploads")
        url = f"https://github.com/ZenNotes/tui/releases/download/v{version}/{name}"
        pattern = rf'url "[^"\n]+_{target}\.tar\.gz"(\n\s+sha256 ")[0-9a-f]{{64}}(")'
        formula, count = re.subn(pattern, lambda m: f'url "{url}"' + m[1] + digest[7:] + m[2], formula)
        if count != 1:
            raise ValueError(f"expected exactly one URL/checksum for {target} in the formula")
    return formula


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", help="published stable version, e.g. 0.1.0 or v0.1.0")
    args = parser.parse_args()
    version = args.version.removeprefix("v")
    if not re.fullmatch(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", version):
        parser.error("version must be a stable X.Y.Z version (optionally prefixed with v)")

    path = Path(__file__).resolve().parent / "Formula" / "zn.rb"
    try:
        response = subprocess.run(
            ["gh", "api", f"repos/ZenNotes/tui/releases/tags/v{version}"],
            check=True, capture_output=True, text=True,
        )
        formula = render_formula(path.read_text(), version, json.loads(response.stdout))
        path.write_text(formula)
    except subprocess.CalledProcessError as error:
        parser.exit(1, error.stderr)
    except (OSError, ValueError) as error:
        parser.exit(1, f"error: {error}\n")
    print(f"Updated {path} to v{version}. Copy Formula/zn.rb into ZenNotes/homebrew-tap after review.")


if __name__ == "__main__":
    main()
