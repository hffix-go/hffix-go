#!/usr/bin/env python3
"""Tag and publish a release from the VERSION file.

The release tag is built by prefixing the contents of VERSION with "v", so a
VERSION of "1.2.3" produces the tag "v1.2.3".

The script refuses to tag unless the local checkout is on main, has no
unstaged, staged, or untracked changes, and matches the remote branch.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
VERSION_FILE = REPO_ROOT / "VERSION"
VERSION_NAME = VERSION_FILE.name
RELEASE_BRANCH = "main"
VERSION_RE = re.compile(r"^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.+-]+)?$")


class ReleaseError(Exception):
    pass


def run_git(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["git", *args], cwd=REPO_ROOT, text=True, capture_output=True
    )


def git(*args: str) -> str:
    result = run_git(*args)
    if result.returncode != 0:
        detail = result.stderr.strip() or result.stdout.strip()
        raise ReleaseError(f"git {' '.join(args)} failed: {detail}")
    return result.stdout.strip()


def git_status_lines() -> list[str]:
    output = git("status", "--porcelain")
    return [line for line in output.splitlines() if line.strip()]


def read_version() -> str:
    try:
        raw = VERSION_FILE.read_text(encoding="utf-8")
    except OSError as exc:
        reason = exc.strerror or exc
        raise ReleaseError(f"cannot read {VERSION_NAME}: {reason}") from exc

    version = raw.strip()
    if not version:
        raise ReleaseError(f"{VERSION_NAME} is empty")
    if "\n" in version or "\r" in version:
        raise ReleaseError(f"{VERSION_NAME} must contain a single line")
    if not VERSION_RE.match(version):
        raise ReleaseError(
            f"{VERSION_NAME} contains {version!r}, which is not a semver version"
        )
    return version


def ensure_on_release_branch() -> None:
    result = run_git("symbolic-ref", "--quiet", "--short", "HEAD")
    if result.returncode != 0:
        raise ReleaseError(
            f"releases must be made from {RELEASE_BRANCH}, but HEAD is detached"
        )
    branch = result.stdout.strip()
    if branch != RELEASE_BRANCH:
        raise ReleaseError(
            f"releases must be made from {RELEASE_BRANCH}, currently on {branch}"
        )


def ensure_clean_work_tree() -> None:
    changes = git_status_lines()
    if changes:
        listed = "\n".join(f"  {line}" for line in changes)
        raise ReleaseError(
            "working tree is not clean; commit or stash local changes first:\n"
            f"{listed}"
        )


def sync_with_remote(remote: str) -> None:
    remote_ref = f"{remote}/{RELEASE_BRANCH}"
    print(f"Fetching {remote_ref}...")
    refspec = f"+refs/heads/{RELEASE_BRANCH}:refs/remotes/{remote_ref}"
    try:
        git("fetch", remote, refspec)
    except ReleaseError as exc:
        raise ReleaseError(f"cannot fetch {remote_ref}: {exc}") from exc

    try:
        git("rev-parse", "--verify", "--quiet", f"refs/remotes/{remote_ref}")
    except ReleaseError as exc:
        raise ReleaseError(f"{remote_ref} does not exist") from exc

    local = git("rev-parse", "HEAD")
    remote_head = git("rev-parse", f"refs/remotes/{remote_ref}")
    if local == remote_head:
        print(f"Already in sync with {remote_ref}.")
        return

    behind = is_ancestor(local, remote_head)
    ahead = is_ancestor(remote_head, local)
    if ahead:
        raise ReleaseError(
            f"local {RELEASE_BRANCH} has commits that are not on {remote_ref}; "
            "push them first"
        )
    if not behind:
        raise ReleaseError(
            f"local {RELEASE_BRANCH} has diverged from {remote_ref}; "
            "reconcile them first"
        )

    print(f"Fast-forwarding {RELEASE_BRANCH} to {remote_ref}...")
    git("merge", "--ff-only", remote_head)


def is_ancestor(ancestor: str, descendant: str) -> bool:
    result = run_git("merge-base", "--is-ancestor", ancestor, descendant)
    if result.returncode not in (0, 1):
        detail = result.stderr.strip() or result.stdout.strip()
        raise ReleaseError(f"git merge-base failed: {detail}")
    return result.returncode == 0


def ensure_tag_available(tag: str, remote: str) -> None:
    existing = run_git("rev-parse", "--verify", "--quiet", f"refs/tags/{tag}")
    if existing.returncode == 0:
        raise ReleaseError(f"tag {tag} already exists locally")

    remote_tag = run_git(
        "ls-remote", "--tags", "--exit-code", remote, f"refs/tags/{tag}"
    )
    if remote_tag.returncode == 0:
        raise ReleaseError(f"tag {tag} already exists on {remote}")
    if remote_tag.returncode != 2:
        detail = remote_tag.stderr.strip() or remote_tag.stdout.strip()
        raise ReleaseError(f"cannot list tags on {remote}: {detail}")


def create_and_push_tag(tag: str, remote: str, dry_run: bool) -> None:
    message = f"Release {tag}"
    if dry_run:
        head = git("rev-parse", "--short", "HEAD")
        print(f"Would create tag {tag} ({message}) at {head}.")
        print(f"Would push {tag} to {remote}.")
        return

    print(f"Creating tag {tag}...")
    git("tag", "--annotate", tag, "--message", message)

    print(f"Pushing {tag} to {remote}...")
    try:
        git("push", remote, f"refs/tags/{tag}")
    except ReleaseError as exc:
        raise ReleaseError(
            f"failed to push {tag} to {remote}: {exc}\n"
            f"The local tag was left in place; remove it with 'git tag -d {tag}' "
            f"before retrying."
        ) from exc


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--remote",
        default="origin",
        help="remote to sync with and push the tag to (default: origin)",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="run all checks without creating or pushing the tag",
    )
    return parser.parse_args(argv)


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    try:
        git("rev-parse", "--is-inside-work-tree")
        ensure_on_release_branch()
        ensure_clean_work_tree()
        tag = f"v{read_version()}"
        ensure_tag_available(tag, args.remote)
        sync_with_remote(args.remote)
        create_and_push_tag(tag, args.remote, args.dry_run)
    except ReleaseError as exc:
        print(f"release: {exc}", file=sys.stderr)
        return 1

    if args.dry_run:
        print(f"Dry run complete; release {tag} was not published.")
    else:
        print(f"Released {tag}.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
