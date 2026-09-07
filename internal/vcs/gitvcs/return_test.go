package gitvcs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReturnWorktreeLocksDurableRefBeforePreparation(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(map[bool]string{false: "loose", true: "packed"}[packed], func(t *testing.T) {
			wt, _, _ := setupSafeResetWorktree(t)
			mustGit(t, wt, "checkout", "-b", "feature")
			mustGit(t, wt, "commit", "--allow-empty", "-m", "branch work")
			mustGit(t, wt, "branch", "aaa-other-witness")
			head, err := worktreeHead(wt)
			if err != nil {
				t.Fatal(err)
			}
			if packed {
				mustGit(t, wt, "pack-refs", "--all", "--prune")
			}
			called := false
			_, err = ReturnWorktree(wt, "main", "", nil, func() error {
				called = true
				repo, err := FindMainRepoRootFrom(wt)
				if err != nil {
					return err
				}
				if _, err := runGit(repo, "update-ref", "refs/heads/feature", "main"); err == nil {
					t.Error("attached branch update bypassed lock")
				}
				if _, err := runGit(wt, "update-ref", "-d", "refs/heads/feature"); err == nil {
					t.Error("witness deletion bypassed lock")
				}
				if _, err := runGit(wt, "commit", "--allow-empty", "-m", "racing commit"); err == nil {
					t.Error("commit bypassed lock")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("preparation not called")
			}
			if got, _ := runGit(wt, "rev-parse", "feature"); got != head {
				t.Fatalf("branch changed: %s", got)
			}
		})
	}
}

func TestReturnWorktreeRefusalPreservesFilesAndSkipsPreparation(t *testing.T) {
	wt, _, _ := setupSafeResetWorktree(t)
	mustGit(t, wt, "commit", "--allow-empty", "-m", "detached work")
	head, err := worktreeHead(wt)
	if err != nil {
		t.Fatal(err)
	}
	// A symbolic ref to HEAD must not pass as durable protection.
	mustGit(t, wt, "symbolic-ref", "refs/heads/transient", "HEAD")
	marker := filepath.Join(wt, "scratch")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = ReturnWorktree(wt, "main", "", nil, func() error { t.Error("preparation ran on unsafe commit"); return nil })
	if err == nil {
		t.Fatal("unsafe return succeeded")
	}
	if got, _ := worktreeHead(wt); got != head {
		t.Fatal("HEAD changed")
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "keep" {
		t.Fatalf("scratch changed: %q %v", got, err)
	}
}

func TestReturnWorktreeRefusesReftableBeforePreparation(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if _, err := runGit("", "init", "--ref-format=reftable", "--initial-branch=main", repo); err != nil {
		t.Skipf("git lacks reftable: %v", err)
	}
	mustGit(t, repo, "config", "user.email", "test@test.com")
	mustGit(t, repo, "config", "user.name", "Test")
	mustGit(t, repo, "commit", "--allow-empty", "-m", "initial")
	wt := filepath.Join(t.TempDir(), "worktree")
	mustGit(t, repo, "worktree", "add", "--detach", wt, "main")
	_, err := ReturnWorktree(wt, "main", "", nil, func() error { t.Error("preparation ran with unsupported ref storage"); return nil })
	if err == nil || !strings.Contains(err.Error(), "unsupported Git ref storage") {
		t.Fatalf("expected explicit unsupported ref storage refusal, got %v", err)
	}
}
