#!/usr/bin/env bash
#
# Tag a release: checks that HEAD is a clean, pushed main commit with a
# green CI run and a matching CHANGELOG.md heading, then creates the
# annotated tag and pushes it. The Release workflow takes it from there.
#
# Usage: scripts/release.sh [--dry-run] X.Y.Z

set -euo pipefail

die() {
	echo "release: $*" >&2
	exit 1
}

usage() {
	die "usage: scripts/release.sh [--dry-run] X.Y.Z"
}

dry_run=false
version=""
for arg in "$@"; do
	case "$arg" in
	--dry-run) dry_run=true ;;
	-*) usage ;;
	*)
		[[ -z "$version" ]] || usage
		version="$arg"
		;;
	esac
done

version="${version#v}"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || usage
tag="v$version"

cd "$(git rev-parse --show-toplevel)"

git fetch --quiet --tags origin main

branch="$(git symbolic-ref --quiet --short HEAD || true)"
[[ "$branch" == "main" ]] || die "not on main (on ${branch:-a detached HEAD})"

[[ -z "$(git status --porcelain)" ]] || die "working tree is not clean; commit, stash, or remove changes first"

head="$(git rev-parse HEAD)"
upstream="$(git rev-parse origin/main)"
if [[ "$head" != "$upstream" ]]; then
	ahead="$(git rev-list --count origin/main..HEAD)"
	behind="$(git rev-list --count HEAD..origin/main)"
	die "HEAD is $ahead ahead of and $behind behind origin/main; push or pull first"
fi

if git rev-parse --quiet --verify "refs/tags/$tag" >/dev/null; then
	die "tag $tag already exists locally"
fi
if [[ -n "$(git ls-remote --tags origin "refs/tags/$tag")" ]]; then
	die "tag $tag already exists on origin"
fi

latest="$(git tag --list 'v*' --sort=-v:refname | head -n 1)"
if [[ -n "$latest" ]]; then
	highest="$(printf '%s\n%s\n' "${latest#v}" "$version" | sort -V | tail -n 1)"
	[[ "$highest" == "$version" ]] || die "$tag is not newer than the latest tag $latest"
fi

heading="$(grep -m 1 '^## ' CHANGELOG.md || true)"
[[ "$heading" == "## $tag" ]] || die "CHANGELOG.md's first heading is '${heading:-none}', want '## $tag'"

command -v gh >/dev/null || die "gh is not installed; it's needed to check CI"
ci="$(gh run list --workflow CI --commit "$head" --limit 1 \
	--json status,conclusion --jq '.[0] | "\(.status) \(.conclusion)"')" ||
	die "couldn't query CI runs with gh (is it authenticated?)"
case "$ci" in
"completed success") ;;
"" | "null null") die "no CI run found for ${head:0:7}; push main and wait for CI" ;;
completed*) die "CI for ${head:0:7} concluded '${ci#completed }'" ;;
*) die "CI for ${head:0:7} is still running (${ci%% *}); wait for it and rerun" ;;
esac

if $dry_run; then
	echo "release: all checks passed for $tag at ${head:0:7}"
	exit 0
fi

echo "$tag → $(git log -1 --format='%h %s' "$head")"
printf 'Tag and push? [y/N] '
read -r answer 2>/dev/null </dev/tty || die "no terminal to confirm on; aborted"
[[ "$answer" == "y" || "$answer" == "Y" ]] || die "aborted"

git tag -a "$tag" -m "$tag" "$head"
if ! git push origin "refs/tags/$tag"; then
	git tag -d "$tag" >/dev/null
	die "push failed; deleted the local tag $tag"
fi

echo "release: pushed $tag; the Release workflow is at $(gh repo view --json url --jq .url)/actions/workflows/release.yml"
