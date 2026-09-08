package pool

import "os"

// NavigationPoolDirs finds managed pools directly under the selected root.
func NavigationPoolDirs(root string) ([]string, error) {
	return prunePoolDirs(root)
}

// ListSnapshot reports existing slots without healing or writing pool state.
// Atomic state replacement permits readers to observe a complete snapshot
// without creating a lock file. Lifecycle operations retain their own locks.
func ListSnapshot(poolDir string) ([]WorktreeStatus, error) {
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
	return describeWorktrees(state), nil
}
