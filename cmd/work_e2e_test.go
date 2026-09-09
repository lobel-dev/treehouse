package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kunchenguid/treehouse/internal/pool"
)

func TestWorkNewBranchKeepsAcquiredBaseE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	base := strings.TrimSpace(gitCmd(t, repo, "rev-parse", "HEAD"))
	// Advance the main checkout after allocation, before work switches. The
	// new branch must start at the commit acquired, not re-resolve the base.
	hook := "git -C " + quoteReturnPath(repo) + " commit --allow-empty -m after-allocation"
	quoted, err := json.Marshal(hook)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(home, ".config", "treehouse", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("[hooks]\npost_create = ["+string(quoted)+"]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runTreehouse(t, repo, home, []string{"SHELL=" + exitShellBin}, "work", "--no-fetch", "feature/fixed-base")
	if code != 0 {
		t.Fatal(stderr)
	}
	if got := strings.TrimSpace(gitCmd(t, repo, "rev-parse", "refs/heads/main")); got == base {
		t.Fatalf("hook did not advance main: %s", stderr)
	}
	if got := strings.TrimSpace(gitCmd(t, repo, "rev-parse", "refs/heads/feature/fixed-base")); got != base {
		t.Fatalf("work re-resolved moving base: got=%s acquired=%s", got, base)
	}
}

func TestWorkNoFetchUsesOnlyKnownOriginRefsE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	base := strings.TrimSpace(gitCmd(t, repo, "rev-parse", "HEAD"))
	gitCmd(t, repo, "switch", "-c", "remote-seed")
	gitCmd(t, repo, "commit", "--allow-empty", "-m", "remote-only")
	gitCmd(t, repo, "push", "origin", "HEAD:refs/heads/feature/no-fetch")
	gitCmd(t, repo, "switch", "main")
	gitCmd(t, repo, "update-ref", "-d", "refs/remotes/origin/feature/no-fetch")
	_, stderr, code := runTreehouse(t, repo, home, []string{"SHELL=" + exitShellBin}, "work", "--no-fetch", "feature/no-fetch")
	if code != 0 {
		t.Fatal(stderr)
	}
	if got := strings.TrimSpace(gitCmd(t, repo, "rev-parse", "refs/heads/feature/no-fetch")); got != base {
		t.Fatalf("no-fetch discovered remote: %s", got)
	}
}

func TestLsMixedGitSlotDoesNotSuggestUnsupportedWorkE2E(t *testing.T) {
	requireJJ(t)
	repo, home := setupColocatedRepoWithoutOptIn(t)
	path := idleReportingSlot(t, repo, home)
	gitCmd(t, path, "switch", "-c", "feature/mixed")
	_, stderr, code := runTreehouse(t, repo, home, nil, "return", path)
	if code != 0 {
		t.Fatal(stderr)
	}
	out, stderr, code := runTreehouse(t, repo, home, []string{"TREEHOUSE_VCS=jj"}, "ls")
	if code != 0 || strings.Contains(out, " work ") {
		t.Fatalf("unsupported work suggestion: code=%d stdout=%s stderr=%s", code, out, stderr)
	}
}

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

func TestWorkRefusesIndirectBranchIdentityE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	path := idleReportingSlot(t, repo, home)
	gitCmd(t, repo, "branch", "feature/saved")
	gitCmd(t, repo, "symbolic-ref", "refs/heads/alias", "refs/heads/feature/saved")
	gitCmd(t, path, "symbolic-ref", "HEAD", "refs/heads/alias")
	_, stderr, code := runTreehouse(t, repo, home, []string{"SHELL=" + exitShellBin}, "work", "--no-fetch", "feature/saved")
	if code != 1 || strings.Contains(stderr, "Reserved the existing branch slot") {
		t.Fatalf("reclaimed indirectly attached branch without locking its alias: %d %s", code, stderr)
	}
}

func TestWorkBranchDecisionsE2E(t *testing.T) {
	for _, kind := range []string{"local", "origin-after-fetch", "main-checkout", "external-checkout", "leased", "dirty-reclaim", "shell-failure", "no-argument-shell-failure", "no-argument", "invalid"} {
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
				wantCode, wantText = 1, "Discard uncommitted changes and return"
			case "shell-failure":
				env = []string{"SHELL=" + filepath.Join(home, "missing-shell"), "COMSPEC=" + filepath.Join(home, "missing-shell")}
				wantCode, wantText = 1, "attempting guarded return"
			case "no-argument-shell-failure":
				args = []string{"work", "--no-fetch"}
				env = []string{"SHELL=" + filepath.Join(home, "missing-shell")}
				wantCode, wantText = 1, "missing-shell"
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
			if kind == "shell-failure" || kind == "no-argument-shell-failure" {
				if kind == "shell-failure" {
					gitCmd(t, repo, "show-ref", "--verify", "refs/heads/"+branch)
				}
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

func TestWorkHealsStaleDestroyReservationE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	path := idleReportingSlot(t, repo, home)
	gitCmd(t, path, "switch", "-c", "feature/interrupted-prune")
	dir := filepath.Dir(filepath.Dir(path))
	state, err := pool.ReadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	state.Worktrees[0].Destroying = true
	state.Worktrees[0].OwnerPID = int32(os.Getpid())
	state.Worktrees[0].OwnerStartedAt = 1 // Deliberately expired process identity.
	if err := pool.WriteState(dir, state); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runTreehouse(t, repo, home, []string{"SHELL=" + exitShellBin}, "work", "--no-fetch", "feature/interrupted-prune")
	if code != 0 || !strings.Contains(stderr, "Reserved the existing branch slot") || !strings.Contains(stderr, "parked") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	state, err = pool.ReadState(dir)
	if err != nil || len(state.Worktrees) != 1 || state.Worktrees[0].Destroying || state.Worktrees[0].OwnerPID != 0 {
		t.Fatalf("reservation not healed and returned: %+v %v", state, err)
	}
}
