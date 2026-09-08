package gitvcs

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ReturnWorktree holds HEAD and a durable containing ref stable before stopping
// writers or touching files. A reflog or another worktree's HEAD is not enough.
func ReturnWorktree(worktreePath, branch, fallback string, seededPaths []string, beforeReset func() error) (string, error) {
	return returnWorktree(worktreePath, branch, fallback, seededPaths, beforeReset, nil)
}

// ReturnReport describes a successful reset. Optional observations never authorize it.
type ReturnReport struct {
	Parked         bool
	TargetBranch   string
	TargetCommit   string
	PriorHead      string
	AttachedBranch string
	PreservingRef  string
	Subject        string
	ChangesKnown   bool
	TrackedPaths   int
	UntrackedPaths int
}

// ReturnWorktreeReport captures protected identities inside the return locks.
func ReturnWorktreeReport(worktreePath, branch, fallback string, seededPaths []string, beforeReset func() error) (ReturnReport, error) {
	var report ReturnReport
	_, err := returnWorktree(worktreePath, branch, fallback, seededPaths, beforeReset, &report)
	return report, err
}

func returnWorktree(worktreePath, branch, fallback string, seededPaths []string, beforeReset func() error, report *ReturnReport) (string, error) {
	run, err := pinnedReturnGit(worktreePath)
	if err != nil {
		return "", err
	}
	return returnWorktreeUsing(run, worktreePath, branch, fallback, seededPaths, beforeReset, report)
}

func returnWorktreeUsing(run gitRunner, worktreePath, branch, fallback string, seededPaths []string, beforeReset func() error, report *ReturnReport) (string, error) {
	storage, err := run(worktreePath, "config", "--default", "files", "--get", "extensions.refStorage")
	if err != nil {
		return "", err
	}
	if storage != "files" {
		return "", fmt.Errorf("unsupported Git ref storage %q: return requires files ref locking", storage)
	}
	target, err := resolveReturnRef(run, worktreePath, branch)
	if err != nil && fallback != "" && fallback != branch {
		target, err = resolveReturnRef(run, worktreePath, fallback)
		branch = fallback
	}
	if err != nil {
		return "", err
	}
	head, err := run(worktreePath, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", err
	}
	prepared := false
	err = resetWorktreeToRefUsing(run, worktreePath, target, head, false, seededPaths, true, func(head string) (func(), error) {
		unlock, ref, attached, err := lockContainingRef(run, worktreePath, head)
		if err != nil {
			return nil, err
		}
		if beforeReset != nil {
			if err := beforeReset(); err != nil {
				unlock()
				return nil, err
			}
		}
		current, err := run(worktreePath, "rev-parse", "--verify", "HEAD^{commit}")
		if err != nil || current != head {
			unlock()
			return nil, fmt.Errorf("worktree HEAD changed since safety check")
		}
		if report != nil {
			report.TargetBranch, report.TargetCommit = branch, target
			report.PriorHead, report.PreservingRef = head, ref
			report.AttachedBranch = strings.TrimPrefix(attached, "refs/heads/")
			report.Subject, _ = run(worktreePath, "show", "-s", "--format=%s", head)
			observeReturnChanges(run, worktreePath, report)
		}
		prepared = true
		return unlock, nil
	})
	if report != nil {
		report.Parked = err == nil
	}
	if err != nil && prepared {
		return branch, fmt.Errorf("return reset did not complete; worktree files may be partially updated: %w", err)
	}
	return branch, err
}

func lockContainingRef(run gitRunner, worktreePath, head string) (func(), string, string, error) {
	refs, err := run(worktreePath, "for-each-ref", "--contains="+head, "--format=%(refname) %(symref)", "refs/heads/", "refs/tags/", "refs/remotes/")
	if err != nil {
		return nil, "", "", err
	}
	// An attached HEAD also changes when another worktree updates its branch.
	// Hold that exact branch, not merely some other containing ref.
	attached, _ := run(worktreePath, "symbolic-ref", "-q", "HEAD")
	var contention error
	verificationFailed := false
	for _, line := range strings.Split(refs, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 1 || (attached != "" && fields[0] != attached) {
			continue
		}
		ref := fields[0]
		unlock, err := lockReturnRef(run, worktreePath, ref)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				contention = err
				continue
			}
			return nil, "", "", err
		}
		if !refStillPreservesHead(run, worktreePath, ref, head) {
			verificationFailed = true
			unlock()
			continue
		}
		current, err := run(worktreePath, "rev-parse", "--verify", "HEAD^{commit}")
		if err != nil || current != head {
			unlock()
			return nil, "", "", fmt.Errorf("worktree HEAD changed since safety check")
		}
		return unlock, ref, attached, nil
	}
	if contention != nil {
		return nil, "", "", contention
	}
	if verificationFailed {
		return nil, "", "", fmt.Errorf("refusing to return worktree: could not revalidate a durable ref preserving HEAD %s", head)
	}
	return nil, "", "", &UnpreservedHeadError{Head: head}
}

func lockReturnRef(run gitRunner, worktreePath, ref string) (func(), error) {
	refPath, err := gitPathUsing(run, worktreePath, ref)
	if err != nil {
		return nil, fmt.Errorf("cannot locate containing ref %s: %w", ref, err)
	}
	if err := os.MkdirAll(filepath.Dir(refPath), 0777); err != nil {
		return nil, fmt.Errorf("cannot lock containing ref %s: %w", ref, err)
	}
	lockPath := refPath + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
	if err != nil {
		return nil, fmt.Errorf("cannot lock containing ref %s: %w", ref, err)
	}
	return func() { _ = lock.Close(); _ = os.Remove(lockPath) }, nil
}

func refStillPreservesHead(run gitRunner, worktreePath, ref, head string) bool {
	// The ref may have moved or become symbolic since enumeration. Git takes
	// this same ref lock for loose and packed refs, including deletion.
	symbolic, err := run(worktreePath, "symbolic-ref", "-q", ref)
	if err == nil || symbolic != "" {
		return false
	}
	_, err = run(worktreePath, "merge-base", "--is-ancestor", head, ref+"^{commit}")
	return err == nil
}

func resolveReturnRef(run gitRunner, worktreePath, branch string) (string, error) {
	return run(worktreePath, "rev-parse", "--verify", branchRefUsing(run, worktreePath, branch)+"^{commit}")
}

// Resolve the linked-worktree marker without invoking Git discovery. The
// backlink authenticates the selected gitdir before any Git command runs.
func returnGitDir(worktreePath string) (string, error) {
	root, err := os.OpenRoot(worktreePath)
	if err != nil {
		return "", err
	}
	defer root.Close()
	marker, err := root.Open(".git")
	if err != nil {
		return "", err
	}
	defer marker.Close()
	info, err := marker.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("expected linked-worktree Git marker")
	}
	contents, err := io.ReadAll(marker)
	if err != nil {
		return "", err
	}
	gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(contents)), "gitdir: ")
	if !ok || gitDir == "" {
		return "", fmt.Errorf("invalid linked-worktree Git marker")
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktreePath, gitDir)
	}
	gitDir, err = filepath.EvalSymlinks(gitDir)
	if err != nil {
		return "", err
	}
	backlink, err := os.ReadFile(filepath.Join(gitDir, "gitdir"))
	if err != nil {
		return "", err
	}
	backlinkPath := strings.TrimSpace(string(backlink))
	if !filepath.IsAbs(backlinkPath) {
		backlinkPath = filepath.Join(gitDir, backlinkPath)
	}
	linkedMarker, err := os.Stat(backlinkPath)
	if err != nil || !os.SameFile(info, linkedMarker) {
		return "", fmt.Errorf("Git directory does not belong to worktree")
	}
	return gitDir, nil
}

func pinnedReturnGit(worktreePath string) (gitRunner, error) {
	worktreePath, err := filepath.Abs(worktreePath)
	if err != nil {
		return nil, err
	}
	worktreePath, err = filepath.EvalSymlinks(worktreePath)
	if err != nil {
		return nil, err
	}
	gitDir, err := returnGitDir(worktreePath)
	if err != nil {
		return nil, fmt.Errorf("refusing to return worktree: cannot verify Git marker: %w", err)
	}
	// Explicit location arguments pin discovery. Scrub repository-location
	// overrides which otherwise redirect the common refs, index, or objects.
	env := make([]string, 0, len(os.Environ())+1)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		switch strings.ToUpper(key) {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_PREFIX", "GIT_GRAFT_FILE":
			continue
		}
		env = append(env, item)
	}
	// Stored parents alone prove reachability: replacement objects and legacy
	// grafts must not manufacture containment, including ambient overrides.
	env = append(env, "GIT_GRAFT_FILE="+os.DevNull)
	return func(_ string, args ...string) (string, error) {
		commandArgs := append([]string{"--no-replace-objects", "--git-dir=" + gitDir, "--work-tree=" + worktreePath}, args...)
		cmd := exec.Command("git", commandArgs...)
		cmd.Dir = worktreePath
		cmd.Env = env
		if len(args) > 0 && args[0] == "status" {
			cmd.Env = append(append([]string(nil), env...), "GIT_OPTIONAL_LOCKS=0")
		}
		out, err := cmd.Output()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exitErr.Stderr)))
			}
			return "", err
		}
		if len(args) > 0 && args[0] == "status" {
			return string(out), nil
		}
		return strings.TrimSpace(string(out)), nil
	}, nil
}

// Porcelain -z makes unusual filenames and renames unambiguous. Ignored files
// are deliberately excluded; these are observations, not a deletion audit.
func observeReturnChanges(run gitRunner, path string, report *ReturnReport) {
	out, err := run(path, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return
	}
	records := strings.Split(out, "\x00")
	tracked, untracked := 0, 0
	for i := 0; i < len(records); i++ {
		record := records[i]
		if record == "" {
			continue
		}
		if len(record) < 3 {
			return
		}
		if strings.HasPrefix(record, "?? ") {
			untracked++
			continue
		}
		tracked++
		if record[0] == 'R' || record[0] == 'C' || record[1] == 'R' || record[1] == 'C' {
			i++
		}
	}
	report.ChangesKnown, report.TrackedPaths, report.UntrackedPaths = true, tracked, untracked
}

// UnpreservedHeadError distinguishes a proven absence from an inspection failure.
type UnpreservedHeadError struct{ Head string }

func (e *UnpreservedHeadError) Error() string {
	return fmt.Sprintf("refusing to return worktree: HEAD %s is not preserved by an available branch, tag, or remote ref; create a branch at HEAD before returning (reflogs are not sufficient)", e.Head)
}
