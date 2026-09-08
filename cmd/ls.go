package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/kunchenguid/treehouse/internal/config"
	"github.com/kunchenguid/treehouse/internal/pool"
	"github.com/kunchenguid/treehouse/internal/vcs"
)

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List slots, branches, and next steps",
	RunE: func(cmd *cobra.Command, args []string) error {
		if statusAll || statusGlobal {
			return globalStatusWithTable(true)
		}
		repo, err := vcs.FindMainRepoRoot()
		if err != nil {
			return fmt.Errorf("not in a git or jj repository: %w", err)
		}
		cfg, err := config.Load(repo)
		if err != nil {
			return err
		}
		dir, err := config.ResolvePoolDir(repo, config.ResolveRoot(rootFlag, cfg))
		if err != nil {
			return err
		}
		slots, err := pool.ListSnapshot(dir)
		if err != nil {
			return err
		}
		if statusJSON {
			return writeStatusJSON(slots)
		}
		return writeLSTable([]pool.PoolSnapshot{{PoolDir: dir, Worktrees: slots}}, false)
	},
}

func init() {
	lsCmd.Flags().BoolVar(&statusJSON, "json", false, "Print the existing status JSON schema")
	lsCmd.Flags().BoolVar(&statusAll, "all", false, "Show every managed pool under the user-level treehouse root")
	lsCmd.Flags().BoolVar(&statusGlobal, "global", false, "Alias for --all")
	rootCmd.AddCommand(lsCmd)
}

func writeLSTable(pools []pool.PoolSnapshot, global bool) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "SLOT\tBRANCH\tSTATE\tNEXT")
	for _, p := range pools {
		for _, slot := range p.Worktrees {
			selector := slot.Name
			if global {
				selector = filepath.Base(p.PoolDir) + "/" + slot.Name
			}
			branch, state, next := lsRow(slot, p.PoolDir, selector, global)
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", selector, branch, state, next)
		}
	}
	return w.Flush()
}

func lsRow(slot pool.WorktreeStatus, poolDir, selector string, global bool) (branch, state, next string) {
	branch, state = "--", slot.Status
	command := "treehouse"
	if global || rootFlag != "" {
		command += " --root " + quoteReturnPath(filepath.Dir(filepath.Dir(poolDir)))
	}
	enter := command + " enter " + quoteReturnPath(selector) + " (writable shell)"
	acquire := command
	canWork := true
	if global {
		if repo, err := vcs.FindMainRepoRootFrom(slot.Path); err == nil {
			cd := "cd "
			if runtime.GOOS == "windows" {
				cd = "cd /d "
			}
			acquire = cd + quoteReturnPath(repo) + " && " + acquire
		} else {
			canWork = false
		}
	}
	next = enter
	if state == pool.StatusAvailable {
		state, next = "idle", acquire+" get"
		if !canWork {
			next = enter
		}
	}
	if slot.Status == pool.StatusLeased && slot.LeaseHolder != "" {
		state += ": " + slot.LeaseHolder
	}
	if slot.Flavor == "" {
		if slot.Status == pool.StatusLeased || slot.Status == pool.StatusInUse || slot.Status == pool.StatusHere {
			state += ", damaged"
		} else {
			state = "damaged"
		}
		return
	}
	if slot.Flavor != "git" {
		return
	}
	if canWork {
		repo, err := vcs.FindMainRepoRootFrom(slot.Path)
		canWork = err == nil && vcs.BackendNameFor(repo) == "git"
	}
	if slot.Status == pool.StatusAvailable {
		next = enter
		if canWork {
			next = acquire + " work <branch>"
		}
	}
	facts := vcs.InspectGitWorktree(slot.Path)
	if facts.Branch != "" {
		branch = facts.Branch
	} else if slot.LastBranch != "" {
		branch = slot.LastBranch + " (last used)"
	}
	if slot.Status != pool.StatusAvailable {
		return
	}
	// Existing status intentionally tolerates dirty-read failures. The richer
	// table must not turn that tolerance into a verified parked/idle claim.
	if dirty, err := vcs.IsDirty(slot.Path); err != nil {
		state, next = "unknown", enter
		return
	} else if dirty {
		state, next = "dirty", enter
		return
	}
	if !facts.IdentityKnown {
		state, next = "identity unknown", enter
		return
	}
	atBase, merged, known := vcs.InspectGitBase(slot.Path, slot.BaseBranch, facts.Head)
	if !known {
		state, next = "base unknown", enter
	} else if !merged {
		state, next = "unmerged", enter
	}
	if facts.Branch == "" {
		switch {
		case !facts.PreservationKnown:
			state, next = "detached, preservation unknown", enter
		case facts.PreservingRef == "":
			state = "detached, unpreserved"
			next = "git -C " + quoteReturnPath(slot.Path) + " branch <new-name> HEAD"
		case known && !merged:
			state = "detached, preserved, unmerged"
		}
		if slot.LastBranch != "" && known && merged {
			if atBase {
				state = "parked"
			}
			if canWork {
				next = acquire + " work " + quoteReturnPath(slot.LastBranch)
			}
		}
	}
	return
}
