package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func idleReportingSlot(t *testing.T, repo, home string, args ...string) string {
	t.Helper()
	out, stderr, code := runTreehouse(t, repo, home, nil, append([]string{"get", "--lease"}, args...)...)
	if code != 0 {
		t.Fatalf("get: %s", stderr)
	}
	path := strings.TrimSpace(out)
	_, stderr, code = runTreehouse(t, repo, home, nil, "return", path)
	if code != 0 {
		t.Fatalf("return: %s", stderr)
	}
	return path
}

func TestPruneDetachedRemediesE2E(t *testing.T) {
	for _, preserved := range []bool{false, true} {
		t.Run(map[bool]string{false: "unpreserved", true: "preserved"}[preserved], func(t *testing.T) {
			repo, home := setupTestRepo(t)
			path := idleReportingSlot(t, repo, home)
			gitCmd(t, path, "commit", "--allow-empty", "-m", "detached work")
			if preserved {
				gitCmd(t, path, "tag", "saved-work")
			}
			statePath := filepath.Join(filepath.Dir(filepath.Dir(path)), "treehouse-state.json")
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			out, stderr, code := runTreehouse(t, repo, home, nil, "prune")
			if code != 0 {
				t.Fatalf("prune: %s", stderr)
			}
			want := "detached HEAD; HEAD has no preserving branch, tag, or remote ref."
			if preserved {
				want = "detached HEAD; HEAD preserved by refs/tags/saved-work"
			}
			if !strings.Contains(out, want) {
				t.Fatalf("missing %s: %s", want, out)
			}
			if strings.Contains(out, "Preserve first:") == preserved {
				t.Fatalf("incorrect preservation remedy: %s", out)
			}
			after, err := os.ReadFile(statePath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("preview changed state: %v", err)
			}
		})
	}
}

func TestPruneRecordedBaseRemedyE2E(t *testing.T) {
	for _, absent := range []bool{false, true} {
		t.Run(map[bool]string{false: "not-merged", true: "unknown"}[absent], func(t *testing.T) {
			repo, home := setupTestRepo(t)
			gitCmd(t, repo, "branch", "develop")
			path := idleReportingSlot(t, repo, home, "--base", "develop")
			gitCmd(t, path, "switch", "-c", "feature/base")
			gitCmd(t, path, "commit", "--allow-empty", "-m", "beyond base")
			if absent {
				gitCmd(t, repo, "branch", "-D", "develop")
			}
			out, stderr, code := runTreehouse(t, repo, home, nil, "prune")
			if code != 0 {
				t.Fatalf("prune: %s", stderr)
			}
			want := "Recorded base develop (refs/heads/develop): not merged"
			if absent {
				want = "Recorded base develop (ref unavailable): unknown"
			}
			if !strings.Contains(out, want) || !strings.Contains(out, "refs/remotes/origin/main") {
				t.Fatalf("incorrect comparisons: %s", out)
			}
			if strings.Contains(out, "fatal:") {
				t.Fatalf("raw error in non-verbose output: %s", out)
			}
		})
	}
}

func TestDestroyOverlappingRiskRemedyE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	out, stderr, code := runTreehouse(t, repo, home, nil, "get", "--lease")
	if code != 0 {
		t.Fatalf("get: %s", stderr)
	}
	path := strings.TrimSpace(out)
	if err := os.WriteFile(filepath.Join(path, "dirty"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	out, stderr, code = runTreehouse(t, repo, home, nil, "destroy", repo, "--all", "--include-unlanded")
	if code != 0 {
		t.Fatalf("destroy: %s", stderr)
	}
	want := "treehouse destroy " + quoteReturnPath(path) + " --include-leased --include-unlanded"
	if !strings.Contains(out, want) || !strings.Contains(out, "never removed by --all") {
		t.Fatalf("incomplete overlap remedy: %s", out)
	}
	if got, err := os.ReadFile(filepath.Join(path, "dirty")); err != nil || string(got) != "keep" {
		t.Fatalf("preview changed files: %q %v", got, err)
	}
}

func TestPruneUnknownIdentityRemedyE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	path := idleReportingSlot(t, repo, home)
	if err := os.WriteFile(filepath.Join(path, ".git"), []byte("invalid marker\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, stderr, code := runTreehouse(t, repo, home, nil, "prune")
	if code != 0 {
		t.Fatalf("prune: %s", stderr)
	}
	if !strings.Contains(out, "commit preservation unknown") || strings.Contains(out, "Preserve first:") || strings.Contains(out, "HEAD has no preserving") {
		t.Fatalf("unknown mistaken for absent: %s", out)
	}
}

func TestJJPruneRemedyUsesLifecycleCommandsE2E(t *testing.T) {
	requireJJ(t)
	repo, home := setupJJTestRepo(t)
	path := idleReportingSlot(t, repo, home)
	if err := os.WriteFile(filepath.Join(path, "dirty"), []byte("jj work"), 0600); err != nil {
		t.Fatal(err)
	}
	out, stderr, code := runTreehouse(t, repo, home, nil, "prune")
	if code != 0 {
		t.Fatalf("prune: %s", stderr)
	}
	if !strings.Contains(out, "treehouse destroy "+quoteReturnPath(path)+" --include-unlanded") || !strings.Contains(out, "writable shell") {
		t.Fatalf("missing lifecycle remedy: %s", out)
	}
	for _, forbidden := range []string{"git -C", "git switch", "HEAD preserved", "delete its branch", "detached HEAD"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("Git-only jj remedy: %s", out)
		}
	}
}

func TestPruneExplainsPreservedBranchE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	// Lease allocation exposes the path; return it to leave an idle slot.
	out, stderr, code := runTreehouse(t, repo, home, nil, "get", "--lease")
	if code != 0 {
		t.Fatalf("lease: %s", stderr)
	}
	path := strings.TrimSpace(out)
	_, stderr, code = runTreehouse(t, repo, home, nil, "return", path)
	if code != 0 {
		t.Fatalf("return: %s", stderr)
	}
	gitCmd(t, path, "switch", "-c", "feature/remedy")
	gitCmd(t, path, "commit", "--allow-empty", "-m", "preserved work")
	head := gitCmd(t, path, "rev-parse", "HEAD")
	out, stderr, code = runTreehouse(t, repo, home, nil, "prune")
	if code != 0 {
		t.Fatalf("prune: %s", stderr)
	}
	for _, want := range []string{"feature/remedy", "refs/remotes/origin/main", "preserved by refs/heads/feature/remedy", "treehouse destroy " + quoteReturnPath(path) + " --include-unlanded", "writable shell"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	poolDir := filepath.Dir(filepath.Dir(path))
	root := filepath.Dir(filepath.Dir(poolDir))
	selector := filepath.ToSlash(filepath.Join(filepath.Base(poolDir), "1"))
	hint := "treehouse --root " + quoteReturnPath(root) + " enter " + quoteReturnPath(selector)
	if !strings.Contains(out, hint) {
		t.Fatalf("missing qualified enter command: %s", out)
	}
	_, stderr, code = runTreehouseFromDir(t, repo, home, home, []string{"SHELL=" + exitShellBin, "COMSPEC=" + exitShellBin}, "--root", root, "enter", selector)
	if code != 0 {
		t.Fatalf("printed enter command failed from outside repo: %s", stderr)
	}
	if got := gitCmd(t, path, "rev-parse", "HEAD"); got != head {
		t.Fatalf("prune changed HEAD: %s", got)
	}
}
