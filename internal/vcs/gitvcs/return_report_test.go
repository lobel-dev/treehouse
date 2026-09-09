package gitvcs

import (
	"errors"
	"os"
	"path/filepath"
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

func TestReturnReportChangedIdentityDoesNotClaimKeptWork(t *testing.T) {
	for _, identity := range []string{"HEAD", "refs/heads/feature"} {
		t.Run(identity, func(t *testing.T) {
			wt, base, _ := setupSafeResetWorktree(t)
			mustGit(t, wt, "switch", "-c", "feature")
			mustGit(t, wt, "commit", "--allow-empty", "-m", "work")
			marker := filepath.Join(wt, "scratch")
			if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			path, err := gitPath(wt, identity)
			if err != nil {
				t.Fatal(err)
			}
			report, err := ReturnWorktreeReport(wt, "main", "", nil, func() error {
				// Ordinary Git writers are tested against both locks separately.
				// Inject a changed identity at the preparation barrier to prove
				// the report cannot claim the earlier observation after a mismatch.
				return os.WriteFile(path, []byte(base+"\n"), 0600)
			})
			if err == nil || report.Parked || report.PriorHead != "" {
				t.Fatalf("false kept report: %+v %v", report, err)
			}
			if data, err := os.ReadFile(marker); err != nil || string(data) != "keep" {
				t.Fatalf("reset after identity changed: %s %v", data, err)
			}
		})
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
