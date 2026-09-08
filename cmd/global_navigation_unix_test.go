//go:build !windows

package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// A colon is legal in a Unix directory name, so a repository such as "proj:v2"
// produces the pool "proj:v2-<hash>". Global status advertises that selector as
// copyable, so qualified enter must accept the very selector the listing
// printed; refusing it makes the project unreachable through the documented
// navigation path. Traversal and separator forms must still be refused.
func TestQualifiedSelectorRoundTripsColonRepoName(t *testing.T) {
	home, root, outside := t.TempDir(), t.TempDir(), t.TempDir()
	repo := setupTestRepoWithHome(t, home, "proj:v2")

	out, errOut, code := runTreehouse(t, repo, home, nil, "--root", root, "get", "--lease", "--json")
	if code != 0 {
		t.Fatalf("get: %s", errOut)
	}
	var lease leaseJSONResult
	if err := json.Unmarshal([]byte(out), &lease); err != nil {
		t.Fatal(err)
	}
	poolName := filepath.Base(filepath.Dir(filepath.Dir(lease.Path)))
	if !strings.Contains(poolName, ":") {
		t.Fatalf("expected a colon in the pool directory name, got %q", poolName)
	}

	out, errOut, code = runTreehouse(t, outside, home, nil, "--root", root, "status", "--all", "--json")
	if code != 0 {
		t.Fatalf("global status: %s", errOut)
	}
	var rows []struct{ Selector, Path string }
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows: %s", out)
	}
	advertised := rows[0].Selector
	if advertised != poolName+"/1" {
		t.Fatalf("selector %q does not name the pool directory %q", advertised, poolName)
	}

	// The exact selector the listing printed must select that exact slot.
	out, errOut, code = runTreehouse(t, outside, home, nil, "--root", root, "enter", "--print-path", advertised)
	if code != 0 {
		t.Fatalf("enter %q: %s", advertised, errOut)
	}
	if strings.TrimSpace(out) != lease.Path {
		t.Fatalf("enter %q selected %q, want %q", advertised, strings.TrimSpace(out), lease.Path)
	}

	// Relaxing the colon must not relax traversal or separator handling.
	for _, selector := range []string{
		"../1", poolName + "/../1", "/" + poolName + "/1", poolName + `\1`,
		poolName + "/", `C:\pool\1`, poolName + "/1/2",
	} {
		if _, _, code := runTreehouse(t, outside, home, nil, "--root", root, "enter", "--print-path", selector); code == 0 {
			t.Errorf("accepted unsafe selector %q", selector)
		}
	}
}
