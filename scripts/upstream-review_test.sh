#!/usr/bin/env bash

set -euo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly SOURCE_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
readonly TEST_ROOT="$(mktemp -d)"

cleanup() {
  rm -rf -- "$TEST_ROOT"
}
trap cleanup EXIT

git_configure() {
  git -C "$1" config user.name 'Upstream Review Test'
  git -C "$1" config user.email 'upstream-review@example.invalid'
}

commit_file() {
  local repository="$1"
  local path="$2"
  local content="$3"
  local message="$4"
  printf '%s\n' "$content" >"$repository/$path"
  git -C "$repository" add "$path"
  git -C "$repository" commit -q -m "$message"
}

expect_failure() {
  local expected="$1"
  shift
  local output
  if output="$("$@" 2>&1)"; then
    printf 'Expected command to fail: %s\n' "$*" >&2
    exit 1
  fi
  grep -F "$expected" <<<"$output" >/dev/null || {
    printf 'Expected failure containing %q, got:\n%s\n' "$expected" "$output" >&2
    exit 1
  }
}

git init -q --bare "$TEST_ROOT/official.git"
git init -q --bare "$TEST_ROOT/hypermeow.git"

git init -q -b main "$TEST_ROOT/official-work"
git_configure "$TEST_ROOT/official-work"
commit_file "$TEST_ROOT/official-work" protocol.txt baseline 'Official baseline'
git -C "$TEST_ROOT/official-work" remote add origin "$TEST_ROOT/official.git"
git -C "$TEST_ROOT/official-work" push -q -u origin main
git -C "$TEST_ROOT/official.git" symbolic-ref HEAD refs/heads/main
official_base="$(git -C "$TEST_ROOT/official-work" rev-parse HEAD)"

git clone -q "$TEST_ROOT/official.git" "$TEST_ROOT/hyper-work"
git_configure "$TEST_ROOT/hyper-work"
commit_file "$TEST_ROOT/hyper-work" architecture.txt candidate 'HyperMeow candidate'
git -C "$TEST_ROOT/hyper-work" remote set-url origin "$TEST_ROOT/hypermeow.git"
git -C "$TEST_ROOT/hyper-work" push -q -u origin main
git -C "$TEST_ROOT/hypermeow.git" symbolic-ref HEAD refs/heads/main
hyper_target="$(git -C "$TEST_ROOT/hyper-work" rev-parse HEAD)"

git clone -q "$TEST_ROOT/official.git" "$TEST_ROOT/integration"
git_configure "$TEST_ROOT/integration"
mkdir -p "$TEST_ROOT/integration/scripts" "$TEST_ROOT/integration/.github"
cp "$SOURCE_ROOT/scripts/upstream-review.sh" "$TEST_ROOT/integration/scripts/upstream-review.sh"
cp "$SOURCE_ROOT/.github/upstream-review.schema.json" "$TEST_ROOT/integration/.github/upstream-review.schema.json"
chmod +x "$TEST_ROOT/integration/scripts/upstream-review.sh"
git -C "$TEST_ROOT/integration" remote add official "$TEST_ROOT/official.git"
git -C "$TEST_ROOT/integration" remote add hypermeow "$TEST_ROOT/hypermeow.git"
git -C "$TEST_ROOT/integration" fetch -q official main
git -C "$TEST_ROOT/integration" fetch -q hypermeow main

jq -n \
  --arg official "$official_base" \
  --arg hyper "$official_base" \
  '{
    schema_version: 2,
    integration_repository: "example/whatsfuck",
    reviewed_at: "2026-08-13T00:00:00Z",
    sources: {
      official: {
        remote: "official",
        repository: "example/official",
        branch: "main",
        integrated_commit: $official,
        observed_commit: $official
      },
      hypermeow: {
        remote: "hypermeow",
        repository: "example/hypermeow",
        branch: "main",
        baseline_commit: $hyper,
        reviewed_through: $hyper,
        observed_commit: $hyper,
        integrated_commits: [],
        decisions: []
      }
    },
    policy: {protected_branches: ["main", "master"]}
  }' >"$TEST_ROOT/integration/UPSTREAMS.lock.json"
git -C "$TEST_ROOT/integration" add scripts .github UPSTREAMS.lock.json
git -C "$TEST_ROOT/integration" commit -q -m 'Add review tooling'

commit_file "$TEST_ROOT/official-work" protocol.txt updated 'Official compatibility update'
git -C "$TEST_ROOT/official-work" push -q
official_target="$(git -C "$TEST_ROOT/official-work" rev-parse HEAD)"
git -C "$TEST_ROOT/integration" fetch -q official main

status_json="$($TEST_ROOT/integration/scripts/upstream-review.sh status --json)"
jq -e '.sources[] | select(.source == "official" and .pending == 1)' <<<"$status_json" >/dev/null
jq -e '.sources[] | select(.source == "hypermeow" and .pending == 1)' <<<"$status_json" >/dev/null

review_root="$TEST_ROOT/reviews"
"$TEST_ROOT/integration/scripts/upstream-review.sh" review official --output "$review_root/official" --no-ai >/dev/null
"$TEST_ROOT/integration/scripts/upstream-review.sh" review hypermeow --output "$review_root/hypermeow" --no-ai >/dev/null
[[ -s "$review_root/official/changes.patch" ]]
[[ -s "$review_root/hypermeow/changes.patch" ]]

jq -n \
  --arg base "$official_base" \
  --arg target "$official_target" \
  '{source: "official", reviewer: "codex", base_commit: $base, target_commit: $target, verdict: "approved", summary: "test", decisions: []}' \
  >"$review_root/official.json"
jq -n \
  --arg base "$official_base" \
  --arg target "$hyper_target" \
  '{source: "hypermeow", reviewer: "codex", base_commit: $base, target_commit: $target, verdict: "approved", summary: "test", decisions: [{commit: $target, verdict: "approved", rationale: "test", risks: [], tests: []}]}' \
  >"$review_root/hypermeow-approved.json"
jq -n \
  --arg base "$official_base" \
  --arg target "$hyper_target" \
  '{source: "hypermeow", reviewer: "codex", base_commit: $base, target_commit: $target, verdict: "rejected", summary: "test", decisions: [{commit: $target, verdict: "rejected", rationale: "test", risks: [], tests: []}]}' \
  >"$review_root/hypermeow-rejected.json"

expect_failure "protected branch 'main'" \
  "$TEST_ROOT/integration/scripts/upstream-review.sh" apply-official "$review_root/official.json" --dry-run

git -C "$TEST_ROOT/integration" switch -q -c review/upstream-policy-test
"$TEST_ROOT/integration/scripts/upstream-review.sh" apply-official "$review_root/official.json" --dry-run >/dev/null
"$TEST_ROOT/integration/scripts/upstream-review.sh" apply-hypermeow "$hyper_target" "$review_root/hypermeow-approved.json" --dry-run >/dev/null
expect_failure 'not individually approved' \
  "$TEST_ROOT/integration/scripts/upstream-review.sh" apply-hypermeow "$hyper_target" "$review_root/hypermeow-rejected.json" --dry-run

head_before="$(git -C "$TEST_ROOT/integration" rev-parse HEAD)"
"$TEST_ROOT/integration/scripts/upstream-review.sh" apply-hypermeow "$hyper_target" "$review_root/hypermeow-approved.json" >/dev/null
[[ -f "$TEST_ROOT/integration/architecture.txt" ]]
[[ "$(git -C "$TEST_ROOT/integration" rev-parse HEAD)" == "$head_before" ]]
jq -e --arg commit "$hyper_target" '.sources.hypermeow.integrated_commits | index($commit)' \
  "$TEST_ROOT/integration/UPSTREAMS.lock.json" >/dev/null
jq -e --arg commit "$hyper_target" '.sources.hypermeow.decisions[] | select(.commit == $commit and .verdict == "approved")' \
  "$TEST_ROOT/integration/UPSTREAMS.lock.json" >/dev/null

printf 'upstream-review tests passed\n'
