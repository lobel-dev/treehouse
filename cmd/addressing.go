package cmd

import (
	"fmt"
	"os"

	"github.com/kunchenguid/treehouse/internal/config"
	"github.com/kunchenguid/treehouse/internal/pool"
	"github.com/kunchenguid/treehouse/internal/vcs"
)

func currentPoolSnapshot() (repo string, slots []pool.WorktreeStatus, err error) {
	repo, err = vcs.FindMainRepoRoot()
	if err != nil {
		return
	}
	cfg, err := config.Load(repo)
	if err != nil {
		return
	}
	dir, err := config.ResolvePoolDir(repo, config.ResolveRoot(rootFlag, cfg))
	if err != nil {
		return
	}
	slots, err = pool.ListSnapshot(dir)
	return
}

// Explicit slot addressing reads membership only; it never consults history or
// falls back to a relative path, and all lifecycle checks still run afterward.
func resolveCurrentSlot(name string) (string, error) {
	if _, _, err := parseGlobalSelector("pool/" + name); err != nil {
		return "", fmt.Errorf("invalid slot name %q", name)
	}
	_, slots, err := currentPoolSnapshot()
	if err != nil {
		return "", err
	}
	path := ""
	for _, slot := range slots {
		if slot.Name == name {
			if path != "" {
				return "", fmt.Errorf("ambiguous slot name %q", name)
			}
			path = slot.Path
		}
	}
	if path == "" {
		return "", fmt.Errorf("no worktree named %q in the current pool; inspect treehouse status", name)
	}
	return path, nil
}

func resolveCurrentBranchSlot(branch string) (*pool.WorktreeStatus, error) {
	repo, slots, err := currentPoolSnapshot()
	if err != nil {
		return nil, err
	}
	facts, err := vcs.InspectGitBranch(repo, branch)
	if err != nil {
		return nil, err
	}
	// Entry is read-only: ignore proven-missing checkouts without pruning
	// registrations. InspectGitBranch fails closed on other stat errors.
	holders := facts.Holders[:0]
	for _, holder := range facts.Holders {
		if !holder.Missing {
			holders = append(holders, holder)
		}
	}
	if len(holders) == 0 {
		return nil, fmt.Errorf("branch %q is not checked out in this pool; resume with treehouse work %s", branch, quoteReturnPath(branch))
	}
	if len(holders) != 1 {
		return nil, fmt.Errorf("branch %q has ambiguous worktree registrations; inspect git worktree list", branch)
	}
	holder := holders[0]
	info, err := os.Stat(holder.Path)
	if err != nil {
		return nil, fmt.Errorf("cannot inspect branch checkout %s: %w", holder.Path, err)
	}
	var target *pool.WorktreeStatus
	for i := range slots {
		other, err := os.Stat(slots[i].Path)
		if err == nil && os.SameFile(info, other) {
			if target != nil {
				return nil, fmt.Errorf("branch %q has ambiguous pool membership", branch)
			}
			target = &slots[i]
		}
	}
	if target == nil {
		return nil, fmt.Errorf("branch %q is checked out outside the current pool at %s", branch, holder.Path)
	}
	if err := vcs.VerifyGitSlotRepository(repo, target.Path); err != nil {
		return nil, err
	}
	identity := vcs.InspectGitWorktree(target.Path)
	if !identity.IdentityKnown || identity.Branch != branch {
		return nil, fmt.Errorf("branch identity at %s changed or cannot be verified", target.Path)
	}
	return target, nil
}
