package cmd

import (
	"fmt"
	"os"
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
			// No input reader survives into hooks or mutation; the engine
			// revalidates the exact displayed paths.
			text, runErr, done := applyWorkspaceCleanup(&options, result, len(action.Paths), err)
			if text != "" {
				fmt.Fprint(os.Stderr, text)
				if !strings.HasSuffix(text, "\n") {
					fmt.Fprintln(os.Stderr)
				}
			}
			if done {
				return runErr
			}
		}
	}
}

func applyWorkspaceCleanup(options *ui.DashboardOptions, result pool.PruneResult, requested int, runErr error) (stderr string, err error, done bool) {
	text := cleanupResultText(result, requested, runErr)
	if options.Page != ui.HomePage {
		if runErr != nil {
			return "", fmt.Errorf("%s", strings.TrimSpace(text)), true
		}
		return text, nil, true
	}
	options.Banner = firstLine(text)
	options.BannerWarning = runErr != nil
	options.Page = ui.HomePage
	return text, nil, false
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func workspaceTreeTitle(slot pool.WorktreeStatus) (title, extra string) {
	if slot.Flavor == "git" {
		facts := vcs.InspectGitWorktree(slot.Path)
		if facts.Branch != "" {
			return facts.Branch, "(current)"
		}
		if slot.LastBranch != "" {
			return slot.LastBranch, "(last used)"
		}
		if facts.IdentityKnown {
			return "No branch checked out", ""
		}
		return "Branch unavailable", ""
	}
	if slot.LastBranch != "" {
		return slot.LastBranch, "(last used)"
	}
	return slot.Name, ""
}

func workspaceTreeRow(slot pool.WorktreeStatus) ui.DashboardRow {
	title, extra := workspaceTreeTitle(slot)
	var b strings.Builder
	if extra != "" {
		b.WriteString(extra + "\n")
	}
	fmt.Fprintf(&b, "tree %s · %s\n", slot.Name, slot.Status)
	b.WriteString(ui.PrettyPath(slot.Path) + "\n")
	if slot.LeaseHolder != "" {
		b.WriteString("leased by " + slot.LeaseHolder + "\n")
	}
	for _, p := range slot.Processes {
		b.WriteString(p.String() + "\n")
	}
	b.WriteString("Enter opens this tree; files stay as you leave them.")
	return ui.DashboardRow{
		ID:         slot.Path,
		Title:      title,
		Annotation: "tree " + slot.Name,
		Status:     slot.Status,
		Details:    b.String(),
		Action:     ui.Action{Kind: ui.OpenTree, Target: slot.Path, Name: slot.Name},
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
		row := workspaceTreeRow(slot)
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
		_, eligible := workBranchLabel(branch, slots)
		if !eligible {
			continue
		}
		details := "No tree assigned. One will be prepared when you open this branch.\nEnter resumes this branch. Unfinished changes stay on exit."
		status := "ready"
		annotation := ""
		if len(branch.Holders) == 0 {
			for _, slot := range slots {
				if slot.LastBranch == branch.Name {
					details = "Last used in tree " + slot.Name + " (history only)\nPath: " + ui.PrettyPath(slot.Path) + "\nEnter resumes the branch in an available tree."
					break
				}
			}
		}
		for _, holder := range branch.Holders {
			for _, slot := range slots {
				if sameMenuPath(slot.Path, holder.Path) {
					details = strings.ReplaceAll(treeRows[slot.Path].Details, "Enter opens this tree; files stay as you leave them.", "Enter resumes this branch. Unfinished changes stay on exit.")
					status = slot.Status
					annotation = "tree " + slot.Name
				}
			}
		}
		s.Rows = append(s.Rows, ui.DashboardRow{ID: branch.Name, Title: branch.Name, Annotation: annotation, Status: status, Details: details, Action: ui.Action{Kind: ui.ResumeBranch, Target: branch.Name}})
	}
	return s, nil
}

func workspaceCleanupTitle(tree pool.PruneWorktree) string {
	label := pruneBranchLabel(tree)
	if label == "jj workspace" {
		if tree.LastBranch != "" {
			return tree.LastBranch
		}
		return tree.Name
	}
	return label
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
		s.Rows = append(s.Rows, ui.DashboardRow{ID: tree.Path, Title: workspaceCleanupTitle(tree), Annotation: "tree " + tree.Name, Status: "removable", Details: strings.TrimSpace(ui.PrettyPath(tree.Path) + "\n" + formatBytes(tree.Bytes) + "\n" + warning)})
		s.CandidatePaths = append(s.CandidatePaths, tree.Path)
	}
	for _, skip := range result.Skipped {
		s.Rows = append(s.Rows, ui.DashboardRow{ID: skip.Path, Title: skip.Name, Annotation: "tree " + skip.Name, Status: "protected", Details: ui.PrettyPath(skip.Path) + "\nKeeping: " + skip.Reason})
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
