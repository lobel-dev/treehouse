package pool

import (
	"fmt"
	"path/filepath"

	"github.com/kunchenguid/treehouse/internal/vcs"
)

// WorkAcquisition identifies a reserved slot. Reclaimed means its existing
// branch checkout was preserved without reset, seeding, or creation hooks.
// A nonempty Path requires guarded caller cleanup, even on acquisition error.
type WorkAcquisition struct {
	Path      string
	Reclaimed bool
}

// AcquireWorkBranch resolves actual refs and registrations; LastBranch is never
// consulted. Errors after allocation return its path for guarded caller cleanup.
func AcquireWorkBranch(repo, dir, branch string, max int, hooks []string, opts AcquireOptions) (WorkAcquisition, error) {
	if err := vcs.ValidateGitBranch(repo, branch); err != nil {
		return WorkAcquisition{}, err
	}
	if !opts.SkipFetch && vcs.HasRemote(repo, "origin") {
		if err := vcs.Fetch(repo); err != nil {
			return WorkAcquisition{}, fmt.Errorf("fetch failed: %w", err)
		}
	}
	facts, err := vcs.InspectGitBranch(repo, branch)
	if err != nil {
		return WorkAcquisition{}, err
	}
	if len(facts.Holders) > 1 {
		return WorkAcquisition{}, fmt.Errorf("branch %q has multiple worktree registrations; inspect git worktree list", branch)
	}
	if len(facts.Holders) == 1 {
		holder := facts.Holders[0]
		if holder.Missing {
			if holder.Locked {
				return WorkAcquisition{}, fmt.Errorf("branch %q is reserved by locked missing worktree %s; inspect git worktree list", branch, holder.Path)
			}
			// Normal acquisition already self-heals registrations when adding a
			// slot. Reuse needs the same cleanup when a missing holder reserves
			// this branch. Git preserves locked registrations; switch rechecks
			// occupancy after allocation before touching the acquired checkout.
			if err := vcs.PruneWorktrees(repo); err != nil {
				return WorkAcquisition{}, fmt.Errorf("cannot clean stale worktree registration: %w", err)
			}
		} else {
			path, err := ReclaimBranch(repo, dir, holder.Path, branch)
			return WorkAcquisition{Path: path, Reclaimed: err == nil}, err
		}
	}
	acquired, err := acquire(repo, dir, max, hooks, acquireOptions{skipFetch: true, baseBranch: opts.BaseBranch, includeManifest: opts.IncludeManifest})
	if err != nil {
		return WorkAcquisition{}, err
	}
	result := WorkAcquisition{Path: acquired.Path}
	err = SwitchOwnedBranch(repo, dir, acquired.Path, branch)
	return result, err
}

func sameExistingPath(a, b string) bool {
	aa, ea := filepath.EvalSymlinks(a)
	bb, eb := filepath.EvalSymlinks(b)
	return ea == nil && eb == nil && aa == bb
}

// ReclaimBranch makes no tree changes. HEAD and branch locks remain held until
// the owner reservation has been persisted under the containing pool lock.
func ReclaimBranch(repo, dir, path, branch string) (string, error) {
	var reclaimed string
	err := WithStateLock(dir, func() error {
		state, err := ReadState(dir)
		if err != nil {
			return err
		}
		state, err = healState(dir, state)
		if err != nil {
			return err
		}
		var entry *WorktreeEntry
		for i := range state.Worktrees {
			if sameExistingPath(state.Worktrees[i].Path, path) {
				entry = &state.Worktrees[i]
				break
			}
		}
		if entry == nil {
			return fmt.Errorf("branch %q is checked out outside this pool at %s", branch, path)
		}
		if entry.Leased {
			return fmt.Errorf("branch %q is in leased slot %s (held by %s); inspect treehouse status", branch, path, entry.LeaseHolder)
		}
		if entry.Destroying {
			return fmt.Errorf("slot %s is being destroyed", path)
		}
		if !entry.SeedInventoryKnown {
			return fmt.Errorf("slot %s is quarantined without a trusted seed inventory", path)
		}
		if ownerAlive(*entry) {
			return fmt.Errorf("slot %s has a live owner; treehouse enter opens another writable shell", path)
		}
		return vcs.WithGitBranchIdentity(repo, entry.Path, branch, func() error {
			facts, err := vcs.InspectGitBranch(repo, branch)
			if err != nil {
				return err
			}
			if len(facts.Holders) != 1 || !sameExistingPath(facts.Holders[0].Path, entry.Path) {
				return fmt.Errorf("branch %q registrations changed or are ambiguous", branch)
			}
			processes, err := findProcessesInWorktree(entry.Path)
			if err != nil {
				return fmt.Errorf("cannot inspect processes in %s: %w", path, err)
			}
			if len(processes) > 0 {
				return fmt.Errorf("slot %s has running processes; treehouse enter opens another writable shell", path)
			}
			if err := reserveOwner(entry); err != nil {
				return err
			}
			entry.LastBranch = ""
			if err := WriteState(dir, state); err != nil {
				return err
			}
			reclaimed = entry.Path
			return nil
		})
	})
	return reclaimed, err
}

// SwitchOwnedBranch switches a slot only while it is unleased and reserved by
// this caller, holding the pool lock through Git's checkout operation.
// On failure the caller must still perform guarded cleanup; no branch is deleted.
func SwitchOwnedBranch(repo, dir, path, branch string) error {
	return WithStateLock(dir, func() error {
		state, err := ReadState(dir)
		if err != nil {
			return err
		}
		_, err = releasableWorktree(&state, path, ReleasePreconditions{RequireOwnedByCaller: true})
		if err != nil {
			return err
		}
		return vcs.SwitchGitBranch(repo, path, branch)
	})
}
