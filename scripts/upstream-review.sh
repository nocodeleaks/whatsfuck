#!/usr/bin/env bash

set -euo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly REPOSITORY_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
readonly LOCK_FILE="$REPOSITORY_ROOT/UPSTREAMS.lock.json"
readonly REVIEW_SCHEMA="$REPOSITORY_ROOT/.github/upstream-review.schema.json"

usage() {
  cat <<'EOF'
Usage:
  scripts/upstream-review.sh status [--fetch] [--json]
  scripts/upstream-review.sh review <official|hypermeow> [--from SHA] [--to SHA] [--output DIR] [--no-ai]
  scripts/upstream-review.sh apply-official <review.json> [--dry-run]
  scripts/upstream-review.sh apply-hypermeow <SHA> <review.json> [--dry-run]
  scripts/upstream-review.sh record-hypermeow-review <review.json> [--dry-run]

The official source is authoritative and is integrated as a reviewed range.
HyperMeow is untrusted and only exact, individually approved commits may be
prepared with cherry-pick --no-commit. No command commits or pushes changes.
EOF
}

die() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "Required command not found: $1"
}

source_field() {
  local source="$1"
  local field="$2"
  jq -er --arg source "$source" --arg field "$field" '.sources[$source][$field]' "$LOCK_FILE"
}

validate_source() {
  case "$1" in
    official|hypermeow) ;;
    *) die "Source must be official or hypermeow" ;;
  esac
}

baseline_for() {
  local source="$1"
  if [[ "$source" == "official" ]]; then
    source_field official integrated_commit
  else
    source_field hypermeow reviewed_through
  fi
}

remote_ref_for() {
  local source="$1"
  printf '%s/%s\n' "$(source_field "$source" remote)" "$(source_field "$source" branch)"
}

fetch_source() {
  local source="$1"
  local remote branch
  remote="$(source_field "$source" remote)"
  branch="$(source_field "$source" branch)"
  git -C "$REPOSITORY_ROOT" fetch --quiet "$remote" \
    "+refs/heads/$branch:refs/remotes/$remote/$branch"
}

resolve_commit() {
  git -C "$REPOSITORY_ROOT" rev-parse --verify "$1^{commit}"
}

assert_source_commit() {
  local source="$1"
  local commit="$2"
  local remote_ref
  remote_ref="$(remote_ref_for "$source")"
  git -C "$REPOSITORY_ROOT" merge-base --is-ancestor "$commit" "$remote_ref" ||
    die "$commit is not part of $remote_ref"
}

assert_safe_integration_workspace() {
  local branch protected
  branch="$(git -C "$REPOSITORY_ROOT" branch --show-current)"
  [[ -n "$branch" ]] || die "Integration is not allowed from a detached HEAD"

  while IFS= read -r protected; do
    [[ "$branch" != "$protected" ]] ||
      die "Integration is blocked on protected branch '$branch'; use a dedicated review branch"
  done < <(jq -r '.policy.protected_branches[]' "$LOCK_FILE")

  [[ -z "$(git -C "$REPOSITORY_ROOT" status --porcelain)" ]] ||
    die "Integration requires a clean worktree"
}

status_command() {
  local fetch=false json=false
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --fetch) fetch=true ;;
      --json) json=true ;;
      *) die "Unknown status option: $1" ;;
    esac
    shift
  done

  local source base current pending
  local records='[]'
  for source in official hypermeow; do
    if [[ "$fetch" == true ]]; then
      fetch_source "$source"
    fi
    base="$(resolve_commit "$(baseline_for "$source")")"
    current="$(resolve_commit "$(remote_ref_for "$source")")"
    git -C "$REPOSITORY_ROOT" merge-base --is-ancestor "$base" "$current" ||
      die "Recorded $source baseline is not an ancestor of the current source branch"
    pending="$(git -C "$REPOSITORY_ROOT" rev-list --count "$base..$current")"
    records="$(jq -cn \
      --argjson records "$records" \
      --arg source "$source" \
      --arg base "$base" \
      --arg current "$current" \
      --argjson pending "$pending" \
      '$records + [{source: $source, baseline: $base, current: $current, pending: $pending}]')"
  done

  if [[ "$json" == true ]]; then
    jq -n --argjson sources "$records" '{sources: $sources}'
    return
  fi

  printf '%-12s %-12s %-12s %s\n' SOURCE BASELINE CURRENT PENDING
  jq -r '.[] | [.source, .baseline[0:12], .current[0:12], (.pending | tostring)] | @tsv' <<<"$records" |
    while IFS=$'\t' read -r source base current pending; do
      printf '%-12s %-12s %-12s %s\n' "$source" "$base" "$current" "$pending"
    done
}

write_review_packet() {
  local source="$1"
  local base="$2"
  local target="$3"
  local output_dir="$4"
  local commits_file="$output_dir/commits.txt"
  local packet_file="$output_dir/packet.md"
  local patch_file="$output_dir/changes.patch"
  local prompt_file="$output_dir/prompt.md"

  git -C "$REPOSITORY_ROOT" rev-list --reverse "$base..$target" >"$commits_file"
  [[ -s "$commits_file" ]] || die "There are no commits to review for $source"
  git -C "$REPOSITORY_ROOT" diff --find-renames "$base" "$target" >"$patch_file"

  {
    printf '# Upstream review packet\n\n'
    printf -- '- Source: `%s`\n' "$source"
    printf -- '- Base: `%s`\n' "$base"
    printf -- '- Target: `%s`\n\n' "$target"
    printf '## Commits\n\n'
    while IFS= read -r commit; do
      git -C "$REPOSITORY_ROOT" show -s --format='- `%H` — %s (%an, %aI)' "$commit"
    done <"$commits_file"
    printf '\n## Changed files\n\n```text\n'
    git -C "$REPOSITORY_ROOT" diff --name-status "$base" "$target"
    printf '```\n\n## Diff statistics\n\n```text\n'
    git -C "$REPOSITORY_ROOT" diff --stat "$base" "$target"
    printf '```\n'
  } >"$packet_file"

  {
    printf 'Review the exact `%s` upstream range `%s..%s` for WhatsFuck.\n\n' "$source" "$base" "$target"
    printf 'Read `%s`, inspect every listed commit and the complete patch at `%s`, and inspect repository code when needed. ' "$packet_file" "$patch_file"
    printf 'Do not edit files, run mutating commands, or broaden the commit range. Return only JSON matching the supplied schema.\n\n'
    if [[ "$source" == "official" ]]; then
      printf 'The official WhatsMeow source is authoritative for WhatsApp protocol compatibility. '
      printf 'Evaluate integration conflicts, regressions, database/time semantics, API changes, and required tests. '
      printf 'Do not reject a commit merely because its architecture differs from the fork; use manual_review when human conflict resolution is required.\n'
    else
      printf 'HyperMeow is untrusted. Evaluate every commit independently and approve only exact commits with a demonstrated benefit, acceptable licensing, no hidden behavior, no protocol-compatibility regression, and a clear validation plan. '
      printf 'Reject speculative rewrites, unexplained database changes, unsafe concurrency, telemetry, credential handling, or commits whose value cannot be proven from the patch.\n'
    fi
  } >"$prompt_file"
}

review_command() {
  [[ $# -ge 1 ]] || die "review requires a source"
  local source="$1"
  shift
  validate_source "$source"

  local base='' target='' output_dir='' no_ai=false
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --from) [[ $# -ge 2 ]] || die "--from requires a SHA"; base="$2"; shift ;;
      --to) [[ $# -ge 2 ]] || die "--to requires a SHA"; target="$2"; shift ;;
      --output) [[ $# -ge 2 ]] || die "--output requires a directory"; output_dir="$2"; shift ;;
      --no-ai) no_ai=true ;;
      *) die "Unknown review option: $1" ;;
    esac
    shift
  done

  fetch_source "$source"
  base="$(resolve_commit "${base:-$(baseline_for "$source")}")"
  target="$(resolve_commit "${target:-$(remote_ref_for "$source")}")"
  assert_source_commit "$source" "$target"
  git -C "$REPOSITORY_ROOT" merge-base --is-ancestor "$base" "$target" ||
    die "Review base is not an ancestor of target"

  if [[ -z "$output_dir" ]]; then
    output_dir="$REPOSITORY_ROOT/.git/upstream-reviews/$(date -u +%Y%m%dT%H%M%SZ)-$source"
  elif [[ "$output_dir" != /* ]]; then
    output_dir="$PWD/$output_dir"
  fi
  mkdir -p "$output_dir"
  write_review_packet "$source" "$base" "$target" "$output_dir"

  printf 'Review packet: %s\n' "$output_dir/packet.md"
  printf 'Complete patch: %s\n' "$output_dir/changes.patch"

  if [[ "$no_ai" == true ]]; then
    printf 'AI review skipped; no integration can use this packet as an approval.\n'
    return
  fi

  require_command codex
  codex exec --ephemeral --sandbox read-only -C "$REPOSITORY_ROOT" \
    --output-schema "$REVIEW_SCHEMA" \
    --output-last-message "$output_dir/review.json" \
    - <"$output_dir/prompt.md"
  jq -e \
    --arg source "$source" \
    --arg base "$base" \
    --arg target "$target" \
    '.reviewer == "codex" and .source == $source and .base_commit == $base and .target_commit == $target' \
    "$output_dir/review.json" >/dev/null ||
    die "AI review identity does not match the requested source range"
  printf 'AI review: %s\n' "$output_dir/review.json"
}

validate_review_identity() {
  local review_file="$1"
  local expected_source="$2"
  [[ -f "$review_file" ]] || die "Review file not found: $review_file"
  jq -e --arg source "$expected_source" '.source == $source' "$review_file" >/dev/null ||
    die "Review source does not match $expected_source"
  jq -e '.reviewer == "codex"' "$review_file" >/dev/null ||
    die "Review was not produced by the configured Codex workflow"
  jq -er '.base_commit' "$review_file" >/dev/null
  jq -er '.target_commit' "$review_file" >/dev/null
}

update_lock_official() {
  local target="$1"
  local now temp_file
  now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  temp_file="$(mktemp "$REPOSITORY_ROOT/.git/upstreams-lock.XXXXXX")"
  jq --arg target "$target" --arg now "$now" '
    .reviewed_at = $now |
    .sources.official.integrated_commit = $target |
    .sources.official.observed_commit = $target
  ' "$LOCK_FILE" >"$temp_file"
  mv "$temp_file" "$LOCK_FILE"
}

record_hypermeow_review() {
  local review_file="$1"
  local now target temp_file
  validate_review_identity "$review_file" hypermeow
  now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  target="$(jq -er '.target_commit' "$review_file")"
  assert_source_commit hypermeow "$target"
  temp_file="$(mktemp "$REPOSITORY_ROOT/.git/upstreams-lock.XXXXXX")"
  jq --arg now "$now" --arg target "$target" --slurpfile review "$review_file" '
    .reviewed_at = $now |
    .sources.hypermeow.reviewed_through = $target |
    .sources.hypermeow.observed_commit = $target |
    .sources.hypermeow.decisions = (
      [.sources.hypermeow.decisions[] | select(.commit as $commit | ($review[0].decisions | map(.commit) | index($commit) | not))] +
      [$review[0].decisions[] | . + {reviewed_at: $now}]
    )
  ' "$LOCK_FILE" >"$temp_file"
  mv "$temp_file" "$LOCK_FILE"
}

apply_official_command() {
  [[ $# -ge 1 ]] || die "apply-official requires review.json"
  local review_file="$1" dry_run=false
  shift
  [[ "${1:-}" != "--dry-run" ]] || dry_run=true
  validate_review_identity "$review_file" official
  assert_safe_integration_workspace

  local base target verdict expected_base
  base="$(jq -er '.base_commit' "$review_file")"
  target="$(jq -er '.target_commit' "$review_file")"
  verdict="$(jq -er '.verdict' "$review_file")"
  expected_base="$(resolve_commit "$(baseline_for official)")"
  [[ "$base" == "$expected_base" ]] || die "Review does not start at the recorded official baseline"
  [[ "$verdict" != "rejected" ]] || die "Official range was rejected by the review"
  assert_source_commit official "$target"

  if [[ "$dry_run" == true ]]; then
    printf 'Validated official range %s..%s; no changes applied.\n' "$base" "$target"
    return
  fi

  git -C "$REPOSITORY_ROOT" merge --no-commit --no-ff "$target"
  update_lock_official "$target"
  printf 'Official range prepared without a commit. Run the complete tests before committing or publishing.\n'
}

apply_hypermeow_command() {
  [[ $# -ge 2 ]] || die "apply-hypermeow requires SHA and review.json"
  local requested_commit="$1" review_file="$2" dry_run=false
  shift 2
  [[ "${1:-}" != "--dry-run" ]] || dry_run=true
  validate_review_identity "$review_file" hypermeow
  assert_safe_integration_workspace

  local commit parent_count base target expected_base
  commit="$(resolve_commit "$requested_commit")"
  base="$(resolve_commit "$(jq -er '.base_commit' "$review_file")")"
  target="$(resolve_commit "$(jq -er '.target_commit' "$review_file")")"
  expected_base="$(resolve_commit "$(baseline_for hypermeow)")"
  [[ "$base" == "$expected_base" ]] || die "Review does not start at the recorded HyperMeow baseline"
  assert_source_commit hypermeow "$target"
  assert_source_commit hypermeow "$commit"
  git -C "$REPOSITORY_ROOT" merge-base --is-ancestor "$base" "$commit" ||
    die "Requested HyperMeow commit precedes the reviewed range"
  git -C "$REPOSITORY_ROOT" merge-base --is-ancestor "$commit" "$target" ||
    die "Requested HyperMeow commit is outside the reviewed range"
  jq -e --arg commit "$commit" '.decisions[] | select(.commit == $commit and .verdict == "approved")' "$review_file" >/dev/null ||
    die "The exact HyperMeow commit is not individually approved by this review"
  parent_count="$(git -C "$REPOSITORY_ROOT" show -s --format='%P' "$commit" | awk '{print NF}')"
  [[ "$parent_count" -le 1 ]] || die "HyperMeow merge commits cannot be cherry-picked by this workflow"

  if [[ "$dry_run" == true ]]; then
    printf 'Validated approved HyperMeow commit %s; no changes applied.\n' "$commit"
    return
  fi

  git -C "$REPOSITORY_ROOT" cherry-pick --no-commit "$commit"
  record_hypermeow_review "$review_file"
  local temp_file
  temp_file="$(mktemp "$REPOSITORY_ROOT/.git/upstreams-lock.XXXXXX")"
  jq --arg commit "$commit" '
    .sources.hypermeow.integrated_commits = ((.sources.hypermeow.integrated_commits + [$commit]) | unique)
  ' "$LOCK_FILE" >"$temp_file"
  mv "$temp_file" "$LOCK_FILE"
  printf 'HyperMeow commit prepared without a commit. Run the complete tests before committing or publishing.\n'
}

record_hypermeow_review_command() {
  [[ $# -ge 1 ]] || die "record-hypermeow-review requires review.json"
  local review_file="$1" dry_run=false
  shift
  [[ "${1:-}" != "--dry-run" ]] || dry_run=true
  validate_review_identity "$review_file" hypermeow

  if [[ "$dry_run" == true ]]; then
    local target
    target="$(jq -er '.target_commit' "$review_file")"
    assert_source_commit hypermeow "$target"
    printf 'Validated HyperMeow review through %s; lock file not changed.\n' "$target"
    return
  fi

  record_hypermeow_review "$review_file"
  printf 'HyperMeow decisions recorded. Commit the lock-file change only after reviewing it.\n'
}

main() {
  require_command git
  require_command jq
  [[ -f "$LOCK_FILE" ]] || die "UPSTREAMS.lock.json not found"

  local command="${1:-status}"
  if [[ $# -gt 0 ]]; then
    shift
  fi
  case "$command" in
    status) status_command "$@" ;;
    review) review_command "$@" ;;
    apply-official) apply_official_command "$@" ;;
    apply-hypermeow) apply_hypermeow_command "$@" ;;
    record-hypermeow-review) record_hypermeow_review_command "$@" ;;
    help|-h|--help) usage ;;
    *) usage >&2; die "Unknown command: $command" ;;
  esac
}

main "$@"
