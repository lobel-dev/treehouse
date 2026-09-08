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
	var run func(pool.PruneOptions) (pool.PruneResult, error)
	var cfg config.Config
	var err error
	if pruneAll || pruneGlobal {
		cfg, err = config.LoadGlobal()
		if err != nil {
			return err
		}
		root, err := config.ResolvePoolRoot("", config.ResolveRoot(rootFlag, cfg))
		if err != nil {
			return err
		}
		run = func(opts pool.PruneOptions) (pool.PruneResult, error) {
			r, err := pool.PruneAllWithOptions(root, opts)
			return r.Result, err
		}
	} else {
		repo, err := vcs.FindMainRepoRoot()
		if err != nil {
			return err
		}
		cfg, err = config.Load(repo)
		if err != nil {
			return err
		}
		dir, err := config.ResolvePoolDir(repo, config.ResolveRoot(rootFlag, cfg))
		if err != nil {
			return err
		}
		run = func(opts pool.PruneOptions) (pool.PruneResult, error) { return pool.PruneWithOptions(repo, dir, opts) }
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
	for _, tree := range preview.Candidates {
		fmt.Fprintf(os.Stderr, "  Tree %s · %s\n", tree.Name, ui.PrettyPath(tree.Path))
		if tree.Orphaned {
			fmt.Fprintln(os.Stderr, "    Missing repository: contents could not be verified.")
		}
		paths = append(paths, tree.Path)
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
