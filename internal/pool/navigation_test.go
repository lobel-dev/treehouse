package pool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kunchenguid/treehouse/internal/process"
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

// An interrupted prune/destroy persists Destroying plus an owner reservation,
// then runs its hooks outside the state lock. If it dies there, the slot and
// the user's work survive on disk, and local status shows the slot because
// healState clears the dead reservation in memory. A read-only global listing
// cannot heal, so it must apply the same staleness rule instead of silently
// dropping a real worktree - navigation exists for exactly this recovery case.
func TestListSnapshot_ExposesStaleDestroyingSlotAndHidesActiveOne(t *testing.T) {
	root := t.TempDir()
	livePID := int32(os.Getpid())
	liveStart, ok := process.StartedAt(livePID)
	if !ok {
		t.Skip("cannot read this process's start time")
	}

	poolDir := writeTestPool(t, root, "interrupted", State{
		Version: stateVersion,
		Worktrees: []WorktreeEntry{
			{Name: "1", Path: filepath.Join(root, "interrupted", "1"), Destroying: true, OwnerPID: deadPID(t), OwnerStartedAt: 1},
			{Name: "2", Path: filepath.Join(root, "interrupted", "2"), Destroying: true, OwnerPID: livePID, OwnerStartedAt: liveStart},
			{Name: "3", Path: filepath.Join(root, "interrupted", "3")},
		},
	})

	got, err := ListSnapshot(poolDir)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, wt := range got {
		names[wt.Name] = true
	}
	if !names["1"] {
		t.Error("a slot whose destroy reservation is stale must be listed, not hidden")
	}
	if names["2"] {
		t.Error("a slot whose destruction is genuinely active must stay hidden")
	}
	if !names["3"] {
		t.Error("an ordinary slot must be listed")
	}

	// The global walk must agree with the single-pool read.
	pools, failures, err := ListSnapshotAll(root)
	if err != nil || len(failures) != 0 || len(pools) != 1 {
		t.Fatalf("ListSnapshotAll: pools=%v failures=%v err=%v", pools, failures, err)
	}
	if len(pools[0].Worktrees) != len(got) {
		t.Fatalf("global walk disagrees with single-pool read: %+v vs %+v", pools[0].Worktrees, got)
	}

	// Local List heals first, so it must reach the same set of slots.
	local, err := List(poolDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(local) != len(got) {
		t.Fatalf("local status and global listing disagree: %+v vs %+v", local, got)
	}
}

// Reading the process table is expensive, so a pool with nothing to classify
// must not read it at all.
func TestDescribeWorktrees_SkipsProcessScanWhenNothingToClassify(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state State
		want  bool
	}{
		{"no slots", State{Version: stateVersion}, false},
		{"only actively destroying slots", State{
			Version:   stateVersion,
			Worktrees: []WorktreeEntry{{Name: "1", Path: t.TempDir(), Destroying: true}},
		}, false},
		{"a classifiable slot", State{
			Version:   stateVersion,
			Worktrees: []WorktreeEntry{{Name: "1", Path: t.TempDir()}},
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var scanned bool
			describeWorktrees(tc.state, func() process.Snapshot {
				scanned = true
				return process.Snapshot{}
			})
			if scanned != tc.want {
				t.Fatalf("process table read = %v, want %v", scanned, tc.want)
			}
		})
	}
}

// deadPID returns a PID that is not running, so a reservation naming it is stale.
func deadPID(t *testing.T) int32 {
	t.Helper()
	for pid := int32(30000); pid < 40000; pid++ {
		if !process.Exists(pid) {
			return pid
		}
	}
	t.Skip("no free PID found")
	return 0
}
