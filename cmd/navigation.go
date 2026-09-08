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
		// Reject traversal, the other platform's separator and path aliases. The
		// component set is platform-correct so a listing never prints a selector
		// its own enter refuses; see invalidSelectorChars.
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, invalidSelectorChars) || strings.TrimRight(part, " .") != part {
			return "", "", fmt.Errorf("invalid worktree selector %q: expected pool/name", selector)
		}
	}
	return parts[0], parts[1], nil
}

func globalStatus() error { return globalStatusWithTable(false) }

func globalStatusWithTable(table bool) error {
	root, err := globalNavigationRoot()
	if err != nil {
		return err
	}
	pools, unreadable, err := pool.ListSnapshotAll(root)
	if err != nil {
		return err
	}

	output := make([]statusJSONWorktree, 0)
	for _, p := range pools {
		rows := statusJSONRows(p.Worktrees)
		for i := range rows {
			rows[i].Pool = filepath.Base(p.PoolDir)
			rows[i].Selector = rows[i].Pool + "/" + rows[i].Name
		}
		output = append(output, rows...)
	}

	// The healthy pools are emitted first and in full - a valid top-level JSON
	// array even when the listing is partial - so one unreadable pool never
	// costs the user navigation to every other project.
	if statusJSON {
		if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
			return err
		}
	} else if table {
		if err := writeLSTable(pools, true); err != nil {
			return err
		}
	} else if len(output) > 0 {
		for _, row := range output {
			fmt.Fprintf(os.Stdout, "%s  %-11s  %s", row.Selector, row.Status, ui.PrettyPath(row.Path))
			if row.LeaseHolder != "" {
				fmt.Fprintf(os.Stdout, "  (held by %s)", row.LeaseHolder)
			}
			fmt.Fprintln(os.Stdout)
		}
	} else if len(unreadable) == 0 {
		fmt.Fprintln(os.Stderr, "\U0001f333 No worktrees in global pools.")
	}

	if len(unreadable) == 0 {
		return nil
	}
	for _, failure := range unreadable {
		fmt.Fprintf(os.Stderr, "\U0001f333 Cannot read pool %s: %v\n", ui.PrettyPath(failure.PoolDir), failure.Err)
	}
	// Nonzero signals that the listing above is incomplete.
	return fmt.Errorf("%d of %d pools could not be read", len(unreadable), len(pools)+len(unreadable))
}
