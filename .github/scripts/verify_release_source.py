"""Reject release tags outside main or inconsistent with the executable version."""

import argparse
import json
from pathlib import Path
import re
import subprocess


def commit_of(ref):
    return subprocess.check_output(
        ["git", "rev-parse", "--verify", ref + "^{commit}"], text=True
    ).strip()


def verify(ref, commit, metadata_path):
    match = re.fullmatch(r"refs/tags/v(\d+\.\d+\.\d+)(?:-[0-9A-Za-z.-]+)?", ref)
    if not match:
        raise ValueError("release must use a vMAJOR.MINOR.PATCH tag")
    if not re.fullmatch(r"[0-9a-fA-F]{40}", commit):
        raise ValueError("release commit must be a full commit SHA")
    actual = commit_of(commit)
    if commit_of(ref) != actual:
        raise ValueError("release tag does not point to the requested commit")
    ancestry = subprocess.run(
        ["git", "merge-base", "--is-ancestor", actual, "refs/remotes/origin/main"],
        check=False,
    )
    if ancestry.returncode != 0:
        raise ValueError("release commit must be reachable from origin/main")
    metadata = json.loads(Path(metadata_path).read_text(encoding="utf-8"))
    expected = match.group(1) + ".0"
    fixed = metadata["RT_VERSION"]["#1"]["0000"]["fixed"]
    identity = metadata["RT_MANIFEST"]["#1"]["0409"]["identity"]
    infos = metadata["RT_VERSION"]["#1"]["0000"]["info"]
    versions = [fixed["file_version"], fixed["product_version"], identity["version"]]
    versions += [value[key] for value in infos.values() for key in ("FileVersion", "ProductVersion")]
    if not versions or any(version != expected for version in versions):
        raise ValueError("release tag and executable resource versions differ")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--ref", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--metadata", default="winres/winres.json")
    args = parser.parse_args()
    try:
        verify(args.ref, args.commit, args.metadata)
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError) as exc:
        parser.exit(1, f"Release source rejected: {exc}\n")
    print("Release tag, main ancestry and resource versions verified")


if __name__ == "__main__":
    main()
