package gitvcs

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

func TestFetchTimeoutKillsProcessTree(t *testing.T) {
	dir := t.TempDir()
	name := "git"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	// A portable fake Git spawns a transport child holding its output pipes.
	// Killing only the direct Git process would leave this child running.
	source := `package main
import ("fmt"; "os"; "os/exec"; "time")
func main() {
 if len(os.Args) > 1 && os.Args[1] == "remote" { fmt.Println("origin"); return }
 if len(os.Args) > 1 && os.Args[1] == "fetch" {
  child := exec.Command(os.Args[0], "transport")
  child.Stdout, child.Stderr = os.Stdout, os.Stderr
  if err := child.Start(); err != nil { panic(err) }
  if err := os.WriteFile(os.Getenv("FETCH_TEST_PIDS"), []byte(fmt.Sprintf("%d %d", os.Getpid(), child.Process.Pid)), 0600); err != nil { panic(err) }
 }
 for { time.Sleep(time.Second) }
}`
	src := filepath.Join(dir, "helper.go")
	if err := os.WriteFile(src, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("go", "build", "-o", filepath.Join(dir, name), src).CombinedOutput(); err != nil {
		t.Fatalf("build helper: %s %v", out, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	pidFile := filepath.Join(dir, "pids")
	t.Setenv("FETCH_TEST_PIDS", pidFile)
	pids := func() []int32 {
		data, _ := os.ReadFile(pidFile)
		var result []int32
		for _, field := range strings.Fields(string(data)) {
			pid, err := strconv.ParseInt(field, 10, 32)
			if err != nil {
				t.Fatal(err)
			}
			result = append(result, int32(pid))
		}
		return result
	}
	t.Cleanup(func() {
		for _, pid := range pids() {
			if p, err := process.NewProcess(pid); err == nil {
				_ = p.Kill()
			}
		}
	})
	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- fetchWithTimeout(dir, time.Second) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected deadline error, got %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("fetch still blocked after its deadline and cleanup allowance")
	}
	if got := pids(); len(got) != 2 {
		t.Fatalf("fetch/transport did not start: %v", got)
	}
	for _, pid := range pids() {
		deadline := time.Now().Add(3 * time.Second)
		for {
			exists, err := process.PidExists(pid)
			if err != nil {
				t.Fatalf("inspect fetch descendant %d: %v", pid, err)
			}
			if !exists {
				break
			}
			// Unix zombies have terminated but may await init's reaping.
			// Windows Status is unsupported; require PidExists to become false.
			if runtime.GOOS != "windows" {
				p := &process.Process{Pid: pid}
				statuses, err := p.Status()
				if err == nil && strings.Contains(strings.Join(statuses, " "), "zombie") {
					break
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("fetch descendant %d still alive", pid)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	t.Logf("fetch and transport terminated after %s; PIDs %v", time.Since(start), pids())
}

func TestFetchNoOrigin(t *testing.T) {
	repo := t.TempDir()
	mustGit(t, "", "init", repo)
	if err := Fetch(repo); err != nil {
		t.Fatal(err)
	}
}

func TestFetchPreservesDiagnostics(t *testing.T) {
	repo := t.TempDir()
	mustGit(t, "", "init", repo)
	mustGit(t, repo, "remote", "add", "origin", filepath.Join(repo, "missing"))
	if err := Fetch(repo); err == nil || !strings.Contains(err.Error(), "does not appear to be a git repository") {
		t.Fatalf("missing git diagnostic: %v", err)
	}
}
