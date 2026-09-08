//go:build !windows

package pool

import (
	"os"
	"path/filepath"
	"testing"
)

// A pool directory that cannot even be stat'ed (an unreadable mount, a
// restricted permission) is a discovery-time failure. Navigation must report it
// and still list the healthy pools, while prune keeps failing closed.
func TestNavigationPoolDirs_UnstatablePoolIsReportedNotFatal(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	root := t.TempDir()
	goodDir := writeTestPool(t, root, "good", State{
		Version:   stateVersion,
		Worktrees: []WorktreeEntry{{Name: "1", Path: filepath.Join(root, "good", "1")}},
	})
	sealed := filepath.Join(root, "sealed")
	if err := os.MkdirAll(sealed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sealed, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sealed, 0o755) })

	dirs, failures, err := NavigationPoolDirs(root)
	if err != nil {
		t.Fatalf("root-level error for a per-pool stat failure: %v", err)
	}
	if len(dirs) != 1 || dirs[0] != goodDir {
		t.Fatalf("healthy pool not discovered: %v", dirs)
	}
	if len(failures) != 1 || failures[0].PoolDir != sealed {
		t.Fatalf("unstatable pool not reported: %+v", failures)
	}

	pools, listFailures, err := ListSnapshotAll(root)
	if err != nil {
		t.Fatalf("ListSnapshotAll: %v", err)
	}
	if len(pools) != 1 || len(pools[0].Worktrees) != 1 {
		t.Fatalf("healthy pool not listed: %+v", pools)
	}
	if len(listFailures) != 1 || listFailures[0].PoolDir != sealed {
		t.Fatalf("unstatable pool not carried through: %+v", listFailures)
	}

	if _, err := prunePoolDirs(root); err == nil {
		t.Fatal("prune must fail closed on a pool it cannot inspect")
	}
}
