package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kunchenguid/treehouse/internal/pool"
	"github.com/kunchenguid/treehouse/internal/ui"
	"github.com/kunchenguid/treehouse/internal/vcs"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "menu",
		Short: "Open the interactive Treehouse home screen",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return interactiveHome() },
	})
}

func interactiveHome() error {
	repo, err := vcs.FindMainRepoRoot()
	if err != nil {
		return fmt.Errorf("open Treehouse from a project repository: %w", err)
	}
	for {
		choices := []string{"Continue working on a branch", "Start a new branch", "Open an existing tree", "Clean up unused trees"}
		if vcs.BackendNameFor(repo) != "git" {
			choices = []string{"Start working in a tree", "Open an existing tree", "Clean up unused trees"}
		}
		fmt.Fprintln(os.Stderr, "\nChoose work below. Type exit in the tree to return to your terminal.")
		selected, err := ui.Choose("Treehouse · "+filepath.Base(repo), choices, "Quit")
		if err != nil || selected < 0 {
			return err
		}
		if vcs.BackendNameFor(repo) != "git" && selected > 0 {
			selected++
		}
		var finished bool
		switch selected {
		case 0:
			if vcs.BackendNameFor(repo) == "git" {
				finished, err = chooseWorkBranch(repo)
			} else {
				finished, err = runHomeWork("get")
			}
		case 1:
			finished, err = startBranch(repo)
		case 2:
			finished, err = chooseTree()
		case 3:
			err = interactivePrune()
		}
		if finished {
			return err
		}
		if err != nil && !errors.Is(err, errReturnAborted) && !errors.Is(err, errReturnAbortedNonTTY) {
			fmt.Fprintf(os.Stderr, "\n%s\n", err)
		}
	}
}

func startBranch(repo string) (bool, error) {
	for {
		name, err := ui.ReadLine("Branch name (Enter to go back): ")
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return false, nil
		}
		if err := vcs.ValidateGitBranch(repo, name); err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}
		return runHomeWork("work", name)
	}
}

func chooseWorkBranch(repo string) (bool, error) {
	branches, err := vcs.ListGitWorkBranches(repo)
	if err != nil {
		return false, err
	}
	_, slots, err := currentPoolSnapshot()
	if err != nil {
		return false, err
	}
	var names, labels []string
	for _, branch := range branches {
		facts, err := vcs.InspectGitBranch(repo, branch)
		if err != nil {
			return false, err
		}
		label := branch
		if len(facts.Holders) > 0 {
			// A checkout outside this pool cannot be resumed here. Protected
			// pool trees remain accessible through the existing-tree picker.
			eligible := len(facts.Holders) == 1
			found := false
			for _, slot := range slots {
				if len(facts.Holders) == 1 && sameMenuPath(slot.Path, facts.Holders[0].Path) {
					found = true
					eligible = eligible && (slot.Status == pool.StatusAvailable || slot.Status == pool.StatusDirty)
					label += " · tree " + slot.Name
					if slot.Status == pool.StatusDirty {
						label += " · uncommitted changes"
					}
				}
			}
			if !eligible || !found {
				continue
			}
		}
		names = append(names, branch)
		labels = append(labels, label)
	}
	if len(names) == 0 {
		fmt.Fprintln(os.Stderr, "No available branches to resume. Start a new branch or open an existing tree.")
		return false, nil
	}
	i, err := ui.Choose("Continue working", labels, "Back")
	if err != nil || i < 0 {
		return false, err
	}
	return runHomeWork("work", names[i])
}

func sameMenuPath(a, b string) bool {
	aa, ea := os.Stat(a)
	bb, eb := os.Stat(b)
	return ea == nil && eb == nil && os.SameFile(aa, bb)
}

func chooseTree() (bool, error) {
	_, slots, err := currentPoolSnapshot()
	if err != nil {
		return false, err
	}
	if len(slots) == 0 {
		fmt.Fprintln(os.Stderr, "No trees yet. Start a new branch from the home screen.")
		return false, nil
	}
	labels := make([]string, len(slots))
	for i, slot := range slots {
		label := "Tree " + slot.Name
		facts := vcs.InspectGitWorktree(slot.Path)
		if facts.Branch != "" {
			label += " · " + facts.Branch
		} else if slot.LastBranch != "" {
			label += " · " + slot.LastBranch + " (last used)"
		}
		label += " · " + slot.Status
		if slot.LeaseHolder != "" {
			label += " by " + slot.LeaseHolder
		}
		labels[i] = label
	}
	i, err := ui.Choose("Open a tree - exit returns to your terminal; files stay as you leave them", labels, "Back")
	if err != nil || i < 0 {
		return false, err
	}
	return true, enterWorktree(&slots[i])
}

// Each acquired shell has its own lifecycle process. The boolean reports
// that the command finished, so the home screen exits with it. Picker
// cancellations return false and stay in the menu.
func runHomeWork(args ...string) (bool, error) {
	executable, err := os.Executable()
	if err != nil {
		return false, err
	}
	if rootFlag != "" {
		args = append([]string{"--root", rootFlag}, args...)
	}
	child := exec.Command(executable, args...)
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		return false, err
	}
	err = child.Wait()
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		return true, nil
	} // The command already explained it.
	return true, err
}
