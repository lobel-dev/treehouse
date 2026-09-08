---
name: sync-upstream
description: Bring upstream Treehouse changes into a reviewable PR in this fork while preserving local customizations. Use when asked to sync or update from upstream.
---

# Sync upstream

1. Inspect the working tree and the `origin` (this fork) and `upstream` remotes. Preserve unrelated work; use an isolated worktree if needed. Follow the repository acceptance-checklist requirements.
2. Fetch both remotes and identify their default branches. If upstream is already an ancestor of the fork's default branch, report that there is nothing to sync.
3. Create a unique branch such as `sync/upstream-YYYY-MM-DD` from the fetched origin default branch. Merge the fetched upstream default branch into it, preserving upstream ancestry and this fork's customizations. Resolve conflicts deliberately; ask about conflicting product intent when necessary.
4. Inspect the resulting diff against origin and verify the affected behavior. Use the installed `no-mistakes` skill for validation, all pushes, and PR creation. Preserve the upstream merge ancestry through the pipeline; do not flatten it with rebase or squash. If the pipeline cannot preserve it, stop and explain the blocker.
5. Open the PR against this fork's default branch, never upstream. Give it a descriptive title such as “Sync upstream: lease safety fixes and Windows support”, based on the actual changes. Summarize upstream changes, effects on fork customizations, conflict resolutions, and validation in the body.
6. Return the PR link for user review. Leave the fork's default branch untouched; do not merge or enable auto-merge. Recommend “Create a merge commit” when the user merges so future syncs retain upstream ancestry.

Never reset the fork to upstream, discard fork commits, or force-push as part of a sync.
