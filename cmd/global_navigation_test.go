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
