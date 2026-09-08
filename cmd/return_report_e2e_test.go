package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReturnReportsProtectedBranchE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	out, stderr, code := runTreehouse(t, repo, home, nil, "get", "--lease")
	if code != 0 {
		t.Fatalf("allocate: %s", stderr)
	}
	path := strings.TrimSpace(out)
	gitCmd(t, path, "switch", "-c", "feature/report")
	gitCmd(t, path, "commit", "--allow-empty", "-m", "Protected work")
	head := strings.TrimSpace(gitCmd(t, path, "rev-parse", "HEAD"))
	out, stderr, code = runTreehouse(t, repo, home, nil, "return", path)
	if code != 0 || out != "" {
		t.Fatalf("return: code=%d stdout=%q stderr=%s", code, out, stderr)
	}
	for _, want := range []string{"parked, reset to main", "Kept: feature/report", "Protected work", head[:12], "work " + quoteReturnPath("feature/report")} {
		if !strings.Contains(stderr, want) {
			t.Errorf("missing %q in %s", want, stderr)
		}
	}
	if got := strings.TrimSpace(gitCmd(t, repo, "rev-parse", "refs/heads/feature/report")); got != head {
		t.Fatalf("branch changed: %s", got)
	}
}

func TestReturnReportOutcomesE2E(t *testing.T) {
	for _, scenario := range []string{"detached-preserved", "detached-unpreserved", "dirty-force", "clean-force", "damaged"} {
		t.Run(scenario, func(t *testing.T) {
			repo, home := setupTestRepo(t)
			out, stderr, code := runTreehouse(t, repo, home, nil, "get", "--lease")
			if code != 0 {
				t.Fatalf("allocate: %s", stderr)
			}
			path := strings.TrimSpace(out)
			want := "Slot 1 parked, reset to main"
			switch scenario {
			case "detached-preserved", "detached-unpreserved":
				gitCmd(t, path, "commit", "--allow-empty", "-m", "Detached work")
				if scenario == "detached-preserved" {
					gitCmd(t, path, "tag", "saved-work")
					want = "Kept: refs/tags/saved-work"
				} else {
					want = "git -C " + quoteReturnPath(path) + " branch <new-name> HEAD"
				}
			case "dirty-force":
				gitCmd(t, path, "config", "status.showUntrackedFiles", "no")
				if err := os.WriteFile(filepath.Join(path, "README.md"), []byte("staged"), 0600); err != nil {
					t.Fatal(err)
				}
				gitCmd(t, path, "add", "README.md")
				if err := os.WriteFile(filepath.Join(path, "hidden"), []byte("untracked"), 0600); err != nil {
					t.Fatal(err)
				}
				want = "Changes observed before forced cleanup: 1 tracked paths, 1 untracked paths."
			case "damaged":
				if err := os.Remove(filepath.Join(path, ".git")); err != nil {
					t.Fatal(err)
				}
				want = "Slot 1 reservation cleared; damaged slot was not reset"
			}
			out, stderr, code = runTreehouse(t, repo, home, nil, "return", "--force", path)
			if scenario == "detached-unpreserved" {
				if code == 0 || strings.Contains(stderr, "Kept:") || strings.Contains(stderr, "parked,") {
					t.Fatalf("unsafe success: %s", stderr)
				}
			} else if code != 0 {
				t.Fatalf("return: %s", stderr)
			}
			if out != "" || !strings.Contains(stderr, want) {
				t.Fatalf("stdout=%q stderr=%s; want %s", out, stderr, want)
			}
			if scenario == "clean-force" && strings.Contains(stderr, "Changes observed") {
				t.Fatalf("invented dirty cleanup: %s", stderr)
			}
			if scenario == "detached-preserved" && strings.Contains(stderr, "Resume:") {
				t.Fatalf("tag presented as local branch: %s", stderr)
			}
		})
	}
}

func TestReturnReportConfirmedCleanupE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	out, stderr, code := runTreehouse(t, repo, home, nil, "get", "--lease")
	if code != 0 {
		t.Fatalf("allocate: %s", stderr)
	}
	path := strings.TrimSpace(out)
	scratch := filepath.Join(path, "scratch")
	if err := os.WriteFile(scratch, []byte("dirty"), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(treehouseBin, "return", path)
	command.Dir, command.Env = repo, buildEnv(home)
	command.Stdin = strings.NewReader("y\n")
	var stdout, diagnostic bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &diagnostic
	if err := command.Run(); err != nil {
		t.Fatalf("return: %v: %s", err, diagnostic.String())
	}
	if stdout.Len() != 0 || !strings.Contains(diagnostic.String(), "Changes observed before confirmed cleanup: 0 tracked paths, 1 untracked paths.") {
		t.Fatalf("incorrect confirmed report: stdout=%s stderr=%s", stdout.String(), diagnostic.String())
	}
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Fatalf("scratch was not cleaned: %v", err)
	}
}
