package gitvcs

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestListWorkBranchStates(t *testing.T) {
	wt, _, _ := setupSafeResetWorktree(t)
	repo, err := FindMainRepoRootFrom(wt)
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, wt, "switch", "-c", "feature/held")
	mustGit(t, repo, "worktree", "lock", wt)
	for _, ref := range []string{"refs/remotes/origin/main", "refs/remotes/origin/remote-only", "refs/remotes/upstream/excluded", "refs/tags/excluded"} {
		mustGit(t, repo, "update-ref", ref, "HEAD")
	}
	mustGit(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	for _, missing := range []bool{false, true} {
		if missing {
			if err := os.RemoveAll(wt); err != nil {
				t.Fatal(err)
			}
		}
		before := mustGitOutput(t, repo, "worktree", "list", "--porcelain")
		branches, err := ListWorkBranchStates(repo)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, branch := range branches {
			names = append(names, branch.Name)
			facts, err := InspectBranch(repo, branch.Name)
			if err != nil || !reflect.DeepEqual(branch.Holders, facts.Holders) {
				t.Fatalf("bulk/single holder mismatch for %s: %+v %+v %v", branch.Name, branch.Holders, facts.Holders, err)
			}
			if branch.Name == "feature/held" && (len(branch.Holders) != 1 || !branch.Holders[0].Locked || branch.Holders[0].Missing != missing) {
				t.Fatalf("holder flags: %+v", branch.Holders)
			}
		}
		if want := []string{"feature/held", "main"}; !reflect.DeepEqual(names, want) {
			t.Fatalf("names: %v want %v", names, want)
		}
		if after := mustGitOutput(t, repo, "worktree", "list", "--porcelain"); before != after {
			t.Fatal("listing mutated registrations")
		}
	}
}

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
