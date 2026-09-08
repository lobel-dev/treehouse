package gitvcs

import (
	"errors"
	"testing"
)

func TestReturnReportOptionalFailures(t *testing.T) {
	wt, _, _ := setupSafeResetWorktree(t)
	mustGit(t, wt, "switch", "-c", "feature")
	run, err := pinnedReturnGit(wt)
	if err != nil {
		t.Fatal(err)
	}
	observed := false
	failing := func(path string, args ...string) (string, error) {
		if args[0] == "show" || args[0] == "status" {
			observed = true
			return "", errors.New("optional reporting unavailable")
		}
		return run(path, args...)
	}
	var report ReturnReport
	_, err = returnWorktreeUsing(failing, wt, "main", "", nil, nil, &report)
	if err != nil || !observed || !report.Parked || report.ChangesKnown || report.Subject != "" || report.AttachedBranch != "feature" {
		t.Fatalf("safe return failed optional reporting: %+v, %v", report, err)
	}
}

func TestReturnReportResetFailureDoesNotClaimParking(t *testing.T) {
	wt, _, _ := setupSafeResetWorktree(t)
	run, err := pinnedReturnGit(wt)
	if err != nil {
		t.Fatal(err)
	}
	failing := func(path string, args ...string) (string, error) {
		if args[0] == "read-tree" {
			return "", errors.New("reset failed")
		}
		return run(path, args...)
	}
	var report ReturnReport
	_, err = returnWorktreeUsing(failing, wt, "main", "", nil, nil, &report)
	if err == nil || report.Parked {
		t.Fatalf("false parking outcome: %+v, %v", report, err)
	}
}
