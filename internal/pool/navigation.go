package pool

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/kunchenguid/treehouse/internal/process"
)

// PoolError names a pool that could not be inspected, so a read-only caller can
// report it and still return the pools that did read.
type PoolError struct {
	PoolDir string
	Err     error
}

func (e PoolError) Error() string {
	return filepath.Base(e.PoolDir) + ": " + e.Err.Error()
}

func (e PoolError) Unwrap() error { return e.Err }

// PoolSnapshot is one pool's read-only listing under a navigation root.
type PoolSnapshot struct {
	PoolDir   string
	Worktrees []WorktreeStatus
}

// NavigationPoolDirs finds managed pools directly under the selected root.
// Pool directories that cannot be inspected are returned as failures rather
// than aborting the scan, so callers choose whether one bad pool hides the
// rest; only a root-level failure is an error. A missing root is an empty
// result, never created.
func NavigationPoolDirs(root string) ([]string, []PoolError, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}

	var poolDirs []string
	var failures []PoolError
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		poolDir := filepath.Join(root, entry.Name())
		if _, err := os.Stat(stateFilePath(poolDir)); err == nil {
			poolDirs = append(poolDirs, poolDir)
		} else if !os.IsNotExist(err) {
			failures = append(failures, PoolError{PoolDir: poolDir, Err: err})
		}
	}
	sort.Strings(poolDirs)
	return poolDirs, failures, nil
}

// ListSnapshotAll lists every managed pool directly under root without healing
// or writing pool state, reading the process table once for the whole walk.
// Navigation is read-only, so a pool that cannot be read is returned as a
// failure instead of hiding every other project; only a root-level failure is
// an error.
func ListSnapshotAll(root string) ([]PoolSnapshot, []PoolError, error) {
	dirs, failures, err := NavigationPoolDirs(root)
	if err != nil {
		return nil, nil, err
	}

	snapshot, _ := process.NewSnapshot()
	pools := make([]PoolSnapshot, 0, len(dirs))
	for _, dir := range dirs {
		worktrees, err := listSnapshot(dir, snapshot)
		if err != nil {
			failures = append(failures, PoolError{PoolDir: dir, Err: err})
			continue
		}
		pools = append(pools, PoolSnapshot{PoolDir: dir, Worktrees: worktrees})
	}
	sort.Slice(failures, func(i, j int) bool { return failures[i].PoolDir < failures[j].PoolDir })
	return pools, failures, nil
}

// ListSnapshot reports existing slots without healing or writing pool state.
// Atomic state replacement permits readers to observe a complete snapshot
// without creating a lock file. Lifecycle operations retain their own locks.
func ListSnapshot(poolDir string) ([]WorktreeStatus, error) {
	snapshot, _ := process.NewSnapshot()
	return listSnapshot(poolDir, snapshot)
}

func listSnapshot(poolDir string, snapshot process.Snapshot) ([]WorktreeStatus, error) {
	state, err := ReadState(poolDir)
	if err != nil {
		return nil, err
	}
	var existing []WorktreeEntry
	for _, wt := range state.Worktrees {
		info, err := os.Stat(wt.Path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			existing = append(existing, wt)
		}
	}
	state.Worktrees = existing
	return describeWorktrees(state, snapshot), nil
}
