#!/usr/bin/env bash
# Stop hook: on the PR 9 working branch, do not let the agent stop until an open pull request
# exists and every check run on its latest commit has finished green. Anything else → block.
# If GitHub cannot be reached the hook allows the stop, so it never traps a session.
set -uo pipefail
BRANCH_GUARDED="claude/beautiful-darwin-ysnct4"
REPO="bdnix/kubelook"

block() { jq -n --arg r "$1" '{decision:"block", reason:$r}'; exit 0; }

cat >/dev/null # the hook input is not needed
branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
[ "$branch" = "$BRANCH_GUARDED" ] || exit 0

pr="$(gh api "repos/$REPO/pulls?head=bdnix:$branch&state=open&per_page=1" 2>/dev/null)" || exit 0
number="$(jq -r '.[0].number // empty' <<<"$pr")"
sha="$(jq -r '.[0].head.sha // empty' <<<"$pr")"
if [ -z "$number" ]; then
  block "Do not stop yet: there is no open pull request for $branch. AGENTS.md says to finish every step of the pull request, open it, and get every CI check green before stopping. Keep working on the remaining steps (see the task list)."
fi

runs="$(gh api "repos/$REPO/commits/$sha/check-runs?per_page=100" 2>/dev/null)" || exit 0
total="$(jq '.total_count' <<<"$runs")"
bad="$(jq -r '[.check_runs[] | select(.status != "completed" or (.conclusion | IN("success","skipped","neutral") | not)) | "\(.name): \(.status)/\(.conclusion // "-")"] | join("; ")' <<<"$runs")"
if [ "$total" = "0" ]; then
  block "Do not stop yet: pull request #$number has no finished check runs on $sha. Wait for CI (use a monitor, do not end the turn) and fix anything that fails."
fi
if [ -n "$bad" ]; then
  block "Do not stop yet: checks on pull request #$number are not all green: $bad. Read the failing job logs, fix the cause, push, and watch again. Never skip a test or lower a threshold."
fi
exit 0
