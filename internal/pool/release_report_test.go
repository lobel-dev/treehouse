package pool

import (
	"os"
	"strings"
	"testing"
)

func TestReleaseReportStateFailureAfterParking(t *testing.T) {
	repo, dir := setupRepo(t)
	path, err := Acquire(repo, dir, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, path, "switch", "-c", "feature")
	runGit(t, path, "commit", "--allow-empty", "-m", "kept work")
	head := gitOut(t, path, "rev-parse", "HEAD")
	report, err := ReleaseConditionalReport(dir, path, "", ReleasePreconditions{}, func() error {
		statePath := stateFilePath(dir)
		if err := os.Rename(statePath, statePath+".saved"); err != nil {
			return err
		}
		return os.Mkdir(statePath, 0700)
	})
	if err == nil || !strings.Contains(err.Error(), "was parked, but pool state could not be saved") || report.Parked {
		t.Fatalf("incorrect partial outcome: %+v, %v", report, err)
	}
	if got := gitOut(t, path, "rev-parse", "HEAD"); got != gitOut(t, repo, "rev-parse", "main") {
		t.Fatalf("not parked: %s", got)
	}
	if got := gitOut(t, repo, "rev-parse", "feature"); got != head {
		t.Fatalf("lost protected work: %s", got)
	}
}
