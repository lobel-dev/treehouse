package gitvcs

import (
	"errors"
	"testing"
)

func TestInspectWorktreeOptionalRefFailureIsUnknown(t *testing.T) {
	wt, _, _ := setupSafeResetWorktree(t)
	mustGit(t, wt, "switch", "-c", "feature")
	run, err := pinnedReturnGit(wt)
	if err != nil {
		t.Fatal(err)
	}
	facts := inspectWorktreeUsing(func(path string, args ...string) (string, error) {
		if args[0] == "for-each-ref" {
			return "", errors.New("cannot read refs")
		}
		return run(path, args...)
	}, wt)
	if !facts.IdentityKnown || facts.Branch != "feature" || facts.PreservationKnown || facts.PreservingRef != "" {
		t.Fatalf("inspection failure became absent preservation: %+v", facts)
	}
}

func TestInspectWorktreeChangedHeadDiscardsFacts(t *testing.T) {
	wt, base, _ := setupSafeResetWorktree(t)
	mustGit(t, wt, "commit", "--allow-empty", "-m", "detached work")
	mustGit(t, wt, "tag", "saved")
	run, err := pinnedReturnGit(wt)
	if err != nil {
		t.Fatal(err)
	}
	facts := inspectWorktreeUsing(func(path string, args ...string) (string, error) {
		out, err := run(path, args...)
		if args[0] == "for-each-ref" {
			mustGit(t, wt, "update-ref", "--no-deref", "HEAD", base)
		}
		return out, err
	}, wt)
	if facts != (WorktreeFacts{}) {
		t.Fatalf("stale facts survived HEAD change: %+v", facts)
	}
}
