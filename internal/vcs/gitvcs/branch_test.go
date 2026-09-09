package gitvcs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBranchReclamationLocksIdentity(t *testing.T) {
	wt, base, _ := setupSafeResetWorktree(t)
	repo, err := FindMainRepoRootFrom(wt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(wt, "switch", "-c", "feature/work"); err != nil {
		t.Fatal(err)
	}
	called := false
	err = WithBranchIdentity(repo, wt, "feature/work", func() error {
		called = true
		if _, err := runGit(wt, "switch", "--detach", base); err == nil {
			t.Fatal("checkout bypassed HEAD lock")
		}
		if _, err := runGit(wt, "update-ref", "-d", "refs/heads/feature/work"); err == nil {
			t.Fatal("ref deletion bypassed branch lock")
		}
		return nil
	})
	if err != nil || !called {
		t.Fatalf("reclamation: called=%v err=%v", called, err)
	}
	if _, err := runGit(wt, "switch", "--detach", base); err != nil {
		t.Fatalf("lock leaked: %v", err)
	}
	if err := WithBranchIdentity(repo, wt, "feature/work", func() error { t.Fatal("reserved changed HEAD"); return nil }); err == nil {
		t.Fatal("changed HEAD accepted")
	}
}

func TestBranchReclamationLockContention(t *testing.T) {
	for _, which := range []string{"HEAD", "refs/heads/feature/work"} {
		t.Run(strings.ReplaceAll(which, "/", "-"), func(t *testing.T) {
			wt, _, _ := setupSafeResetWorktree(t)
			repo, err := FindMainRepoRootFrom(wt)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := runGit(wt, "switch", "-c", "feature/work"); err != nil {
				t.Fatal(err)
			}
			path, err := gitPath(wt, which)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path+".lock", []byte("other writer"), 0600); err != nil {
				t.Fatal(err)
			}
			err = WithBranchIdentity(repo, wt, "feature/work", func() error { t.Fatal("reserved despite contention"); return nil })
			if err == nil {
				t.Fatal("contention ignored")
			}
			if data, err := os.ReadFile(path + ".lock"); err != nil || string(data) != "other writer" {
				t.Fatalf("other writer lock touched: %s %v", data, err)
			}
		})
	}
}
