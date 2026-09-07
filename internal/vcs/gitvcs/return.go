package gitvcs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ReturnWorktree holds HEAD and a durable containing ref stable before stopping
// writers or touching files. A reflog or another worktree's HEAD is not enough.
func ReturnWorktree(worktreePath, branch, fallback string, seededPaths []string, beforeReset func() error) (string, error) {
	storage, err := runGit(worktreePath, "config", "--default", "files", "--get", "extensions.refStorage")
	if err != nil {
		return "", err
	}
	if storage != "files" {
		return "", fmt.Errorf("unsupported Git ref storage %q: return requires files ref locking", storage)
	}
	target, err := resolveResetRef(worktreePath, branch)
	if err != nil && fallback != "" && fallback != branch {
		target, err = resolveResetRef(worktreePath, fallback)
		branch = fallback
	}
	if err != nil {
		return "", err
	}
	head, err := worktreeHead(worktreePath)
	if err != nil {
		return "", err
	}
	err = resetWorktreeToRef(worktreePath, target, head, false, seededPaths, true, func(head string) (func(), error) {
		unlock, err := lockContainingRef(worktreePath, head)
		if err != nil {
			return nil, err
		}
		if beforeReset != nil {
			if err := beforeReset(); err != nil {
				unlock()
				return nil, err
			}
		}
		current, err := worktreeHead(worktreePath)
		if err != nil || current != head {
			unlock()
			return nil, fmt.Errorf("worktree HEAD changed since safety check")
		}
		return unlock, nil
	})
	return branch, err
}

func lockContainingRef(worktreePath, head string) (func(), error) {
	refs, err := runGitStoredHistory(worktreePath, "for-each-ref", "--contains="+head, "--format=%(refname) %(symref)", "refs/heads/", "refs/tags/", "refs/remotes/")
	if err != nil {
		return nil, err
	}
	// An attached HEAD also changes when another worktree updates its branch.
	// Hold that exact branch, not merely some other containing ref.
	attached, _ := runGit(worktreePath, "symbolic-ref", "-q", "HEAD")
	for _, line := range strings.Split(refs, "\n") {
		fields := strings.Fields(line)
		// Symbolic refs do not independently preserve a commit.
		if len(fields) != 1 {
			continue
		}
		ref := fields[0]
		if attached != "" && ref != attached {
			continue
		}
		refPath, err := gitPath(worktreePath, ref)
		if err != nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(refPath), 0777); err != nil {
			continue
		}
		lockPath := refPath + ".lock"
		lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
		if err != nil {
			continue
		}
		unlock := func() { _ = lock.Close(); _ = os.Remove(lockPath) }
		// The ref may have moved or become symbolic since enumeration. Git takes
		// this same ref lock for loose and packed refs, including deletion.
		symbolic, symErr := runGit(worktreePath, "symbolic-ref", "-q", ref)
		if symErr == nil || symbolic != "" {
			unlock()
			continue
		}
		if _, err := runGitStoredHistory(worktreePath, "merge-base", "--is-ancestor", head, ref+"^{commit}"); err != nil {
			unlock()
			continue
		}
		current, err := worktreeHead(worktreePath)
		if err != nil || current != head {
			unlock()
			return nil, fmt.Errorf("worktree HEAD changed since safety check")
		}
		return unlock, nil
	}
	return nil, fmt.Errorf("refusing to return worktree: HEAD %s is not preserved by an available branch, tag, or remote ref; create a branch at HEAD before returning (reflogs are not sufficient)", head)
}

// Proof of durable reachability must use stored parents. Replacement objects
// and legacy grafts can make a ref appear to contain an otherwise lost commit.
// Force the graft override last so an inherited GIT_GRAFT_FILE cannot win.
func runGitStoredHistory(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"--no-replace-objects"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_GRAFT_FILE="+os.DevNull)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
