package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func runInteractiveInput(t *testing.T, repo, home, input string, args ...string) (string, string, int) {
	t.Helper()
	child := exec.Command(treehouseBin, args...)
	child.Dir = repo
	child.Env = buildEnv(home, "SHELL="+exitShellBin)
	child.Stdin = strings.NewReader(input)
	var out, stderr bytes.Buffer
	child.Stdout = &out
	child.Stderr = &stderr
	err := child.Run()
	code := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	return out.String(), stderr.String(), code
}

func TestInteractiveHomeCancelE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	out, stderr, code := runInteractiveInput(t, repo, home, "q\n", "menu")
	if code != 0 || out != "" || !strings.Contains(stderr, "Start a new branch") || !strings.Contains(stderr, "Clean up unused trees") {
		t.Fatalf("menu: code=%d stdout=%s stderr=%s", code, out, stderr)
	}
	if strings.Contains(stderr, "Setting up worktree") {
		t.Fatal("menu allocated before selection")
	}
}

func TestInteractiveBranchCycleE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	for _, input := range []string{"2\nfeature/first\n", "2\nfeature/second\n", "1\n1\n", "3\n1\n"} {
		out, stderr, code := runInteractiveInput(t, repo, home, input, "menu")
		if code != 0 || out != "" || strings.Count(stderr, "Treehouse ·") != 1 {
			t.Fatalf("shell exit reopened menu: code=%d stdout=%s stderr=%s", code, out, stderr)
		}
	}
	for _, name := range []string{"feature/first", "feature/second"} {
		gitCmd(t, repo, "show-ref", "--verify", "refs/heads/"+name)
	}
}

func TestInteractiveEnterKeepsTreeE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	path := idleReportingSlot(t, repo, home)
	gitCmd(t, path, "switch", "-c", "feature/dirty")
	file := filepath.Join(path, "unfinished.txt")
	if err := os.WriteFile(file, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(filepath.Dir(filepath.Dir(path)), "treehouse-state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	out, stderr, code := runInteractiveInput(t, repo, home, "1\n", "enter")
	if code != 0 || out != "" || !strings.Contains(stderr, "feature/dirty") {
		t.Fatalf("enter: %d %s %s", code, out, stderr)
	}
	after, err := os.ReadFile(statePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("enter changed state: %v", err)
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "keep me" {
		t.Fatalf("enter changed files: %v", err)
	}
	// An idle dirty branch is still offered for resuming, but canceling its
	// picker cannot reserve it or discard its changes.
	_, stderr, code = runInteractiveInput(t, repo, home, "1\nq\nq\n", "menu")
	if code != 0 || !strings.Contains(stderr, "feature/dirty · tree") {
		t.Fatalf("dirty branch missing: %d %s", code, stderr)
	}
	after, err = os.ReadFile(statePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("picker changed state: %v", err)
	}
}

func TestInteractivePruneE2E(t *testing.T) {
	for _, confirm := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "confirm"}[confirm], func(t *testing.T) {
			repo, home := setupTestRepo(t)
			// Keep a leased tree while creating a separate disposable one.
			out, stderr, code := runTreehouse(t, repo, home, nil, "get", "--lease")
			if code != 0 {
				t.Fatal(stderr)
			}
			leased := strings.TrimSpace(out)
			path := idleReportingSlot(t, repo, home)
			statePath := filepath.Join(filepath.Dir(filepath.Dir(path)), "treehouse-state.json")
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			answer := "n"
			if confirm {
				answer = "y"
			}
			out, stderr, code = runInteractiveInput(t, repo, home, "4\n"+answer+"\nq\n", "menu")
			if code != 0 || out != "" || !strings.Contains(stderr, "Remove the listed trees?") || strings.Count(stderr, "Treehouse ·") != 2 {
				t.Fatalf("prune flow: %d %s %s", code, out, stderr)
			}
			if _, err := os.Stat(leased); err != nil {
				t.Fatalf("leased tree removed: %v", err)
			}
			_, err = os.Stat(path)
			if confirm {
				if !os.IsNotExist(err) {
					t.Fatalf("tree not removed: %v %s", err, stderr)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				after, err := os.ReadFile(statePath)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("cancel changed state: %v", err)
				}
			}
			gitCmd(t, repo, "show-ref", "--verify", "refs/heads/main")
		})
	}
}

func TestInteractiveCanceledNameAndInvalidChoiceE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	before := gitCmd(t, repo, "worktree", "list", "--porcelain")
	_, stderr, code := runInteractiveInput(t, repo, home, "invalid\n2\n\nq\n", "menu")
	if code != 0 || !strings.Contains(stderr, "Choose one of the listed numbers") {
		t.Fatalf("invalid input: %d %s", code, stderr)
	}
	after := gitCmd(t, repo, "worktree", "list", "--porcelain")
	if before != after {
		t.Fatal("cancel allocated a tree")
	}
	if _, err := os.Stat(filepath.Join(home, ".treehouse")); !os.IsNotExist(err) {
		t.Fatalf("cancel created pool: %v", err)
	}
}

func TestInteractiveDirtyExitKeepsByDefaultE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	path := idleReportingSlot(t, repo, home)
	gitCmd(t, path, "switch", "-c", "feature/unfinished")
	file := filepath.Join(path, "unfinished.txt")
	if err := os.WriteFile(file, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		_, stderr, code := runInteractiveInput(t, repo, home, "1\n1\n\n", "menu")
		if code != 0 || strings.Count(stderr, "Treehouse ·") != 1 || !strings.Contains(stderr, "Reserved the existing branch slot") {
			t.Fatalf("kept work could not be reopened and exited: %d %s", code, stderr)
		}
		data, err := os.ReadFile(file)
		if err != nil || string(data) != "keep" {
			t.Fatalf("default exit discarded work: %v\n%s", err, stderr)
		}
	}
}

func TestInteractiveTerminalE2E(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY driver uses Unix; menu input tests also run on Windows")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python PTY driver unavailable")
	}
	driver, err := filepath.Abs(filepath.Join("testdata", "interactive_pty.py"))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"home", "cancel", "confirm"} {
		t.Run(mode, func(t *testing.T) {
			repo, home := setupTestRepo(t)
			var path string
			if mode != "home" {
				path = idleReportingSlot(t, repo, home)
			}
			child := exec.Command(python, driver, treehouseBin, mode)
			child.Dir = repo
			child.Env = buildEnv(home, "SHELL=/bin/sh")
			out, err := child.CombinedOutput()
			if err != nil {
				t.Fatalf("terminal flow: %v\n%s", err, out)
			}
			if mode == "home" {
				gitCmd(t, repo, "show-ref", "--verify", "refs/heads/feature/terminal")
			} else {
				_, err := os.Stat(path)
				if mode == "confirm" && !os.IsNotExist(err) {
					t.Fatalf("confirmation failed to remove tree: %v", err)
				}
				if mode == "cancel" && err != nil {
					t.Fatalf("cancel removed tree: %v", err)
				}
			}
		})
	}
}

func TestInteractiveJJHomeE2E(t *testing.T) {
	requireJJ(t)
	repo, home := setupJJTestRepo(t)
	for _, input := range []string{"1\n", "2\n1\n", "3\ny\nq\n"} {
		out, stderr, code := runInteractiveInput(t, repo, home, input, "menu")
		wantMenus := 1
		if strings.HasPrefix(input, "3") {
			wantMenus = 2
		}
		if code != 0 || out != "" || strings.Contains(stderr, "Start a new branch") || strings.Count(stderr, "Treehouse ·") != wantMenus {
			t.Fatalf("jj menu cycle: %d %s %s", code, out, stderr)
		}
	}
}
