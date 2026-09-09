package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kunchenguid/treehouse/internal/pool"
	"github.com/kunchenguid/treehouse/internal/ui"
	"github.com/kunchenguid/treehouse/internal/vcs"
)

func runWorkspace(page ui.Page) error {
	repo, err := vcs.FindMainRepoRoot()
	if err != nil && !(page == ui.CleanupPage && (pruneAll || pruneGlobal)) {
		return fmt.Errorf("open Treehouse from a project repository: %w", err)
	}
	name := filepath.Base(repo)
	if repo == "" {
		name = "All pools"
	}
	options := ui.DashboardOptions{Repository: name, Page: page, Selection: map[ui.Page]string{}, JJ: repo != "" && vcs.BackendNameFor(repo) != "git"}
	options.ValidateBranch = func(name string) error { return vcs.ValidateGitBranch(repo, name) }
	options.Load = func(page ui.Page) (ui.DashboardSnapshot, error) {
		if page == ui.CleanupPage {
			return workspaceCleanupSnapshot()
		}
		return workspaceSnapshot(repo, page, options.JJ)
	}
	for {
		action, err := ui.RunDashboard(options)
		if err != nil {
			return err
		}
		switch action.Kind {
		case ui.NoAction:
			return nil
		case ui.ResumeBranch, ui.CreateBranch:
			_, err = runHomeWork("work", action.Target)
			return err
		case ui.StartTree:
			_, err = runHomeWork("get")
			return err
		case ui.OpenTree:
			return enterWorktree(&pool.WorktreeStatus{Path: action.Target, Name: action.Name})
		case ui.RemoveTrees:
			run, cfg, err := interactivePruneRunner()
			var result pool.PruneResult
			if err == nil {
				result, err = run(pool.PruneOptions{CandidatePaths: action.Paths, PruneOrphans: pruneOrphans, PreDestroy: cfg.Hooks.PreDestroy})
			}
			// A result is shown before returning home. No input reader survives into
			// hooks or mutation; the engine revalidates the exact displayed paths.
			options.Result = cleanupResultText(result, len(action.Paths), err)
		}
	}
}

func workspaceSnapshot(repo string, page ui.Page, jj bool) (ui.DashboardSnapshot, error) {
	_, slots, err := currentPoolSnapshot()
	if err != nil {
		return ui.DashboardSnapshot{}, err
	}
	available, leased := 0, 0
	for _, slot := range slots {
		if slot.Status == pool.StatusAvailable {
			available++
		}
		if slot.Status == pool.StatusLeased {
			leased++
		}
	}
	treeWord := "trees"
	if len(slots) == 1 {
		treeWord = "tree"
	}
	s := ui.DashboardSnapshot{Summary: fmt.Sprintf("%d %s · %d available · %d leased", len(slots), treeWord, available, leased)}
	treeRows := make(map[string]ui.DashboardRow, len(slots))
	for _, slot := range slots {
		branch := "Branch unavailable"
		if slot.Flavor == "git" {
			facts := vcs.InspectGitWorktree(slot.Path)
			if facts.IdentityKnown {
				branch = "No branch checked out"
			}
			if facts.Branch != "" {
				branch = facts.Branch + " (current)"
			} else if slot.LastBranch != "" {
				branch = slot.LastBranch + " (last used)"
			}
		} else if slot.Flavor == "jj" {
			branch = "jj workspace"
		}
		details := fmt.Sprintf("%s\nTree %s · %s · %s\nPath: %s\n", branch, slot.Name, slot.Status, slot.Flavor, ui.PrettyPath(slot.Path))
		if slot.LeaseHolder != "" {
			details += "Lease holder: " + slot.LeaseHolder + "\n"
		}
		for _, p := range slot.Processes {
			details += "Process: " + p.String() + "\n"
		}
		details += "Enter opens this tree; files stay as you leave them."
		row := ui.DashboardRow{ID: slot.Path, Label: branch + " · tree " + slot.Name, Status: slot.Status, Details: details, Action: ui.Action{Kind: ui.OpenTree, Target: slot.Path, Name: slot.Name}}
		treeRows[slot.Path] = row
		if page == ui.TreesPage || jj {
			s.Rows = append(s.Rows, row)
		}
	}
	if page == ui.TreesPage || jj {
		return s, nil
	}
	branches, err := vcs.ListGitWorkBranchStates(repo)
	if err != nil {
		return s, err
	}
	for _, branch := range branches {
		label, eligible := workBranchLabel(branch, slots)
		if !eligible {
			continue
		}
		details := branch.Name + "\nNo tree assigned. One will be prepared when you open this branch."
		status := "ready"
		if len(branch.Holders) == 0 {
			for _, slot := range slots {
				if slot.LastBranch == branch.Name {
					details = branch.Name + "\nLast used in tree " + slot.Name + " (history only)\nPath: " + slot.Path + "\nEnter resumes the branch in an available tree."
					break
				}
			}
		}
		for _, holder := range branch.Holders {
			for _, slot := range slots {
				if sameMenuPath(slot.Path, holder.Path) {
					details = strings.ReplaceAll(treeRows[slot.Path].Details, "Enter opens this tree; files stay as you leave them.", "Enter resumes work; unfinished changes are kept by default on exit.")
					status = slot.Status
				}
			}
		}
		s.Rows = append(s.Rows, ui.DashboardRow{ID: branch.Name, Label: label, Status: status, Details: details, Action: ui.Action{Kind: ui.ResumeBranch, Target: branch.Name}})
	}
	return s, nil
}

func workspaceCleanupSnapshot() (ui.DashboardSnapshot, error) {
	run, cfg, err := interactivePruneRunner()
	if err != nil {
		return ui.DashboardSnapshot{}, err
	}
	result, err := run(pool.PruneOptions{DryRun: true, ReadOnlySnapshot: true, PruneOrphans: pruneOrphans, PreDestroy: cfg.Hooks.PreDestroy})
	if err != nil {
		return ui.DashboardSnapshot{}, err
	}
	s := ui.DashboardSnapshot{Summary: fmt.Sprintf("%d removable · %d protected · %s reclaimable", len(result.Candidates), len(result.Skipped), formatBytes(result.ReclaimableBytes)), Notice: "Remove the listed trees? Git branches are kept."}
	for _, tree := range result.Candidates {
		warning := tree.Warning
		if tree.Orphaned {
			warning = "Missing repository: contents could not be verified."
			if !strings.HasPrefix(s.Notice, "WARNING:") {
				s.Notice = "WARNING: orphan contents could not be verified.\n" + s.Notice
			}
		}
		s.Rows = append(s.Rows, ui.DashboardRow{ID: tree.Path, Label: "Tree " + tree.Name + " · " + pruneBranchLabel(tree), Status: "removable", Details: strings.TrimSpace(pruneBranchLabel(tree) + "\n" + ui.PrettyPath(tree.Path) + "\n" + formatBytes(tree.Bytes) + "\n" + warning)})
		s.CandidatePaths = append(s.CandidatePaths, tree.Path)
	}
	for _, skip := range result.Skipped {
		s.Rows = append(s.Rows, ui.DashboardRow{ID: skip.Path, Label: "Tree " + skip.Name, Status: "protected", Details: ui.PrettyPath(skip.Path) + "\nKeeping: " + skip.Reason})
	}
	return s, nil
}

func cleanupResultText(result pool.PruneResult, requested int, err error) string {
	if err != nil {
		return "Cleanup failed: " + err.Error()
	}
	text := fmt.Sprintf("Removed %d unused trees.\n", len(result.Pruned))
	if len(result.Pruned) < requested {
		text += "Trees that could not be safely removed were kept.\n"
	}
	for _, skip := range result.Skipped {
		text += fmt.Sprintf("Kept tree %s — %s.\n", skip.Name, skip.Reason)
	}
	return text
}
