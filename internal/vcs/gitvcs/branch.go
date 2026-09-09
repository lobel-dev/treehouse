package gitvcs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BranchHolder describes an actual Git worktree registration for a branch.
// Missing registrations remain visible; Locked means Git forbids pruning them.
type BranchHolder struct {
	Path    string
	Locked  bool
	Missing bool
}

// BranchState reports exact local and origin ref existence and all registered
// checkouts of the local branch, including missing worktree paths.
type BranchState struct {
	Local   bool
	Origin  bool
	Holders []BranchHolder
}

// ValidateLiteralBranch accepts literal branch names, not revision expressions
// or special HEAD/@ names. It checks syntax only, not ref existence.
func ValidateLiteralBranch(repo, branch string) error {
	if branch == "" || branch == "HEAD" || branch == "@" || strings.HasPrefix(branch, "-") {
		return fmt.Errorf("invalid literal branch name %q", branch)
	}
	if _, err := runGit(repo, "check-ref-format", "refs/heads/"+branch); err != nil {
		return fmt.Errorf("invalid literal branch name %q: %w", branch, err)
	}
	return nil
}

// InspectBranch validates a literal branch name and reads its exact local and
// origin refs and worktree registrations without fetching or modifying them.
func InspectBranch(repo, branch string) (BranchState, error) {
	if err := ValidateLiteralBranch(repo, branch); err != nil {
		return BranchState{}, err
	}
	var facts BranchState
	refs, err := runGit(repo, "for-each-ref", "--format=%(refname)", "refs/heads/", "refs/remotes/origin/")
	if err != nil {
		return facts, err
	}
	for _, ref := range strings.Split(refs, "\n") {
		if ref == "refs/heads/"+branch {
			facts.Local = true
		}
		if ref == "refs/remotes/origin/"+branch {
			facts.Origin = true
		}
	}
	data, err := runGitRaw(repo, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return facts, err
	}
	var holder BranchHolder
	attached := ""
	for _, field := range strings.Split(string(data), "\x00") {
		if field == "" {
			if attached == "refs/heads/"+branch {
				_, statErr := os.Stat(holder.Path)
				if statErr != nil && !os.IsNotExist(statErr) {
					return facts, statErr
				}
				holder.Missing = os.IsNotExist(statErr)
				facts.Holders = append(facts.Holders, holder)
			}
			holder, attached = BranchHolder{}, ""
			continue
		}
		key, value, _ := strings.Cut(field, " ")
		switch key {
		case "worktree":
			holder.Path = filepath.FromSlash(value)
		case "branch":
			attached = value
		case "locked":
			holder.Locked = true
		}
	}
	return facts, nil
}

func verifiedSlotGit(repo, path string) (gitRunner, error) {
	run, err := pinnedReturnGit(path)
	if err != nil {
		return nil, err
	}
	want, err := runGit(repo, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	actual, err := run(path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	a, err := os.Stat(actual)
	if err != nil {
		return nil, err
	}
	b, err := os.Stat(want)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(a, b) {
		return nil, fmt.Errorf("Git slot %s belongs to a different repository", path)
	}
	return run, nil
}

// WithBranchIdentity locks HEAD first, then its branch ref, matching return's
// order. The caller holds the pool lock through callback and state persistence.
func WithBranchIdentity(repo, path, branch string, callback func() error) error {
	run, err := verifiedSlotGit(repo, path)
	if err != nil {
		return err
	}
	storage, err := run(path, "config", "--default", "files", "--get", "extensions.refStorage")
	if err != nil {
		return err
	}
	if storage != "files" {
		return fmt.Errorf("branch reclamation requires files ref storage")
	}
	headPath, err := gitPathUsing(run, path, "HEAD")
	if err != nil {
		return err
	}
	lock, err := os.OpenFile(headPath+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("cannot lock slot HEAD: %w", err)
	}
	defer func() { _ = lock.Close(); _ = os.Remove(headPath + ".lock") }()
	attached, err := run(path, "symbolic-ref", "--no-recurse", "-q", "HEAD")
	if err != nil || attached != "refs/heads/"+branch {
		return fmt.Errorf("slot %s no longer holds branch %q", path, branch)
	}
	unlock, err := lockReturnRef(run, path, attached)
	if err != nil {
		return err
	}
	defer unlock()
	// A symbolic branch would introduce another writable ref outside these
	// locks. Reclamation requires a direct HEAD -> branch -> commit identity.
	ref, err := run(path, "for-each-ref", "--format=%(refname) %(symref)", attached)
	if err != nil || strings.TrimSpace(ref) != attached {
		return fmt.Errorf("cannot verify a direct branch ref for %q", branch)
	}
	if _, err := run(path, "rev-parse", "--verify", "HEAD^{commit}"); err != nil {
		return err
	}
	return callback()
}

// SwitchBranch leaves Git's own dirty-file and checked-out-branch guards on.
// Treehouse holds the slot's pool ownership lock while this operation runs.
func SwitchBranch(repo, path, branch string) error {
	run, err := verifiedSlotGit(repo, path)
	if err != nil {
		return err
	}
	facts, err := InspectBranch(repo, branch)
	if err != nil {
		return err
	}
	for _, holder := range facts.Holders {
		a, ea := filepath.EvalSymlinks(holder.Path)
		b, eb := filepath.EvalSymlinks(path)
		if ea != nil || eb != nil || a != b {
			return fmt.Errorf("branch %q is held by %s; inspect git worktree list before cleanup", branch, holder.Path)
		}
	}
	args := []string{"switch", "--no-guess"}
	switch {
	case facts.Local:
		args = append(args, branch)
	case facts.Origin:
		args = append(args, "--track", "-c", branch, "refs/remotes/origin/"+branch)
	default:
		// Acquisition already chose and checked out the base. Do not resolve
		// its movable name again after hooks; start at the acquired HEAD.
		args = append(args, "-c", branch)
	}
	_, err = run(path, args...)
	return err
}
