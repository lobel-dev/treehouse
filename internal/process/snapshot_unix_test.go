//go:build !windows

package process

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// One Snapshot must answer for many worktrees exactly as a per-worktree scan
// would: a global listing reads the process table once, so classification must
// not depend on rescanning per slot. Symlinked worktree paths must still match
// (macOS /tmp -> /private/tmp), and a sibling worktree must not.
func TestSnapshot_ClassifiesManyWorktreesFromOneReading(t *testing.T) {
	parent := t.TempDir()
	busy := filepath.Join(parent, "busy")
	idle := filepath.Join(parent, "idle")
	child := filepath.Join(busy, "sub")
	if err := exec.Command("mkdir", "-p", child, idle).Run(); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("sleep", "60")
	cmd.Dir = child
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sleep: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	time.Sleep(200 * time.Millisecond)

	snapshot, err := NewSnapshot()
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}

	for _, tc := range []struct {
		path string
		want bool
	}{
		{busy, true},
		{idle, false},
	} {
		procs, err := snapshot.ProcessesInWorktree(tc.path)
		if err != nil {
			t.Fatalf("ProcessesInWorktree(%s): %v", tc.path, err)
		}
		var found bool
		for _, p := range procs {
			if int(p.PID) == cmd.Process.Pid {
				found = true
				if p.Name == "" {
					t.Errorf("%s: matched pid %d with no name", tc.path, p.PID)
				}
			}
		}
		if found != tc.want {
			t.Errorf("%s: found pid %d = %v, want %v (got %v)", tc.path, cmd.Process.Pid, found, tc.want, procs)
		}

		// The single-worktree helper must agree with the shared snapshot.
		direct, err := FindProcessesInWorktree(tc.path)
		if err != nil {
			t.Fatalf("FindProcessesInWorktree(%s): %v", tc.path, err)
		}
		var directFound bool
		for _, p := range direct {
			if int(p.PID) == cmd.Process.Pid {
				directFound = true
			}
		}
		if directFound != found {
			t.Errorf("%s: snapshot found=%v but FindProcessesInWorktree found=%v", tc.path, found, directFound)
		}
	}
}
