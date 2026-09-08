package gitvcs

import "strings"

// WorktreeFacts are optional observations, never permission to reset or delete.
type WorktreeFacts struct {
	Branch            string
	IdentityKnown     bool
	PreservingRef     string
	PreservationKnown bool
}

// InspectWorktree reads real Git ancestry through the slot's authenticated
// metadata. A changed HEAD or failed read leaves the corresponding fact unknown.
func InspectWorktree(path string) WorktreeFacts {
	run, err := pinnedReturnGit(path)
	if err != nil {
		return WorktreeFacts{}
	}
	return inspectWorktreeUsing(run, path)
}

func inspectWorktreeUsing(run gitRunner, path string) WorktreeFacts {
	head, err := run(path, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return WorktreeFacts{}
	}
	identity, err := run(path, "rev-parse", "--symbolic-full-name", "HEAD")
	if err != nil {
		return WorktreeFacts{}
	}
	if identity != "HEAD" && !strings.HasPrefix(identity, "refs/heads/") {
		return WorktreeFacts{}
	}
	facts := WorktreeFacts{IdentityKnown: true}
	if strings.HasPrefix(identity, "refs/heads/") {
		facts.Branch = strings.TrimPrefix(identity, "refs/heads/")
	}
	refs, err := run(path, "for-each-ref", "--contains="+head, "--format=%(refname) %(symref)", "refs/heads/", "refs/tags/", "refs/remotes/")
	if err == nil {
		facts.PreservationKnown = true
		for _, line := range strings.Split(refs, "\n") {
			fields := strings.Fields(line)
			if len(fields) != 1 {
				continue
			}
			if facts.PreservingRef == "" || fields[0] == identity {
				facts.PreservingRef = fields[0]
			}
		}
		if facts.PreservingRef != "" && !refStillPreservesHead(run, path, facts.PreservingRef, head) {
			facts.PreservingRef, facts.PreservationKnown = "", false
		}
	}
	current, headErr := run(path, "rev-parse", "--verify", "HEAD^{commit}")
	attachment, identityErr := run(path, "rev-parse", "--symbolic-full-name", "HEAD")
	if headErr != nil || identityErr != nil || current != head || attachment != identity {
		return WorktreeFacts{}
	}
	return facts
}
