#!/usr/bin/env bash
# htmlup cleanup — remove published top-level entries older than TTL_DAYS from the
# checked-out Pages branch, then record the removals as a GitHub-signed commit.
#
# The commit is created through the GitHub API (createCommitOnBranch), so GitHub
# signs it with its own key using the workflow token — the commit shows as
# Verified and no GPG keys need to be managed in the target repo.
#
# Portable on purpose: timestamps come from `date -u +%s` (POSIX) and
# `git log --format=%ct` (Unix epoch), so it behaves identically on Linux CI and
# macOS dev machines.
#
# Required env:
#   TTL_DAYS          integer — entries whose last commit is older are removed
#   BRANCH            Pages branch the cleanup commit lands on
#   EXCLUDE_PATTERNS  space-separated globs that are never removed
# Required to commit (skipped when DRY_RUN=1):
#   GH_TOKEN          token with contents:write (the workflow's GITHUB_TOKEN)
#   GITHUB_REPOSITORY owner/name (set automatically on GitHub Actions)
set -euo pipefail
# Disable globbing: EXCLUDE_PATTERNS is split into words and matched by `case`,
# so the shell must not pathname-expand the globs when iterating them.
set -f

: "${TTL_DAYS:?TTL_DAYS is required}"
: "${BRANCH:?BRANCH is required}"
: "${EXCLUDE_PATTERNS:?EXCLUDE_PATTERNS is required}"

cutoff=$(($(date -u +%s) - TTL_DAYS * 86400))

# excluded returns success when the top-level entry matches a protected glob.
excluded() {
  local entry=$1 pat
  for pat in $EXCLUDE_PATTERNS; do
    # shellcheck disable=SC2254  # $pat is an intentional glob, not a literal
    case "$entry" in
      $pat) return 0 ;;
    esac
  done
  return 1
}

while IFS= read -r -d '' entry; do
  [ -n "$entry" ] || continue
  excluded "$entry" && continue
  last=$(git log -1 --format=%ct -- "$entry")
  [ -n "$last" ] || continue
  if [ "$last" -lt "$cutoff" ]; then
    echo "removing stale entry: $entry (last commit epoch $last)"
    git rm -r --quiet -- "$entry"
  fi
  # NUL-delimited and unquoted (quotepath=false) so entries with spaces,
  # newlines, or non-ASCII names match their real path instead of git's
  # C-quoted form — otherwise those entries would never be cleaned up.
done < <(git -c core.quotepath=false ls-tree -z --name-only HEAD)

# git rm -r expands directories into individual file paths, which is what the
# API deletion list needs. Kept as a newline-separated string (not a bash array)
# so the script also runs on macOS's bash 3.2.
deleted=$(git diff --cached --name-only --diff-filter=D)
if [ -z "$deleted" ]; then
  echo "nothing to clean up"
  exit 0
fi
count=$(printf '%s\n' "$deleted" | grep -c .)

if [ "${DRY_RUN:-}" = "1" ]; then
  echo "DRY_RUN=1 — staged $count deletion(s), skipping commit"
  exit 0
fi

: "${GH_TOKEN:?GH_TOKEN is required to commit}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required to commit}"

deletions=$(printf '%s' "$deleted" | jq -Rs 'split("\n") | map(select(length > 0) | {path: .})')
variables=$(jq -n \
  --arg repo "$GITHUB_REPOSITORY" \
  --arg branch "$BRANCH" \
  --arg oid "$(git rev-parse HEAD)" \
  --arg msg "htmlup-cleanup: remove entries older than ${TTL_DAYS} days" \
  --argjson dels "$deletions" \
  '{input: {branch: {repositoryNameWithOwner: $repo, branchName: $branch}, message: {headline: $msg}, expectedHeadOid: $oid, fileChanges: {deletions: $dels}}}')

jq -n \
  --arg q 'mutation($input: CreateCommitOnBranchInput!) { createCommitOnBranch(input: $input) { commit { oid } } }' \
  --argjson v "$variables" \
  '{query: $q, variables: $v}' | gh api /graphql --method POST --input - >/dev/null

echo "created signed cleanup commit on $BRANCH ($count deletion(s))"
