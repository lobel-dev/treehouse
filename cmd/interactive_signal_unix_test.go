//go:build !windows

package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInteractiveChildSignalE2E(t *testing.T) {
	repo, home := setupTestRepo(t)
	shell := filepath.Join(t.TempDir(), "kill-shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nkill -KILL \"$PPID\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(treehouseBin, "menu")
	child.Dir = repo
	child.Env = buildEnv(home, "SHELL="+shell)
	child.Stdin = strings.NewReader("2\nfeature/killed\n")
	var stderr bytes.Buffer
	child.Stderr = &stderr
	err := child.Run()
	if err == nil || !strings.Contains(stderr.String(), "signal: killed") {
		t.Fatalf("child signal hidden: err=%v stderr=%s", err, stderr.String())
	}
	if strings.Count(stderr.String(), "Treehouse ·") != 1 {
		t.Fatal("signal reopened the home screen")
	}
}
