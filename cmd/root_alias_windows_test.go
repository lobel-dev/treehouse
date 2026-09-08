//go:build windows

package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Windows addresses one pool directory through several spellings too: NTFS is
// case-insensitive, so an upper-cased root names the very same slots the state
// file recorded in their canonical case. Matching those slots to their state
// entries reads their filesystem identity, and Windows resolves that identity
// by reopening the directory - a step that can fail while the plain os.Stat
// that precedes it succeeds. A live slot must be recognized as registered, or
// the failure must surface; neither may fabricate a duplicate "recovered"
// lease over the real one. An open handle on the worktree is held throughout,
// because a slot in use by an agent is the ordinary case.
func TestCaseAliasedRootDoesNotFabricateRecoveredSlots(t *testing.T) {
	home, outside := t.TempDir(), t.TempDir()
	repo := setupTestRepoWithHome(t, home, "case-aliased")

	realRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	aliasRoot := strings.ToUpper(realRoot)
	if aliasRoot == realRoot {
		t.Skipf("root %s has no case alias", realRoot)
	}
	if _, err := os.Stat(aliasRoot); err != nil {
		t.Skipf("filesystem is case-sensitive: %v", err)
	}

	out, errOut, code := runTreehouse(t, repo, home, nil, "--root", realRoot, "get", "--lease", "--json", "--lease-holder", "case-alias-test")
	if code != 0 {
		t.Fatalf("get: %s", errOut)
	}
	var lease leaseJSONResult
	if err := json.Unmarshal([]byte(out), &lease); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(filepath.Dir(filepath.Dir(lease.Path)), "treehouse-state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	held, err := os.Open(lease.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	out, errOut, code = runTreehouse(t, outside, home, nil, "--root", aliasRoot, "status", "--all", "--json")
	if code != 0 {
		t.Fatalf("global status: %s", errOut)
	}
	var rows []statusJSONResult
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("case-aliased root listed %d slots, want 1: %s", len(rows), out)
	}
	if rows[0].Status != "leased" || rows[0].LeaseID != lease.LeaseID || rows[0].LeaseHolder != lease.LeaseHolder {
		t.Fatalf("case-aliased root changed the original lease: %+v", rows[0])
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("global status wrote pool state:\nbefore %s\nafter  %s", before, after)
	}

	if _, errOut, code = runTreehouse(t, outside, home, nil, "--root", aliasRoot, "prune", "--all"); code != 0 {
		t.Fatalf("global prune: %s", errOut)
	}
	after, err = os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("prune through case-aliased root rewrote pool state: %s", after)
	}
}
