package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkCreatesLiteralBranchE2E(t *testing.T) {
	for _, branch := range []string{"feature/work", "123"} {
		t.Run(branch, func(t *testing.T) {
			repo, home := setupTestRepo(t)
			out, stderr, code := runTreehouse(t, repo, home, []string{"SHELL=" + exitShellBin, "COMSPEC=" + exitShellBin}, "work", branch)
			if code != 0 {
				t.Fatalf("work: %s", stderr)
			}
			if out != "" || !strings.Contains(stderr, "Kept: "+branch) {
				t.Fatalf("missing branch outcome: stdout=%s stderr=%s", out, stderr)
			}
			gitCmd(t, repo, "show-ref", "--verify", "refs/heads/"+branch)
		})
	}
}

func TestWorkBranchDecisionsE2E(t *testing.T) {
	for _, kind := range []string{"local", "origin-after-fetch", "main-checkout", "external-checkout", "leased", "dirty-reclaim", "shell-failure", "no-argument", "invalid"} {
		t.Run(kind, func(t *testing.T) {
			repo, home := setupTestRepo(t)
			branch := "feature/resume"
			args := []string{"work", branch}
			env := []string{"SHELL=" + exitShellBin, "COMSPEC=" + exitShellBin}
			wantCode, wantText := 0, "Kept: "+branch
			var slot string
			switch kind {
			case "local":
				gitCmd(t, repo, "branch", branch)
			case "origin-after-fetch":
				gitCmd(t, repo, "push", "origin", "HEAD:refs/heads/"+branch)
				gitCmd(t, repo, "update-ref", "-d", "refs/remotes/origin/"+branch)
			case "main-checkout":
				gitCmd(t, repo, "switch", "-c", branch)
				wantCode, wantText = 1, repo
			case "external-checkout":
				slot = filepath.Join(filepath.Dir(repo), "external")
				gitCmd(t, repo, "worktree", "add", "-b", branch, slot)
				wantCode, wantText = 1, slot
			case "leased":
				out, stderr, code := runTreehouse(t, repo, home, nil, "get", "--lease")
				if code != 0 {
					t.Fatal(stderr)
				}
				slot = strings.TrimSpace(out)
				gitCmd(t, slot, "switch", "-c", branch)
				wantCode, wantText = 1, "leased"
			case "dirty-reclaim":
				slot = idleReportingSlot(t, repo, home)
				gitCmd(t, slot, "switch", "-c", branch)
				gitCmd(t, slot, "commit", "--allow-empty", "-m", "unmerged work")
				if err := os.WriteFile(filepath.Join(slot, "scratch"), []byte("keep me"), 0600); err != nil {
					t.Fatal(err)
				}
				// EOF at the cleanup prompt must leave dirty work intact.
				wantCode, wantText = 1, "Clean worktree and return"
			case "shell-failure":
				env = []string{"SHELL=" + filepath.Join(home, "missing-shell"), "COMSPEC=" + filepath.Join(home, "missing-shell")}
				wantCode, wantText = 1, "attempting guarded return"
			case "no-argument":
				args, wantText = []string{"work"}, "parked"
			case "invalid":
				args, wantCode, wantText = []string{"work", "@{-1}"}, 1, "invalid literal branch"
			}
			out, stderr, code := runTreehouse(t, repo, home, env, args...)
			if code != wantCode || out != "" || !strings.Contains(stderr, wantText) {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, out, stderr)
			}
			if kind == "origin-after-fetch" {
				got := gitCmd(t, repo, "for-each-ref", "--format=%(upstream)", "refs/heads/"+branch)
				if strings.TrimSpace(got) != "refs/remotes/origin/"+branch {
					t.Fatalf("upstream: %s", got)
				}
			}
			if kind == "dirty-reclaim" {
				data, err := os.ReadFile(filepath.Join(slot, "scratch"))
				if err != nil || string(data) != "keep me" {
					t.Fatalf("dirty file lost: %s %v", data, err)
				}
				if !strings.Contains(stderr, "existing changes remain in place") {
					t.Fatal(stderr)
				}
				if got := strings.TrimSpace(gitCmd(t, slot, "symbolic-ref", "--short", "HEAD")); got != branch {
					t.Fatalf("branch changed: %s", got)
				}
			}
			if kind == "shell-failure" {
				gitCmd(t, repo, "show-ref", "--verify", "refs/heads/"+branch)
				if !strings.Contains(stderr, "parked") {
					t.Fatal(stderr)
				}
			}
		})
	}
}

func TestWorkJJUnsupportedE2E(t *testing.T) {
	requireJJ(t)
	repo, home := setupJJTestRepo(t)
	_, stderr, code := runTreehouse(t, repo, home, nil, "work", "feature/jj")
	if code != 2 || !strings.Contains(stderr, "work requires the Git backend; use treehouse get") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}

func TestWorkStaleRegistrationE2E(t *testing.T) {
	for _, locked := range []bool{false, true} {
		t.Run(map[bool]string{false: "stale", true: "locked"}[locked], func(t *testing.T) {
			repo, home := setupTestRepo(t)
			idleReportingSlot(t, repo, home) // Force the normal allocation to reuse.
			missing := filepath.Join(filepath.Dir(repo), "missing")
			gitCmd(t, repo, "worktree", "add", "-b", "feature/stale", missing)
			metadata := strings.TrimSpace(gitCmd(t, missing, "rev-parse", "--absolute-git-dir"))
			if locked {
				gitCmd(t, repo, "worktree", "lock", missing)
			}
			if err := os.RemoveAll(missing); err != nil {
				t.Fatal(err)
			}
			_, stderr, code := runTreehouse(t, repo, home, []string{"SHELL=" + exitShellBin, "COMSPEC=" + exitShellBin}, "work", "--no-fetch", "feature/stale")
			if locked {
				if code != 1 || !strings.Contains(stderr, "locked missing worktree") {
					t.Fatalf("code=%d stderr=%s", code, stderr)
				}
				if _, err := os.Stat(filepath.Join(metadata, "locked")); err != nil {
					t.Fatalf("lock metadata removed: %v", err)
				}
				if _, err := os.Stat(filepath.Join(metadata, "HEAD")); err != nil {
					t.Fatalf("registration removed: %v", err)
				}
			} else {
				if code != 0 {
					t.Fatalf("work: %s", stderr)
				}
				if _, err := os.Stat(metadata); !os.IsNotExist(err) {
					t.Fatalf("stale metadata remains: %v", err)
				}
			}
		})
	}
}
