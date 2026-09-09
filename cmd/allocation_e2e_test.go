package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kunchenguid/treehouse/internal/config"
)

func TestGetSkipsUnmanagedSlotE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	poolDir, err := config.ResolvePoolDir(repo, home)
	if err != nil {
		t.Fatal(err)
	}
	leftover := filepath.Join(poolDir, "1", filepath.Base(repo), "Library", "keep.txt")
	if err := os.MkdirAll(filepath.Dir(leftover), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leftover, []byte("Unity leftovers must survive"), 0644); err != nil {
		t.Fatal(err)
	}
	// A non-directory slot must also be preserved and skipped.
	occupiedFile := filepath.Join(poolDir, "2")
	if err := os.WriteFile(occupiedFile, []byte("keep slot file"), 0644); err != nil {
		t.Fatal(err)
	}
	out, stderr, code := runTreehouse(t, repo, home, nil, "get", "--lease", "--no-fetch")
	if code != 0 {
		t.Fatalf("get failed: %s", stderr)
	}
	want := filepath.Join(poolDir, "3", filepath.Base(repo))
	if strings.TrimSpace(out) != want {
		t.Fatalf("acquired %q, want %s", out, want)
	}
	gitCmd(t, want, "rev-parse", "--verify", "HEAD")
	for path, want := range map[string]string{leftover: "Unity leftovers must survive", occupiedFile: "keep slot file"} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("existing content changed at %s: %q, %v", path, data, err)
		}
	}
}
