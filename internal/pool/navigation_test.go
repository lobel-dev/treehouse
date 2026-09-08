package pool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeTestPool(t *testing.T, root, name string, state State) string {
	t.Helper()
	poolDir := filepath.Join(root, name)
	for i := range state.Worktrees {
		if err := os.MkdirAll(state.Worktrees[i].Path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateFilePath(poolDir), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return poolDir
}

// A pool whose state cannot be read must not hide the pools that can: global
// navigation is read-only, so it reports the bad pool and still lists the rest.
func TestListSnapshotAll_ReportsUnreadablePoolAndKeepsHealthyOnes(t *testing.T) {
	root := t.TempDir()
	goodDir := writeTestPool(t, root, "good", State{
		Version:   stateVersion,
		Worktrees: []WorktreeEntry{{Name: "1", Path: filepath.Join(root, "good", "1")}},
	})
	badDir := filepath.Join(root, "bad")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A state file from a newer treehouse is a hard ReadState failure.
	if err := os.WriteFile(stateFilePath(badDir), []byte(`{"version":999,"worktrees":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	pools, failures, err := ListSnapshotAll(root)
	if err != nil {
		t.Fatalf("root-level error for a per-pool failure: %v", err)
	}
	if len(pools) != 1 || pools[0].PoolDir != goodDir || len(pools[0].Worktrees) != 1 || pools[0].Worktrees[0].Name != "1" {
		t.Fatalf("healthy pool not listed: %+v", pools)
	}
	if len(failures) != 1 || failures[0].PoolDir != badDir {
		t.Fatalf("unreadable pool not reported: %+v", failures)
	}
	if failures[0].Err == nil {
		t.Fatal("failure carries no cause")
	}
}

// A healthy root with no pools is an empty listing, not a failure, and is never
// created by reading it.
func TestListSnapshotAll_MissingRootIsEmptyAndNotCreated(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	pools, failures, err := ListSnapshotAll(root)
	if err != nil || len(pools) != 0 || len(failures) != 0 {
		t.Fatalf("missing root: pools=%v failures=%v err=%v", pools, failures, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("reading a missing root created it")
	}
}

// prune deletes, so it keeps its all-or-nothing reading of the same scan that
// navigation tolerates.
func TestPrunePoolDirs_FailsClosedOnPoolNavigationTolerates(t *testing.T) {
	root := t.TempDir()
	writeTestPool(t, root, "good", State{Version: stateVersion})
	badDir := filepath.Join(root, "bad")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateFilePath(badDir), []byte(`{"version":999,"worktrees":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Discovery finds both; only the read of "bad" fails, which prune surfaces
	// through planPrunePool rather than prunePoolDirs.
	dirs, failures, err := NavigationPoolDirs(root)
	if err != nil || len(failures) != 0 || len(dirs) != 2 {
		t.Fatalf("discovery: dirs=%v failures=%v err=%v", dirs, failures, err)
	}
	pruneDirs, err := prunePoolDirs(root)
	if err != nil {
		t.Fatalf("prunePoolDirs: %v", err)
	}
	if len(pruneDirs) != len(dirs) {
		t.Fatalf("prune and navigation disagree on discovery: %v vs %v", pruneDirs, dirs)
	}
}
