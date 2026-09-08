package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func parkedHistorySlot(t *testing.T, repo, home string) string {
	t.Helper()
	path := idleReportingSlot(t, repo, home)
	gitCmd(t, path, "switch", "-c", "feature/history")
	gitCmd(t, path, "commit", "--allow-empty", "-m", "saved work")
	_, stderr, code := runTreehouse(t, repo, home, nil, "return", path)
	if code != 0 {
		t.Fatalf("return: %s", stderr)
	}
	return path
}

func TestLsGlobalPartialAndUnknownReadsDoNotWriteE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	path := parkedHistorySlot(t, repo, home)
	poolDir := filepath.Dir(filepath.Dir(path))
	statePath := filepath.Join(poolDir, "treehouse-state.json")
	brokenPool(t, filepath.Dir(poolDir), "broken-pool")
	if err := os.WriteFile(filepath.Join(path, ".git"), []byte("unavailable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--all", "--global"} {
		status, statusErr, statusCode := runTreehouse(t, home, home, nil, "status", flag, "--json")
		out, stderr, code := runTreehouse(t, home, home, nil, "ls", flag, "--json")
		if code == 0 || statusCode == 0 || status != out || !strings.Contains(stderr, "broken-pool") || !strings.Contains(statusErr, "broken-pool") {
			t.Fatalf("partial JSON mismatch: status=%s ls=%s stderr=%s", status, out, stderr)
		}
		var rows []map[string]json.RawMessage
		if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 1 {
			t.Fatalf("invalid partial JSON: %s %v", out, err)
		}
		out, stderr, code = runTreehouse(t, home, home, nil, "ls", flag)
		if code == 0 || !strings.Contains(out, filepath.Base(poolDir)+"/1") || !strings.Contains(out, "unknown") || !strings.Contains(out, "last used") || !strings.Contains(stderr, "broken-pool") {
			t.Fatalf("partial human listing: %s %s", out, stderr)
		}
	}
	after, err := os.ReadFile(statePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("global optional read wrote state: %v", err)
	}
}

func TestLsInUseHistoryE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	path := parkedHistorySlot(t, repo, home)
	signals := t.TempDir()
	ready := filepath.Join(signals, "ready")
	child := exec.Command(waitShellBin)
	child.Dir, child.Env = path, buildEnv(home, "TREEHOUSE_TEST_READY="+ready, "TREEHOUSE_TEST_RELEASE="+filepath.Join(signals, "release"))
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	deadline := time.Now().Add(10 * time.Second)
	for {
		if data, err := os.ReadFile(ready); err == nil && len(data) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not reach ready barrier")
		}
		time.Sleep(10 * time.Millisecond)
	}
	out, stderr, code := runTreehouse(t, repo, home, nil, "ls")
	if code != 0 || !strings.Contains(out, "in-use") || !strings.Contains(out, "last used") || strings.Contains(out, "parked") {
		t.Fatalf("history masked live process: %s %s", out, stderr)
	}
}

func TestJJLsOmitsGitHistoryE2E(t *testing.T) {
	requireJJ(t)
	repo, home := setupJJTestRepo(t)
	idleReportingSlot(t, repo, home)
	out, stderr, code := runTreehouse(t, repo, home, nil, "ls")
	if code != 0 || !strings.Contains(out, "idle") {
		t.Fatalf("jj ls: %s %s", out, stderr)
	}
	for _, forbidden := range []string{"git switch", "preserved", "last used", "branch <new-name>"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("Git claim for jj: %s", out)
		}
	}
}

func TestLsNextPreservesExplicitRootE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	root := t.TempDir()
	out, stderr, code := runTreehouse(t, repo, home, nil, "--root", root, "get", "--lease")
	if code != 0 {
		t.Fatalf("get: %s", stderr)
	}
	path := strings.TrimSpace(out)
	actualRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(path))))
	out, stderr, code = runTreehouse(t, repo, home, nil, "--root", root, "ls")
	want := "treehouse --root " + quoteReturnPath(actualRoot) + " enter " + quoteReturnPath("1")
	if code != 0 || !strings.Contains(out, want) {
		t.Fatalf("NEXT lost explicit root: %s %s; want %s", out, stderr, want)
	}
	out, stderr, code = runTreehouse(t, repo, home, nil, "--root", actualRoot, "enter", "--print-path", "1")
	if code != 0 || strings.TrimSpace(out) != path {
		t.Fatalf("NEXT selected wrong slot: %s %s", out, stderr)
	}
}

func TestLsJSONMatchesStatusWithoutHistoryFieldE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	parkedHistorySlot(t, repo, home)
	status, stderr, code := runTreehouse(t, repo, home, nil, "status", "--json")
	if code != 0 {
		t.Fatalf("status: %s", stderr)
	}
	out, stderr, code := runTreehouse(t, repo, home, nil, "ls", "--json")
	if code != 0 || out != status {
		t.Fatalf("JSON mismatch: status=%s ls=%s stderr=%s", status, out, stderr)
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	want := []string{"name", "path", "status", "flavor", "lease_id", "lease_holder", "leased_at", "processes"}
	if len(rows) != 1 || len(rows[0]) != len(want) {
		t.Fatalf("schema changed: %s", out)
	}
	for _, key := range want {
		if _, ok := rows[0][key]; !ok {
			t.Errorf("missing key %s", key)
		}
	}
}

func TestLsHistoryNeverMasksCurrentStateE2E(t *testing.T) {
	for _, scenario := range []string{"dirty", "leased", "damaged", "repeated-return", "deleted-branch", "base-advanced", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			repo, home := setupTestRepo(t)
			path := parkedHistorySlot(t, repo, home)
			want := "dirty"
			switch scenario {
			case "dirty":
				if err := os.WriteFile(filepath.Join(path, "dirty"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "leased":
				_, stderr, code := runTreehouse(t, repo, home, nil, "lease", "1", "--lease-holder", "agent-a")
				if code != 0 {
					t.Fatalf("lease: %s", stderr)
				}
				want = "leased: agent-a"
			case "damaged":
				if err := os.Remove(filepath.Join(path, ".git")); err != nil {
					t.Fatal(err)
				}
				want = "damaged"
			case "repeated-return":
				_, stderr, code := runTreehouse(t, repo, home, nil, "return", path)
				if code != 0 {
					t.Fatalf("return: %s", stderr)
				}
				want = "idle"
			case "deleted-branch":
				gitCmd(t, repo, "branch", "-D", "feature/history")
				want = "idle"
			case "unavailable":
				if err := os.WriteFile(filepath.Join(path, ".git"), []byte("unavailable\n"), 0600); err != nil {
					t.Fatal(err)
				}
				want = "unknown"
			case "base-advanced":
				gitCmd(t, repo, "commit", "--allow-empty", "-m", "new base")
				want = "idle"
			}
			statePath := filepath.Join(filepath.Dir(filepath.Dir(path)), "treehouse-state.json")
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			out, stderr, code := runTreehouse(t, repo, home, nil, "ls")
			if code != 0 || !strings.Contains(out, want) || strings.Contains(out, "parked") {
				t.Fatalf("incorrect %s row: %s %s", scenario, out, stderr)
			}
			if scenario == "dirty" || scenario == "leased" || scenario == "base-advanced" || scenario == "unavailable" {
				if !strings.Contains(out, "feature/history (last used)") {
					t.Fatalf("lost advisory history: %s", out)
				}
			} else if strings.Contains(out, "feature/history") {
				t.Fatalf("stale or damaged history shown: %s", out)
			}
			if _, errOut, exitCode := runTreehouse(t, repo, home, nil, "ls", "--json"); exitCode != 0 {
				t.Fatalf("ls JSON: %s", errOut)
			}
			after, err := os.ReadFile(statePath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("ls wrote state: %v", err)
			}
		})
	}
}

func TestLsDetachedPreservationE2E(t *testing.T) {
	for _, preserved := range []bool{false, true} {
		t.Run(map[bool]string{false: "unpreserved", true: "preserved"}[preserved], func(t *testing.T) {
			repo, home := setupTestRepo(t)
			path := idleReportingSlot(t, repo, home)
			gitCmd(t, path, "commit", "--allow-empty", "-m", "detached")
			if preserved {
				gitCmd(t, path, "tag", "saved")
			}
			out, stderr, code := runTreehouse(t, repo, home, nil, "ls")
			want := "detached, unpreserved"
			if preserved {
				want = "detached, preserved, unmerged"
			}
			if code != 0 || !strings.Contains(out, want) {
				t.Fatalf("incorrect preservation: %s %s", out, stderr)
			}
			if strings.Contains(out, "branch <new-name> HEAD") == preserved {
				t.Fatalf("incorrect remedy: %s", out)
			}
		})
	}
}

func TestLsParkedHistoryE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	path := idleReportingSlot(t, repo, home)
	gitCmd(t, path, "switch", "-c", "feature/history")
	gitCmd(t, path, "commit", "--allow-empty", "-m", "saved work")
	_, stderr, code := runTreehouse(t, repo, home, nil, "return", path)
	if code != 0 {
		t.Fatalf("return: %s", stderr)
	}
	out, stderr, code := runTreehouse(t, repo, home, nil, "ls")
	if code != 0 {
		t.Fatalf("ls: %s", stderr)
	}
	for _, want := range []string{"SLOT", "BRANCH", "STATE", "NEXT", "feature/history", "last used", "work " + quoteReturnPath("feature/history")} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q: %s", want, out)
		}
	}
}
