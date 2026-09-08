package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobalNavigation(t *testing.T) {
	home, root, outside := t.TempDir(), t.TempDir(), t.TempDir()
	repos := []string{setupTestRepoWithHome(t, home, "same"), setupTestRepoWithHome(t, home, "same")}
	paths := map[string]string{}
	states := map[string][]byte{}
	heads := map[string]string{}
	for _, repo := range repos {
		out, errOut, code := runTreehouse(t, repo, home, nil, "--root", root, "get", "--lease", "--json")
		if code != 0 {
			t.Fatal(errOut)
		}
		var lease leaseJSONResult
		if err := json.Unmarshal([]byte(out), &lease); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Dir(filepath.Dir(lease.Path))
		selector := filepath.Base(dir) + "/1"
		paths[selector] = lease.Path
		statePath := filepath.Join(dir, "treehouse-state.json")
		states[statePath], _ = os.ReadFile(statePath)
		heads[lease.Path] = gitCmd(t, lease.Path, "rev-parse", "HEAD")
		if err := os.WriteFile(filepath.Join(lease.Path, "precious"), []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if len(paths) != 2 {
		t.Fatal("same basenames must have distinct pools")
	}
	for _, flag := range []string{"--all", "--global"} {
		t.Run(flag, func(t *testing.T) {
			out, errOut, code := runTreehouse(t, outside, home, nil, "--root", root, "status", flag, "--json")
			if code != 0 {
				t.Fatalf("global status: %s", errOut)
			}
			var rows []struct{ Pool, Selector, Path, Status string }
			if err := json.Unmarshal([]byte(out), &rows); err != nil {
				t.Fatal(err)
			}
			if len(rows) != 2 {
				t.Fatalf("rows: %s", out)
			}
			for _, row := range rows {
				if paths[row.Selector] != row.Path || row.Pool+"/1" != row.Selector || row.Status != "leased" {
					t.Fatalf("bad row: %+v", row)
				}
			}
		})
	}
	for selector, path := range paths {
		t.Run(selector, func(t *testing.T) {
			out, errOut, code := runTreehouse(t, outside, home, nil, "--root", root, "enter", "--print-path", selector)
			if code != 0 || strings.TrimSpace(out) != path {
				t.Fatalf("qualified enter: %q %s", out, errOut)
			}
			_, errOut, code = runTreehouse(t, outside, home, []string{"SHELL=" + exitShellBin}, "--root", root, "enter", selector)
			if code != 0 {
				t.Fatal(errOut)
			}
		})
	}
	for path, before := range states {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("state changed: %s", path)
		}
	}
	for path, head := range heads {
		if gitCmd(t, path, "rev-parse", "HEAD") != head {
			t.Fatal("HEAD changed")
		}
		data, err := os.ReadFile(filepath.Join(path, "precious"))
		if err != nil || string(data) != "keep" {
			t.Fatal("files changed")
		}
	}
	for _, selector := range []string{"../1", "pool/../1", "/pool/1", `C:\pool\1`, `pool\1`, "pool/", "missing/1"} {
		_, _, code := runTreehouse(t, outside, home, nil, "--root", root, "enter", "--print-path", selector)
		if code == 0 {
			t.Errorf("accepted invalid/unknown selector %q", selector)
		}
	}
	empty := filepath.Join(outside, "absent")
	out, errOut, code := runTreehouse(t, outside, home, nil, "--root", empty, "status", "--all", "--json")
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("empty root: %q %s", out, errOut)
	}
	if _, err := os.Stat(empty); !os.IsNotExist(err) {
		t.Fatal("navigation created root")
	}
	_, _, code = runTreehouse(t, outside, home, nil, "--root", "relative", "status", "--all")
	if code == 0 {
		t.Fatal("accepted relative global root")
	}
}

func brokenPool(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A state file from a newer treehouse is a hard read failure.
	if err := os.WriteFile(filepath.Join(dir, "treehouse-state.json"), []byte(`{"version":999,"worktrees":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// One unreadable pool must not hide every other project. Global status emits
// the healthy pools on stdout (still a valid top-level JSON array), names the
// unreadable pool on stderr, and exits nonzero to flag the partial listing.
func TestGlobalStatusPartialListing(t *testing.T) {
	home, root, outside := t.TempDir(), t.TempDir(), t.TempDir()
	repo := setupTestRepoWithHome(t, home, "healthy")
	out, errOut, code := runTreehouse(t, repo, home, nil, "--root", root, "get", "--lease", "--json")
	if code != 0 {
		t.Fatal(errOut)
	}
	var lease leaseJSONResult
	if err := json.Unmarshal([]byte(out), &lease); err != nil {
		t.Fatal(err)
	}
	poolDir := filepath.Dir(filepath.Dir(lease.Path))
	navRoot := filepath.Dir(poolDir)
	healthySelector := filepath.Base(poolDir) + "/1"
	brokenPool(t, navRoot, "broken-pool")

	out, errOut, code = runTreehouse(t, outside, home, nil, "--root", root, "status", "--all", "--json")
	if code == 0 {
		t.Error("a partial listing must exit nonzero")
	}
	var rows []struct{ Selector, Path string }
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("stdout is not a JSON array on partial success: %q (%v)", out, err)
	}
	if len(rows) != 1 || rows[0].Selector != healthySelector || rows[0].Path != lease.Path {
		t.Fatalf("healthy pool missing from partial listing: %q", out)
	}
	if !strings.Contains(errOut, "broken-pool") {
		t.Errorf("stderr does not identify the unreadable pool: %q", errOut)
	}

	out, errOut, code = runTreehouse(t, outside, home, nil, "--root", root, "status", "--all")
	if code == 0 {
		t.Error("a partial human listing must exit nonzero")
	}
	if !strings.Contains(out, healthySelector) {
		t.Errorf("healthy pool missing from partial human listing: %q", out)
	}
	if !strings.Contains(errOut, "broken-pool") {
		t.Errorf("stderr does not identify the unreadable pool: %q", errOut)
	}

	// An all-healthy root still succeeds, so the nonzero exit means "incomplete".
	if err := os.RemoveAll(filepath.Join(navRoot, "broken-pool")); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code = runTreehouse(t, outside, home, nil, "--root", root, "status", "--all", "--json"); code != 0 {
		t.Fatalf("healthy root must exit zero: %s", errOut)
	}
}

// A qualified selector is answered from the user-level root, so its not-found
// guidance must name commands that work outside any repository. Local guidance
// is unchanged.
func TestEnterNotFoundGuidance(t *testing.T) {
	home, root, outside := t.TempDir(), t.TempDir(), t.TempDir()
	repo := setupTestRepoWithHome(t, home, "guide")
	out, errOut, code := runTreehouse(t, repo, home, nil, "--root", root, "get", "--lease", "--json")
	if code != 0 {
		t.Fatal(errOut)
	}
	var lease leaseJSONResult
	if err := json.Unmarshal([]byte(out), &lease); err != nil {
		t.Fatal(err)
	}
	poolDir := filepath.Dir(filepath.Dir(lease.Path))
	poolName := filepath.Base(poolDir)

	emptyPool := filepath.Join(filepath.Dir(poolDir), "empty-pool")
	if err := os.MkdirAll(emptyPool, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(emptyPool, "treehouse-state.json"), []byte(`{"version":4,"worktrees":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name     string
		dir      string
		selector string
	}{
		{"unknown slot", outside, poolName + "/99"},
		{"empty pool", outside, "empty-pool/1"},
		{"unknown pool", outside, "absent-pool/1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, errOut, code := runTreehouse(t, tc.dir, home, nil, "--root", root, "enter", "--print-path", tc.selector)
			if code == 0 {
				t.Fatal("expected failure")
			}
			if !strings.Contains(errOut, "treehouse status --all") {
				t.Errorf("qualified guidance does not name a globally usable command: %q", errOut)
			}
			if strings.Contains(errOut, "treehouse get") {
				t.Errorf("qualified guidance suggests 'treehouse get', which cannot resolve the pool from cwd: %q", errOut)
			}
		})
	}

	t.Run("local guidance preserved", func(t *testing.T) {
		_, errOut, code := runTreehouse(t, repo, home, nil, "--root", root, "enter", "--print-path", "99")
		if code == 0 {
			t.Fatal("expected failure")
		}
		if !strings.Contains(errOut, "treehouse status") || strings.Contains(errOut, "--all") {
			t.Errorf("local guidance changed: %q", errOut)
		}
	})
}
