package pool

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kunchenguid/treehouse/internal/process"
)

func workSlot(t *testing.T) (repo, dir, path string) {
	t.Helper()
	repo, dir = setupRepo(t)
	var err error
	path, err = Acquire(repo, dir, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, path, "switch", "-c", "feature/work")
	clearOwnerReservation(t, dir, path)
	return
}

func TestWorkReclamationRefusalsPreserveState(t *testing.T) {
	for _, kind := range []string{"leased", "destroying", "quarantined", "owner", "process", "process-error", "damaged", "other-repo"} {
		t.Run(kind, func(t *testing.T) {
			repo, dir, path := workSlot(t)
			state, err := ReadState(dir)
			if err != nil {
				t.Fatal(err)
			}
			entry := &state.Worktrees[0]
			switch kind {
			case "leased":
				entry.Leased = true
			case "destroying":
				entry.Destroying = true
			case "quarantined":
				setSeedInventory(entry, nil, false)
			case "owner":
				if err := reserveOwner(entry); err != nil {
					t.Fatal(err)
				}
			case "damaged":
				if err := os.Remove(filepath.Join(path, ".git")); err != nil {
					t.Fatal(err)
				}
			case "other-repo":
				repo, _ = setupRepo(t)
			}
			if err := WriteState(dir, state); err != nil {
				t.Fatal(err)
			}
			old := findProcessesInWorktree
			if kind == "process" || kind == "process-error" {
				findProcessesInWorktree = func(string) ([]process.ProcessInfo, error) {
					if kind == "process-error" {
						return nil, fmt.Errorf("inspection unavailable")
					}
					return []process.ProcessInfo{{PID: 123, Name: "writer"}}, nil
				}
			}
			t.Cleanup(func() { findProcessesInWorktree = old })
			before, err := os.ReadFile(stateFilePath(dir))
			if err != nil {
				t.Fatal(err)
			}
			got, err := ReclaimBranch(repo, dir, path, "feature/work")
			if err == nil || got != "" {
				t.Fatalf("reclaimed protected slot: %s %v", got, err)
			}
			after, readErr := os.ReadFile(stateFilePath(dir))
			if readErr != nil || !bytes.Equal(before, after) {
				t.Fatalf("refusal changed state: %v", readErr)
			}
		})
	}
}

func TestWorkReclamationPreservesBaseSeedAndFiles(t *testing.T) {
	repo, dir, path := workSlot(t)
	state, err := ReadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	state.Worktrees[0].BaseBranch = "main"
	state.Worktrees[0].LastBranch = "unrelated-history"
	if err := WriteState(dir, state); err != nil {
		t.Fatal(err)
	}
	state, err = ReadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	before := state.Worktrees[0]
	runGit(t, path, "commit", "--allow-empty", "-m", "unmerged")
	head := gitOut(t, path, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(path, "scratch"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := AcquireWorkBranch(repo, dir, "feature/work", 4, []string{"this-hook-must-not-run"}, AcquireOptions{SkipFetch: true, BaseBranch: "missing-base"})
	if err != nil || got.Path != path || !got.Reclaimed {
		t.Fatalf("reclaim: %+v %v", got, err)
	}
	after, err := FindByPath(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	if after.BaseBranch != before.BaseBranch || after.SeedInventoryDigest != before.SeedInventoryDigest || !reflect.DeepEqual(after.SeededPaths, before.SeededPaths) || after.LastBranch != "" {
		t.Fatalf("metadata changed: before=%+v after=%+v", before, after)
	}
	if got := gitOut(t, path, "rev-parse", "HEAD"); got != head {
		t.Fatal("HEAD changed")
	}
	if data, err := os.ReadFile(filepath.Join(path, "scratch")); err != nil || string(data) != "keep" {
		t.Fatalf("files changed: %s %v", data, err)
	}
}

func TestWorkConcurrentReclamation(t *testing.T) {
	repo, dir, path := workSlot(t)
	entered, proceed := make(chan struct{}), make(chan struct{})
	old := findProcessesInWorktree
	findProcessesInWorktree = func(string) ([]process.ProcessInfo, error) { close(entered); <-proceed; return nil, nil }
	t.Cleanup(func() { findProcessesInWorktree = old })
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { _, err := ReclaimBranch(repo, dir, path, "feature/work"); first <- err }()
	<-entered
	go func() { _, err := ReclaimBranch(repo, dir, path, "feature/work"); second <- err }()
	close(proceed)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err == nil || !strings.Contains(err.Error(), "live owner") {
		t.Fatalf("second claimant: %v", err)
	}
}

func TestWorkSwitchRefusesChangedLease(t *testing.T) {
	repo, dir, path := workSlot(t)
	if _, err := ReclaimBranch(repo, dir, path, "feature/work"); err != nil {
		t.Fatal(err)
	}
	if _, err := LeaseExisting(dir, "1", "new-holder"); err != nil {
		t.Fatal(err)
	}
	if err := SwitchOwnedBranch(repo, dir, path, "new-branch", ""); err == nil {
		t.Fatal("switch ignored changed lease")
	}
	if got := gitOut(t, path, "symbolic-ref", "--short", "HEAD"); got != "feature/work" {
		t.Fatalf("branch changed: %s", got)
	}
}
