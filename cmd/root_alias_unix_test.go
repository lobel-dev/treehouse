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

	out, errOut, code := runTreehouse(t, repo, home, nil, "--root", realRoot, "get", "--lease", "--json", "--lease-holder", "root-alias-test")
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
	var rows []statusJSONResult
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("aliased root listed %d slots, want 1: %s", len(rows), out)
	}
	if rows[0].Path != lease.Path || rows[0].Status != "leased" || rows[0].LeaseID != lease.LeaseID || rows[0].LeaseHolder != lease.LeaseHolder {
		t.Fatalf("aliased root changed the original lease: %+v", rows[0])
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
	after, err = os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("prune through aliased root rewrote pool state: %s", after)
	}
}

func TestAliasedRootPreservesStateWhenRecordedPathIsInaccessible(t *testing.T) {
	home, outside := t.TempDir(), t.TempDir()
	repo := setupTestRepoWithHome(t, home, "inaccessible-alias")
	realRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	aliasParent := t.TempDir()
	aliasRoot := filepath.Join(aliasParent, "alias")
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	out, errOut, code := runTreehouse(t, repo, home, nil, "--root", aliasRoot, "get", "--lease", "--json")
	if code != 0 {
		t.Fatalf("get: %s", errOut)
	}
	var lease leaseJSONResult
	if err := json.Unmarshal([]byte(out), &lease); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(lease.Path, aliasRoot+string(filepath.Separator)) {
		t.Fatalf("fixture must record the alias path, got %s", lease.Path)
	}
	statePath, err := filepath.EvalSymlinks(filepath.Join(filepath.Dir(filepath.Dir(lease.Path)), "treehouse-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(aliasParent, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(aliasParent, 0o755); err != nil {
			t.Errorf("restoring alias permissions: %v", err)
		}
	})
	if _, err := os.Stat(lease.Path); err == nil {
		t.Skip("filesystem does not enforce directory permissions")
	} else if !os.IsPermission(err) {
		t.Fatalf("expected permission-denied fixture, got %v", err)
	}

	for _, command := range [][]string{{"status", "--all", "--json"}, {"prune", "--all"}} {
		t.Run(command[0], func(t *testing.T) {
			args := append([]string{"--root", realRoot}, command...)
			_, errOut, code := runTreehouse(t, outside, home, nil, args...)
			if code == 0 {
				t.Errorf("command succeeded despite inaccessible registered path")
			}
			if !strings.Contains(errOut, "permission denied") {
				t.Errorf("missing filesystem error: %s", errOut)
			}
			after, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatalf("inaccessible path changed pool state:\nbefore %s\nafter %s", before, after)
			}
		})
	}
}
