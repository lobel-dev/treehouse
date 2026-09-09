package cmd

import (
	"fmt"
	"os"

	"github.com/kunchenguid/treehouse/internal/config"
	"github.com/kunchenguid/treehouse/internal/pool"
	"github.com/kunchenguid/treehouse/internal/ui"
	"github.com/kunchenguid/treehouse/internal/vcs"
)

func interactivePrune() error {
	if ui.DashboardSupported() {
		return runWorkspace(ui.CleanupPage)
	}
	run, cfg, err := interactivePruneRunner()
	if err != nil {
		return err
	}
	opts := pool.PruneOptions{DryRun: true, ReadOnlySnapshot: true, PruneOrphans: pruneOrphans, PreDestroy: cfg.Hooks.PreDestroy}
	preview, err := run(opts)
	if err != nil {
		return err
	}
	for _, skip := range preview.Skipped {
		fmt.Fprintf(os.Stderr, "Keeping tree %s — %s.\n", skip.Name, skip.Reason)
	}
	if len(preview.Candidates) == 0 {
		fmt.Fprintln(os.Stderr, "Nothing to clean up. Your work is kept.")
		return nil
	}
	fmt.Fprintln(os.Stderr, "\nUnused trees ready to remove:")
	paths := make([]string, 0, len(preview.Candidates))
	hasGitTree := false
	for _, tree := range preview.Candidates {
		fmt.Fprintf(os.Stderr, "  Tree %s · %s\n    %s\n", tree.Name, pruneBranchLabel(tree), ui.PrettyPath(tree.Path))
		hasGitTree = hasGitTree || vcs.WorktreeBackendName(tree.Path) == "git"
		if tree.Orphaned {
			fmt.Fprintln(os.Stderr, "    Missing repository: contents could not be verified.")
		}
		paths = append(paths, tree.Path)
	}
	if hasGitTree {
		fmt.Fprintln(os.Stderr, "Git branches are kept.")
	}
	fmt.Fprintf(os.Stderr, "Reclaimable space: %s\n", formatBytes(preview.ReclaimableBytes))
	ok, err := ui.Confirm("Remove the listed trees?", false)
	if err != nil || !ok {
		fmt.Fprintln(os.Stderr, "Canceled. Nothing removed.")
		return nil
	}
	opts.DryRun = false
	opts.CandidatePaths = paths
	result, err := run(opts)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Removed %d unused trees.\n", len(result.Pruned))
	if len(result.Pruned) < len(paths) {
		fmt.Fprintln(os.Stderr, "Trees that could not be safely removed were kept.")
	}
	for _, skip := range result.Skipped {
		fmt.Fprintf(os.Stderr, "Kept tree %s — %s.\n", skip.Name, skip.Reason)
	}
	return nil
}

// Reporting is advisory. The prune engine independently rechecks deletion
// safety; neither a current branch label nor history authorizes removal.
func pruneBranchLabel(tree pool.PruneWorktree) string {
	if vcs.WorktreeBackendName(tree.Path) == "jj" {
		return "jj workspace"
	}
	facts := vcs.InspectGitWorktree(tree.Path)
	if facts.Branch != "" {
		return facts.Branch
	}
	if tree.LastBranch != "" {
		return tree.LastBranch + " (last used)"
	}
	if !facts.IdentityKnown {
		return "Branch unavailable"
	}
	return "No branch checked out; no branch history"
}

func interactivePruneRunner() (func(pool.PruneOptions) (pool.PruneResult, error), config.Config, error) {
	var run func(pool.PruneOptions) (pool.PruneResult, error)
	var cfg config.Config
	var err error
	if pruneAll || pruneGlobal {
		cfg, err = config.LoadGlobal()
		if err != nil {
			return nil, cfg, err
		}
		root, err := config.ResolvePoolRoot("", config.ResolveRoot(rootFlag, cfg))
		if err != nil {
			return nil, cfg, err
		}
		run = func(opts pool.PruneOptions) (pool.PruneResult, error) {
			r, err := pool.PruneAllWithOptions(root, opts)
			return r.Result, err
		}
	} else {
		repo, err := vcs.FindMainRepoRoot()
		if err != nil {
			return nil, cfg, err
		}
		cfg, err = config.Load(repo)
		if err != nil {
			return nil, cfg, err
		}
		dir, err := config.ResolvePoolDir(repo, config.ResolveRoot(rootFlag, cfg))
		if err != nil {
			return nil, cfg, err
		}
		run = func(opts pool.PruneOptions) (pool.PruneResult, error) { return pool.PruneWithOptions(repo, dir, opts) }
	}
	return run, cfg, nil
}
