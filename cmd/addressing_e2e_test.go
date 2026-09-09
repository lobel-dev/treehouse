package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitSlotAddressingE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	out, stderr, code := runTreehouse(t, repo, home, nil, "get", "--lease")
	if code != 0 {
		t.Fatal(stderr)
	}
	path := strings.TrimSpace(out)
	_, stderr, code = runTreehouse(t, repo, home, nil, "return", "--slot", "1")
	if code != 0 || !strings.Contains(stderr, "Slot 1 parked") {
		t.Fatalf("return --slot: %d %s", code, stderr)
	}
	_, stderr, code = runTreehouse(t, repo, home, nil, "destroy", "--slot", "1", "--yes")
	if code != 0 {
		t.Fatalf("destroy --slot: %s", stderr)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("slot not removed: %v", err)
	}
}

func TestEnterBranchAddressingE2E(t *testing.T) {
	for _, branch := range []string{"feature/slash", "123"} {
		t.Run(branch, func(t *testing.T) {
			repo, home := setupTestRepo(t)
			path := idleReportingSlot(t, repo, home)
			gitCmd(t, path, "switch", "-c", branch)
			statePath := filepath.Join(filepath.Dir(filepath.Dir(path)), "treehouse-state.json")
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			out, stderr, code := runTreehouse(t, repo, home, nil, "enter", "--branch", branch, "--print-path")
			if code != 0 || strings.TrimSpace(out) != path {
				t.Fatalf("enter branch: %d %s %s", code, out, stderr)
			}
			after, err := os.ReadFile(statePath)
			if err != nil || string(before) != string(after) {
				t.Fatalf("enter branch wrote state: %v", err)
			}
			_, stderr, code = runTreehouse(t, repo, home, nil, "return", path)
			if code != 0 {
				t.Fatal(stderr)
			}
			_, stderr, code = runTreehouse(t, repo, home, nil, "enter", "--branch", branch, "--print-path")
			if code != 1 || !strings.Contains(stderr, "work "+quoteReturnPath(branch)) {
				t.Fatalf("history resolved as checkout: %d %s", code, stderr)
			}
		})
	}
}

func TestAddressingRefusalsPreserveLeaseE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	out, stderr, code := runTreehouse(t, repo, home, nil, "get", "--lease")
	if code != 0 {
		t.Fatal(stderr)
	}
	path := strings.TrimSpace(out)
	statePath := filepath.Join(filepath.Dir(filepath.Dir(path)), "treehouse-state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"return", "1"}, // Missing relative path must not mean slot 1.
		{"return", "--slot", "1", path},
		{"destroy", "--slot", "1", path},
		{"destroy", "--slot", "1", "--include-leased", "--yes"},
		{"destroy", "--slot", "1", "--all", "--yes"},
		{"enter", "--branch", "123", "1"},
		{"return", "--slot", "../1"},
		{"return", "--slot", "pool/1"},
		{"return", "--slot", ""},
		{"return", "--slot", "1", "--if-lease-id", "wrong"},
	} {
		_, stderr, code = runTreehouse(t, repo, home, nil, args...)
		if code == 0 {
			t.Fatalf("accepted conflicting/unsafe target %v: %s", args, stderr)
		}
		after, err := os.ReadFile(statePath)
		if err != nil || string(before) != string(after) {
			t.Fatalf("%v changed lease state: %v", args, err)
		}
	}
	// The exact path keeps the existing explicit leased-destruction policy.
	_, stderr, code = runTreehouse(t, repo, home, nil, "destroy", path, "--include-leased", "--yes")
	if code != 0 {
		t.Fatalf("exact path no longer works: %s", stderr)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("exact path not removed: %v", err)
	}
}

func TestEnterBranchMissingHolderE2E(t *testing.T) {
	for _, locked := range []bool{false, true} {
		name := "unlocked"
		if locked {
			name = "locked"
		}
		t.Run(name, func(t *testing.T) {
			repo, home := setupTestRepo(t)
			path := idleReportingSlot(t, repo, home)
			branch := "feature/stale"
			gitCmd(t, path, "switch", "-c", branch)
			other := filepath.Join(filepath.Dir(repo), "stale")
			gitCmd(t, repo, "worktree", "add", "--force", other, branch)
			if locked {
				gitCmd(t, repo, "worktree", "lock", other)
			}
			if err := os.RemoveAll(other); err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(filepath.Dir(filepath.Dir(path)), "treehouse-state.json")
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			registrations := gitCmd(t, repo, "worktree", "list", "--porcelain")
			out, stderr, code := runTreehouse(t, repo, home, nil, "enter", "--branch", branch, "--print-path")
			if code != 0 || strings.TrimSpace(out) != path {
				t.Fatalf("missing holder blocked live checkout: %d %s %s", code, out, stderr)
			}
			if after := gitCmd(t, repo, "worktree", "list", "--porcelain"); after != registrations {
				t.Fatal("enter changed worktree registrations")
			}
			after, err := os.ReadFile(statePath)
			if err != nil || string(before) != string(after) {
				t.Fatalf("enter changed state: %v", err)
			}

			// With no live checkout, stale registrations must not resolve a slot.
			if err := os.RemoveAll(path); err != nil {
				t.Fatal(err)
			}
			registrations = gitCmd(t, repo, "worktree", "list", "--porcelain")
			out, stderr, code = runTreehouse(t, repo, home, nil, "enter", "--branch", branch, "--print-path")
			if code != 1 || out != "" || !strings.Contains(stderr, "work "+quoteReturnPath(branch)) {
				t.Fatalf("missing-only holders resolved: %d %s %s", code, out, stderr)
			}
			if after := gitCmd(t, repo, "worktree", "list", "--porcelain"); after != registrations {
				t.Fatal("enter changed missing registrations")
			}
			after, err = os.ReadFile(statePath)
			if err != nil || string(before) != string(after) {
				t.Fatalf("enter healed missing state: %v", err)
			}
		})
	}
}

func TestEnterBranchUnverifiableHolderE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	path := idleReportingSlot(t, repo, home)
	branch := "feature/unverifiable"
	gitCmd(t, path, "switch", "-c", branch)
	other := filepath.Join(filepath.Dir(repo), "unverifiable")
	gitCmd(t, repo, "worktree", "add", "--force", other, branch)
	if err := os.RemoveAll(other); err != nil {
		t.Fatal(err)
	}
	// A symlink loop produces a stat error other than not-exist, even when
	// tests run with elevated privileges. Do not treat it as a missing holder.
	if err := os.Symlink(other, other); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := os.Stat(other); err == nil || os.IsNotExist(err) {
		t.Fatalf("expected an unverifiable holder, got %v", err)
	}
	registrations := gitCmd(t, repo, "worktree", "list", "--porcelain")
	out, stderr, code := runTreehouse(t, repo, home, nil, "enter", "--branch", branch, "--print-path")
	if code != 1 || out != "" || stderr == "" {
		t.Fatalf("unverifiable holder ignored: %d %s %s", code, out, stderr)
	}
	if after := gitCmd(t, repo, "worktree", "list", "--porcelain"); after != registrations {
		t.Fatal("enter changed unverifiable registrations")
	}
}

func TestEnterBranchAmbiguityE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	path := idleReportingSlot(t, repo, home)
	gitCmd(t, path, "switch", "-c", "feature/ambiguous")
	other := filepath.Join(filepath.Dir(repo), "duplicate")
	gitCmd(t, repo, "worktree", "add", "--force", other, "feature/ambiguous")
	_, stderr, code := runTreehouse(t, repo, home, nil, "enter", "--branch", "feature/ambiguous", "--print-path")
	if code != 1 || !strings.Contains(stderr, "ambiguous") {
		t.Fatalf("ambiguous checkout selected: %d %s", code, stderr)
	}
}
