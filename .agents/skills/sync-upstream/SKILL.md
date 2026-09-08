---
name: sync-upstream
description: Bring upstream Treehouse changes into a reviewable PR in this fork while preserving local customizations. Use when asked to sync or update from upstream.
---

# Sync upstream

1. Inspect the working tree and the `origin` (this fork) and `upstream` remotes. Preserve unrelated work; use an isolated worktree if needed. Before changes, state a short acceptance checklist in the conversation: required behavior, fork customizations that must not change, and the smallest proving commands. Run those commands and report their results before declaring the PR ready.
2. Check `git remote get-url upstream` before fetching. If missing, add it with `git remote add upstream <url>` only using a URL already confirmed by the user; otherwise ask for the URL and stop before fetching or changing refs. Fetch both remotes, identify their default branches, and record their immutable tips as `fork_sha` and `upstream_sha`. If upstream is already an ancestor of the fork's tip, report that there is nothing to sync.
3. Create a unique branch such as `sync/upstream-YYYY-MM-DD-<random-suffix>` from `fork_sha`. Check both local refs and `git ls-remote --heads origin` for collisions; choose another unused name instead of reusing or overwriting a branch. Merge `upstream_sha` into it, preserving upstream ancestry and this fork's customizations. Resolve conflicts deliberately; ask about conflicting product intent when necessary. If the histories diverged, record the resulting two-parent merge commit as `merge_sha`.
4. Inspect the resulting diff against origin and verify the affected behavior. Before publication, run `git merge-base --is-ancestor <upstream_sha> <candidate_sha>` and, for a divergent sync, `git merge-base --is-ancestor <merge_sha> <candidate_sha>`. Both must exit 0. Use the installed `no-mistakes` skill for validation, all pushes, and PR creation. Preserve the upstream merge ancestry through the pipeline; do not flatten it with rebase or squash. If the pipeline cannot preserve it, stop and explain the blocker.
5. Open the PR against this fork's default branch, never upstream. Give it a descriptive title such as “Sync upstream: lease safety fixes and Windows support”, based on the actual changes. Summarize upstream changes, effects on fork customizations, conflict resolutions, and validation in the body.
6. Fetch the published branch and obtain the PR head SHA. Confirm it matches `git ls-remote --heads origin refs/heads/<sync-branch>`, then repeat the ancestry checks against that exact SHA. If any check fails, report the PR as blocked, not ready; recheck after any pipeline rewrite. Return the PR link and verification results for user review. Leave the fork's default branch untouched; do not merge or enable auto-merge. Recommend “Create a merge commit” when the user merges so future syncs retain upstream ancestry.

Never reset the fork to upstream, discard fork commits, or force-push as part of a sync.
