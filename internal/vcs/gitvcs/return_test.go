package gitvcs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReturnWorktreeLocksDurableRefBeforePreparation(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(map[bool]string{false: "loose", true: "packed"}[packed], func(t *testing.T) {
			wt, base, _ := setupSafeResetWorktree(t)
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
			report, err := ReturnWorktreeReport(wt, "main", "", nil, func() error {
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
			if !report.Parked || report.TargetCommit != base || report.PriorHead != head || report.AttachedBranch != "feature" || report.PreservingRef != "refs/heads/feature" {
				t.Fatalf("incorrect protected report: %+v", report)
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

func TestReturnWorktreeIgnoresSyntheticAncestry(t *testing.T) {
	for _, rewrite := range []string{"replace", "grafts", "ambient-grafts"} {
		t.Run(rewrite, func(t *testing.T) {
			wt, base, _ := setupSafeResetWorktree(t)
			mustGit(t, wt, "commit", "--allow-empty", "-m", "unprotected work")
			head, err := worktreeHead(wt)
			if err != nil {
				t.Fatal(err)
			}
			switch rewrite {
			case "replace":
				replacement, err := runGit(wt, "commit-tree", base+"^{tree}", "-p", head, "-m", "synthetic ancestry")
				if err != nil {
					t.Fatal(err)
				}
				mustGit(t, wt, "replace", base, replacement)
			default:
				grafts, err := gitPath(wt, "info/grafts")
				if err != nil {
					t.Fatal(err)
				}
				if rewrite == "ambient-grafts" {
					grafts = filepath.Join(t.TempDir(), "grafts")
					t.Setenv("GIT_GRAFT_FILE", grafts)
				}
				if err := os.MkdirAll(filepath.Dir(grafts), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(grafts, []byte(base+" "+head+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			containing, err := runGit(wt, "for-each-ref", "--contains="+head, "--format=%(refname)", "refs/heads/")
			if err != nil || !strings.Contains(containing, "refs/heads/main") {
				t.Fatalf("fixture must forge main ancestry: %q %v", containing, err)
			}
			scratch := filepath.Join(wt, "scratch")
			if err := os.WriteFile(scratch, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			_, err = ReturnWorktree(wt, "main", "", nil, func() error { t.Error("preparation ran on unprotected commit"); return nil })
			if err == nil {
				t.Error("synthetic ancestry allowed unsafe return")
			}
			if got, _ := worktreeHead(wt); got != head {
				t.Error("HEAD changed")
			}
			if got, err := os.ReadFile(scratch); err != nil || string(got) != "keep" {
				t.Fatalf("scratch changed: %q %v", got, err)
			}
		})
	}
}

func TestReturnWorktreeDurableRefsSurviveFailedPreparation(t *testing.T) {
	for _, ref := range []string{"refs/tags/saved", "refs/remotes/origin/saved"} {
		t.Run(ref, func(t *testing.T) {
			wt, base, _ := setupSafeResetWorktree(t)
			mustGit(t, wt, "commit", "--allow-empty", "-m", "saved work")
			head, err := worktreeHead(wt)
			if err != nil {
				t.Fatal(err)
			}
			mustGit(t, wt, "update-ref", ref, head)
			preparationErr := errors.New("preparation failed")
			_, err = ReturnWorktree(wt, "main", "", nil, func() error { return preparationErr })
			if !errors.Is(err, preparationErr) {
				t.Fatalf("expected preparation failure, got %v", err)
			}
			if got, _ := worktreeHead(wt); got != head {
				t.Fatal("failed preparation moved HEAD")
			}
			// Both the containing ref lock and HEAD lock must be released on failure.
			mustGit(t, wt, "update-ref", ref, head)
			if _, err := ReturnWorktree(wt, "main", "", nil, nil); err != nil {
				t.Fatal(err)
			}
			if got, _ := worktreeHead(wt); got != base {
				t.Fatalf("not parked: %s", got)
			}
			if got, _ := runGit(wt, "rev-parse", ref); got != head {
				t.Fatalf("saved ref changed: %s", got)
			}
		})
	}
}

func TestReturnWorktreeRefLockFailures(t *testing.T) {
	for _, failure := range []string{"busy", "permission"} {
		t.Run(failure, func(t *testing.T) {
			wt, _, _ := setupSafeResetWorktree(t)
			mustGit(t, wt, "checkout", "-b", "saved/nested")
			mustGit(t, wt, "commit", "--allow-empty", "-m", "saved work")
			head, _ := worktreeHead(wt)
			refPath, err := gitPath(wt, "refs/heads/saved/nested")
			if err != nil {
				t.Fatal(err)
			}
			if failure == "busy" {
				if err := os.WriteFile(refPath+".lock", []byte("busy"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				mustGit(t, wt, "pack-refs", "--all", "--prune")
				if err := os.MkdirAll(filepath.Dir(refPath), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(filepath.Dir(refPath), 0500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(filepath.Dir(refPath), 0755) })
				probe, probeErr := os.OpenFile(refPath+".lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if probeErr == nil {
					_ = probe.Close()
					_ = os.Remove(refPath + ".lock")
					t.Skip("filesystem does not enforce directory permissions")
				}
			}
			scratch := filepath.Join(wt, "scratch")
			if err := os.WriteFile(scratch, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			_, err = ReturnWorktree(wt, "main", "", nil, func() error { t.Error("preparation ran without ref lock"); return nil })
			if err == nil || !strings.Contains(err.Error(), "cannot lock containing ref refs/heads/saved/nested") || strings.Contains(err.Error(), "not preserved") {
				t.Fatalf("expected ref lock diagnostic, got %v", err)
			}
			if failure == "busy" && !errors.Is(err, os.ErrExist) {
				t.Fatalf("lost lock contention cause: %v", err)
			}
			if failure == "permission" {
				var pathErr *os.PathError
				if !errors.As(err, &pathErr) {
					t.Fatalf("lost filesystem cause: %v", err)
				}
			}
			if got, _ := worktreeHead(wt); got != head {
				t.Fatal("HEAD changed")
			}
			if got, err := os.ReadFile(scratch); err != nil || string(got) != "keep" {
				t.Fatalf("scratch changed: %q %v", got, err)
			}
		})
	}
}

func TestReturnWorktreeRefLockUsesAlternateDetachedWitness(t *testing.T) {
	wt, _, _ := setupSafeResetWorktree(t)
	mustGit(t, wt, "commit", "--allow-empty", "-m", "saved work")
	mustGit(t, wt, "branch", "aaa-busy")
	mustGit(t, wt, "branch", "zzz-available")
	refPath, err := gitPath(wt, "refs/heads/aaa-busy")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(refPath+".lock", []byte("busy"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = ReturnWorktree(wt, "main", "", nil, func() error {
		if _, err := runGit(wt, "update-ref", "-d", "refs/heads/zzz-available"); err == nil {
			t.Error("alternate witness is not locked")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(refPath + ".lock"); err != nil || string(got) != "busy" {
		t.Fatalf("other writer lock changed: %q %v", got, err)
	}
}

func TestReturnWorktreePinsRepositoryAfterMarkerChange(t *testing.T) {
	for _, change := range []string{"removed", "replaced"} {
		t.Run(change, func(t *testing.T) {
			wt, _, _ := setupSafeResetWorktree(t)
			other, _, _ := setupSafeResetWorktree(t)
			mustGit(t, other, "commit", "--allow-empty", "-m", "different repository head")
			otherMarker, err := os.ReadFile(filepath.Join(other, ".git"))
			if err != nil {
				t.Fatal(err)
			}
			otherHead, _ := worktreeHead(other)
			scratch := filepath.Join(other, "scratch")
			if err := os.WriteFile(scratch, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			_, err = ReturnWorktree(wt, "main", "", nil, func() error {
				marker := filepath.Join(wt, ".git")
				if change == "replaced" {
					return os.WriteFile(marker, otherMarker, 0600)
				}
				return os.Remove(marker)
			})
			if err != nil {
				t.Fatalf("pinned return should still address original worktree: %v", err)
			}
			if got, _ := worktreeHead(other); got != otherHead {
				t.Fatal("other HEAD changed")
			}
			if got, err := os.ReadFile(scratch); err != nil || string(got) != "keep" {
				t.Fatalf("other scratch changed: %q %v", got, err)
			}
		})
	}
}

func TestReturnWorktreeIgnoresAmbientRepositoryLocations(t *testing.T) {
	wt, base, _ := setupSafeResetWorktree(t)
	other, _, _ := setupSafeResetWorktree(t)
	mustGit(t, wt, "checkout", "-b", "saved")
	mustGit(t, wt, "commit", "--allow-empty", "-m", "saved work")
	headPath, err := gitPath(wt, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	otherDir, err := returnGitDir(other)
	if err != nil {
		t.Fatal(err)
	}
	otherCommon, err := runGit(other, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		t.Fatal(err)
	}
	otherIndex, err := gitPath(other, "index")
	if err != nil {
		t.Fatal(err)
	}
	indexBefore, err := os.ReadFile(otherIndex)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{wt, other} {
		if err := os.WriteFile(filepath.Join(dir, "scratch"), []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GIT_DIR", otherDir)
	t.Setenv("GIT_WORK_TREE", other)
	t.Setenv("GIT_COMMON_DIR", otherCommon)
	t.Setenv("GIT_INDEX_FILE", otherIndex)
	t.Setenv("GIT_OBJECT_DIRECTORY", filepath.Join(otherCommon, "objects"))
	t.Setenv("GIT_ALTERNATE_OBJECT_DIRECTORIES", filepath.Join(t.TempDir(), "missing"))
	_, err = ReturnWorktree(wt, "main", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if head, err := os.ReadFile(headPath); err != nil || strings.TrimSpace(string(head)) != base {
		t.Fatalf("wrong slot HEAD: %q %v", head, err)
	}
	if _, err := os.Stat(filepath.Join(wt, "scratch")); !os.IsNotExist(err) {
		t.Fatalf("slot was not cleaned: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(other, "scratch")); err != nil || string(got) != "keep" {
		t.Fatalf("other scratch changed: %q %v", got, err)
	}
	if got, err := os.ReadFile(otherIndex); err != nil || string(got) != string(indexBefore) {
		t.Fatalf("other index changed: %v", err)
	}
}

func TestReturnWorktreeWithRelativeGitLinks(t *testing.T) {
	wt, _, _ := setupSafeResetWorktree(t)
	if _, err := runGit(wt, "worktree", "repair", "--relative-paths", wt); err != nil {
		t.Skipf("Git lacks relative worktree links: %v", err)
	}
	marker, err := os.ReadFile(filepath.Join(wt, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	target := strings.TrimSpace(strings.TrimPrefix(string(marker), "gitdir: "))
	if filepath.IsAbs(target) {
		t.Fatalf("fixture did not create relative gitfile: %s", marker)
	}
	if _, err := ReturnWorktree(wt, "main", "", nil, nil); err != nil {
		t.Fatal(err)
	}
}
