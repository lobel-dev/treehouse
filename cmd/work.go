package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kunchenguid/treehouse/internal/config"
	"github.com/kunchenguid/treehouse/internal/pool"
	"github.com/kunchenguid/treehouse/internal/shell"
	"github.com/kunchenguid/treehouse/internal/vcs"
	"github.com/spf13/cobra"
)

type unsupportedBranchBackendError struct{ command string }

func (e unsupportedBranchBackendError) Error() string {
	if e.command == "work" {
		return "work requires the Git backend; use treehouse get for this repository"
	}
	return e.command + " requires the Git backend; inspect treehouse status and use the existing explicit lifecycle commands"
}

// ExitCode preserves existing command failures while distinguishing unsupported
// branch lifecycle operations from ordinary refusals.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var unsupported unsupportedBranchBackendError
	if errors.As(err, &unsupported) {
		return 2
	}
	return 1
}

var workCmd = &cobra.Command{
	Use:   "work [branch]",
	Short: "Resume or create a Git branch in a pooled worktree",
	Long: `Resume or create a literal Git branch, including slash and numeric names.
An eligible idle slot already holding the branch is reserved without changing
its files. Otherwise acquire a slot and switch to the local branch, track the
origin branch, or create it from the requested base. Existing dirty changes
still require confirmation at exit. With no branch, delegate to get.`,
	Args: cobra.MaximumNArgs(1),
	RunE: workRunE,
}

func init() {
	workCmd.Flags().BoolVar(&getNoFetch, "no-fetch", false, "Skip fetching origin before acquiring")
	workCmd.Flags().StringVar(&getBase, "base", "", "Base for a newly acquired slot (existing branch slots keep their base)")
	workCmd.Flags().StringVar(&getIncludeFile, "include-file", "", "Replace committed .worktreeinclude for normal acquisition")
	rootCmd.AddCommand(workCmd)
}

func workRunE(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return getRunE(cmd, args)
	}
	repo, err := vcs.FindMainRepoRoot()
	if err != nil {
		return err
	}
	if vcs.BackendNameFor(repo) != "git" {
		return unsupportedBranchBackendError{command: "work"}
	}
	branch := args[0]
	if err := vcs.ValidateGitBranch(repo, branch); err != nil {
		return err
	}
	cfg, err := config.Load(repo)
	if err != nil {
		return err
	}
	dir, err := config.ResolvePoolDir(repo, config.ResolveRoot(rootFlag, cfg))
	if err != nil {
		return err
	}
	var manifest []byte
	if cmd.Flags().Changed("include-file") {
		manifest, err = os.ReadFile(getIncludeFile)
		if err != nil {
			return fmt.Errorf("failed to read include file %q: %w", getIncludeFile, err)
		}
	}
	if err := config.EnsureExcluded(filepath.Dir(dir)); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to update git exclude: %v\n", err)
	}
	acquired, err := pool.AcquireWorkBranch(repo, dir, branch, cfg.MaxTrees, cfg.Hooks.PostCreate, pool.AcquireOptions{
		SkipFetch: getNoFetch, BaseBranch: resolveRequestedBase(cfg), IncludeManifest: manifest,
	})
	if err == nil {
		if acquired.Reclaimed {
			fmt.Fprintln(os.Stderr, "Reserved the existing branch slot; existing changes remain in place.")
		}
		fmt.Fprintf(os.Stderr, "🌳 Working on %s at %s. Type 'exit' to return.\n", branch, acquired.Path)
		_, err = shell.Spawn(acquired.Path, []string{"TREEHOUSE_DIR=" + acquired.Path})
	}
	if acquired.Path == "" {
		return err
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "work failed after allocating %s: %v; attempting guarded return.\n", acquired.Path, err)
	}
	releaseErr := finishAcquiredWorktree(repo, dir, acquired.Path, cfg)
	if err != nil {
		if releaseErr != nil {
			return fmt.Errorf("work failed at %s: %w; guarded return also failed: %v", acquired.Path, err, releaseErr)
		}
		return err
	}
	return releaseErr
}
