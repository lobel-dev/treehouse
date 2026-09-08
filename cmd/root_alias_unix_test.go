//go:build !windows

package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A root spelled through a symlink (macOS's /tmp -> /private/tmp is the everyday
// case) addresses the very same pool directory as the canonical spelling. The
// state file records the canonical path git reports, so a purely textual
// comparison declares every registered slot missing and fabricates a duplicate
// "recovered" lease for it: status lists one slot twice, and the next writing
// command persists the phantom into pool state.
func TestAliasedRootDoesNotFabricateRecoveredSlots(t *testing.T) {
	home, outside := t.TempDir(), t.TempDir()
	repo := setupTestRepoWithHome(t, home, "aliased")

	realRoot := t.TempDir()
	realRoot, err := filepath.EvalSymlinks(realRoot)
	if err != nil {
		t.Fatal(err)
	}
	aliasRoot := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	out, errOut, code := runTreehouse(t, repo, home, nil, "--root", realRoot, "get", "--lease", "--json")
	if code != 0 {
		t.Fatalf("get: %s", errOut)
	}
	var lease leaseJSONResult
	if err := json.Unmarshal([]byte(out), &lease); err != nil {
		t.Fatal(err)
	}
	poolDir := filepath.Dir(filepath.Dir(lease.Path))
	statePath := filepath.Join(poolDir, "treehouse-state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	// Listing through the aliased root must report the one real slot, and must
	// not write pool state.
	out, errOut, code = runTreehouse(t, outside, home, nil, "--root", aliasRoot, "status", "--all", "--json")
	if code != 0 {
		t.Fatalf("global status: %s", errOut)
	}
	var rows []struct{ Selector, Path, Status, LeaseHolder string }
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("aliased root listed %d slots, want 1: %s", len(rows), out)
	}
	if strings.Contains(rows[0].LeaseHolder, "recovered") {
		t.Fatalf("aliased root fabricated a recovered slot: %+v", rows[0])
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("global status wrote pool state:\nbefore %s\nafter  %s", before, after)
	}

	// A writing command through the aliased root must not append a duplicate.
	if _, errOut, code = runTreehouse(t, outside, home, nil, "--root", aliasRoot, "prune", "--all"); code != 0 {
		t.Fatalf("global prune: %s", errOut)
	}
	var state struct {
		Worktrees []struct{ Path, LeaseHolder string } `json:"worktrees"`
	}
	after, err = os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Worktrees) != 1 || state.Worktrees[0].Path != lease.Path {
		t.Fatalf("prune through aliased root rewrote pool state: %s", after)
	}
}
