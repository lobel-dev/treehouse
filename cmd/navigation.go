package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kunchenguid/treehouse/internal/config"
	"github.com/kunchenguid/treehouse/internal/pool"
	"github.com/kunchenguid/treehouse/internal/ui"
)

func globalNavigationRoot() (string, error) {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}
	return config.ResolvePoolRoot("", config.ResolveRoot(rootFlag, cfg))
}

func parseGlobalSelector(selector string) (string, string, error) {
	parts := strings.Split(selector, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid worktree selector %q: expected pool/name", selector)
	}
	for _, part := range parts {
		// Reject both platforms' separators, drive/stream syntax and path aliases,
		// even when the command is running on the other platform.
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "\\:\x00") || strings.TrimRight(part, " .") != part {
			return "", "", fmt.Errorf("invalid worktree selector %q: expected pool/name", selector)
		}
	}
	return parts[0], parts[1], nil
}

func globalStatus() error {
	root, err := globalNavigationRoot()
	if err != nil {
		return err
	}
	dirs, err := pool.NavigationPoolDirs(root)
	if err != nil {
		return err
	}
	output := make([]statusJSONWorktree, 0)
	for _, dir := range dirs {
		worktrees, err := pool.ListSnapshot(dir)
		if err != nil {
			return fmt.Errorf("pool %s: %w", filepath.Base(dir), err)
		}
		rows := statusJSONRows(worktrees)
		for i := range rows {
			rows[i].Pool = filepath.Base(dir)
			rows[i].Selector = rows[i].Pool + "/" + rows[i].Name
		}
		output = append(output, rows...)
	}
	if statusJSON {
		return json.NewEncoder(os.Stdout).Encode(output)
	}
	if len(output) == 0 {
		fmt.Fprintln(os.Stderr, "🌳 No worktrees in global pools.")
		return nil
	}
	for _, row := range output {
		fmt.Fprintf(os.Stdout, "%s  %-11s  %s", row.Selector, row.Status, ui.PrettyPath(row.Path))
		if row.LeaseHolder != "" {
			fmt.Fprintf(os.Stdout, "  (held by %s)", row.LeaseHolder)
		}
		fmt.Fprintln(os.Stdout)
	}
	return nil
}
