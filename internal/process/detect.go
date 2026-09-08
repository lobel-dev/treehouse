package process

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/shirou/gopsutil/v4/process"
)

type ProcessInfo struct {
	PID  int32
	Name string
}

func (p ProcessInfo) String() string {
	return fmt.Sprintf("%s (%d)", p.Name, p.PID)
}

func IsWorktreeInUse(worktreePath string) (bool, error) {
	procs, err := FindProcessesInWorktree(worktreePath)
	if err != nil {
		return false, err
	}
	return len(procs) > 0, nil
}

func Exists(pid int32) bool {
	exists, err := process.PidExists(pid)
	return err == nil && exists
}

func StartedAt(pid int32) (int64, bool) {
	proc, err := process.NewProcess(pid)
	if err != nil {
		return 0, false
	}
	startedAt, err := proc.CreateTime()
	return startedAt, err == nil
}

// Snapshot holds one reading of the process table so any number of worktrees
// can be matched against it. Reading a process working directory is expensive
// (a dlopen plus a locked OS thread on darwin, an OpenProcess plus a remote
// memory read on Windows), so listing many pools must not re-enumerate the
// table per worktree.
type Snapshot struct {
	procs []snapshotProcess
}

type snapshotProcess struct {
	proc *process.Process
	cwd  string
}

// NewSnapshot reads every process working directory once.
func NewSnapshot() (Snapshot, error) {
	procs, err := process.Processes()
	if err != nil {
		return Snapshot{}, err
	}
	snapshot := Snapshot{procs: make([]snapshotProcess, 0, len(procs))}
	for _, p := range procs {
		cwd, err := p.Cwd()
		if err != nil {
			continue
		}
		absCwd := normalizeCwd(cwd)
		if absCwd == "" {
			continue
		}
		snapshot.procs = append(snapshot.procs, snapshotProcess{proc: p, cwd: absCwd})
	}
	return snapshot, nil
}

// ProcessesInWorktree returns the snapshot's processes whose current directory
// is the worktree root or a descendant after absolute path and symlink
// resolution.
func (s Snapshot) ProcessesInWorktree(worktreePath string) ([]ProcessInfo, error) {
	absWorktree, err := filepath.Abs(worktreePath)
	if err != nil {
		return nil, err
	}
	absWorktree = resolvePath(absWorktree)

	var result []ProcessInfo
	for _, p := range s.procs {
		if !cwdWithinWorktree(absWorktree, p.cwd) {
			continue
		}
		name, _ := p.proc.Name()
		result = append(result, ProcessInfo{
			PID:  p.proc.Pid,
			Name: name,
		})
	}
	return result, nil
}

// FindProcessesInWorktree returns processes whose current directory is the
// worktree root or a descendant after absolute path and symlink resolution.
func FindProcessesInWorktree(worktreePath string) ([]ProcessInfo, error) {
	snapshot, err := NewSnapshot()
	if err != nil {
		return nil, err
	}
	return snapshot.ProcessesInWorktree(worktreePath)
}

// normalizeCwd returns the absolute, symlink-resolved form of a process working
// directory, or "" when it cannot be matched against a worktree. gopsutil
// returns "" (with no error) for processes whose working directory cannot be
// read - notably Windows system processes such as System and csrss.exe - and
// filepath.Abs("") would otherwise resolve to the caller's own directory,
// wrongly matching every such process whenever the caller runs from inside the
// worktree.
func normalizeCwd(cwd string) string {
	if cwd == "" {
		return ""
	}
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return ""
	}
	return resolvePath(absCwd)
}

// cwdWithinWorktree reports whether a process working directory falls inside the
// worktree root. Both must already be absolute and symlink-resolved; an empty
// cwd never matches (see normalizeCwd).
func cwdWithinWorktree(absWorktree, absCwd string) bool {
	if absCwd == "" {
		return false
	}
	rel, err := filepath.Rel(absWorktree, absCwd)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// resolvePath returns the symlink-resolved path, or the input if resolution
// fails (e.g. path doesn't exist). This lets us match process cwds (which
// gopsutil returns canonicalized, e.g. /private/var/... on macOS) against
// caller-supplied worktree paths that may still contain symlinks.
func resolvePath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}
